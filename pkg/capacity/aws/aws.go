package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/bootstrap"
)

const ProviderName = "aws"

// Provider provisions EC2 GPU instances.
type Provider struct {
	client    *ec2.Client
	region    string
	poolName  string
	instanceTypes []string
	subnets   []string
	capacityType string
	boot      capacity.BootstrapConfig
	lastICE   []time.Time
	hourlyUSD float64
}

func NewFactory() capacity.Factory {
	return func(ctx context.Context, pool *opencodav1alpha1.GPUPool, creds map[string]string, boot capacity.BootstrapConfig) (capacity.CapacityProvider, error) {
		region := pool.Spec.Provider.Params["region"]
		if region == "" {
			region = "us-east-1"
		}
		cfg, err := loadAWSConfig(ctx, region, creds)
		if err != nil {
			return nil, err
		}
		capType := pool.Spec.Provider.Params["capacityType"]
		if capType == "" {
			capType = "spot"
		}
		subnets := splitCSV(pool.Spec.Provider.Params["subnets"])
		return &Provider{
			client:        ec2.NewFromConfig(cfg),
			region:        region,
			poolName:      pool.Name,
			instanceTypes: pool.Spec.InstanceTypes,
			subnets:       subnets,
			capacityType:  capType,
			boot:          boot,
			hourlyUSD:     0,
		}, nil
	}
}

func loadAWSConfig(ctx context.Context, region string, creds map[string]string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	if ak, ok := creds["AWS_ACCESS_KEY_ID"]; ok {
		sk := creds["AWS_SECRET_ACCESS_KEY"]
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, creds["AWS_SESSION_TOKEN"])))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) Quote(ctx context.Context, req capacity.GPURequest) ([]capacity.Offer, error) {
	instanceType := "p5.48xlarge"
	if len(p.instanceTypes) > 0 {
		instanceType = p.instanceTypes[0]
	}
	if len(req.InstanceTypes) > 0 {
		instanceType = req.InstanceTypes[0]
	}
	zone := p.region + "a"
	if req.Constraints.Zone != "" {
		zone = req.Constraints.Zone
	}
	price := p.hourlyUSD
	if price == 0 {
		out, err := p.client.DescribeSpotPriceHistory(ctx, &ec2.DescribeSpotPriceHistoryInput{
			InstanceTypes: []types.InstanceType{types.InstanceType(instanceType)},
			ProductDescriptions: []string{"Linux/UNIX"},
			MaxResults: aws.Int32(1),
		})
		if err == nil && len(out.SpotPriceHistory) > 0 {
			if v, err := parseSpotPrice(out.SpotPriceHistory[0].SpotPrice); err == nil {
				price = v
				p.hourlyUSD = v
			}
		}
	}
	if price == 0 {
		price = 21.0
	}
	return []capacity.Offer{{
		ID:            fmt.Sprintf("aws-%s-%d", p.poolName, time.Now().UnixNano()),
		InstanceType:  instanceType,
		Zone:          zone,
		HourlyUSD:     price,
		ExpiresAt:     time.Now().Add(5 * time.Minute),
		Interruptible: p.capacityType == "spot" || req.Constraints.CapacityType == "spot",
	}}, nil
}

func parseSpotPrice(s *string) (float64, error) {
	if s == nil {
		return 0, fmt.Errorf("nil price")
	}
	var f float64
	_, err := fmt.Sscanf(*s, "%f", &f)
	return f, err
}

func (p *Provider) Provision(ctx context.Context, offer capacity.Offer) (*capacity.NodeHandle, error) {
	userdata := bootstrap.UserData(bootstrap.Config{
		APIServerURL: p.boot.APIServerURL,
		CABundle:     p.boot.CABundle,
		JoinToken:    p.boot.JoinToken,
		PoolName:     p.poolName,
		JoinMode:     "eks",
	})
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String("resolve-latest"),
		InstanceType: types.InstanceType(offer.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		UserData:     aws.String(userdata),
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeInstance,
			Tags: []types.Tag{
				{Key: aws.String("opencoda.dev/pool"), Value: aws.String(p.poolName)},
				{Key: aws.String("opencoda.dev/provider"), Value: aws.String(ProviderName)},
			},
		}},
	}
	if len(p.subnets) > 0 {
		input.SubnetId = aws.String(p.subnets[0])
	}
	if p.capacityType == "spot" || offer.Interruptible {
		input.InstanceMarketOptions = &types.InstanceMarketOptionsRequest{
			MarketType: types.MarketTypeSpot,
		}
	}
	out, err := p.client.RunInstances(ctx, input)
	if err != nil {
		if isInsufficientCapacity(err) {
			p.lastICE = append(p.lastICE, time.Now())
			return nil, capacity.ErrICE
		}
		return nil, err
	}
	if len(out.Instances) == 0 {
		return nil, fmt.Errorf("no instances returned")
	}
	inst := out.Instances[0]
	id := aws.ToString(inst.InstanceId)
	zone := aws.ToString(inst.Placement.AvailabilityZone)
	nodeName := fmt.Sprintf("aws-%s", id)
	return &capacity.NodeHandle{
		ProviderID: fmt.Sprintf("aws://%s/%s", zone, id),
		NodeName:   nodeName,
		Labels: map[string]string{
			"opencoda.dev/provider": ProviderName,
			"opencoda.dev/pool":     p.poolName,
			"node.kubernetes.io/instance-type": offer.InstanceType,
		},
		LaunchedAt: time.Now(),
	}, nil
}

func isInsufficientCapacity(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "InsufficientInstanceCapacity" || apiErr.ErrorCode() == "InsufficientCapacityException"
	}
	return false
}

func (p *Provider) Release(ctx context.Context, h *capacity.NodeHandle) error {
	id := parseInstanceID(h.ProviderID)
	if id == "" {
		return nil
	}
	_, err := p.client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{id},
	})
	return err
}

func parseInstanceID(providerID string) string {
	// aws://zone/i-abc123
	parts := strings.Split(providerID, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (p *Provider) Capacity(ctx context.Context, pool string) (capacity.CapacityReport, error) {
	return capacity.CapacityReport{
		Available:         8,
		RecentICE:         append([]time.Time{}, p.lastICE...),
		ObservedHourlyUSD: p.hourlyUSD,
	}, nil
}
