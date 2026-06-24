package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/proto"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/bootstrap"
)

const ProviderName = "gcp"

// Provider provisions GCE GPU instances.
type Provider struct {
	client    *compute.InstancesClient
	project   string
	zone      string
	poolName  string
	instanceTypes []string
	boot      capacity.BootstrapConfig
	lastICE   []time.Time
	hourlyUSD float64
}

func NewFactory() capacity.Factory {
	return func(ctx context.Context, pool *opencodav1alpha1.GPUPool, creds map[string]string, boot capacity.BootstrapConfig) (capacity.CapacityProvider, error) {
		project := pool.Spec.Provider.Params["project"]
		if project == "" {
			project = creds["GCP_PROJECT"]
		}
		zone := pool.Spec.Provider.Params["zone"]
		if zone == "" {
			zone = "us-central1-a"
		}
		var opts []option.ClientOption
		if jsonCreds := creds["GOOGLE_APPLICATION_CREDENTIALS_JSON"]; jsonCreds != "" {
			opts = append(opts, option.WithCredentialsJSON([]byte(jsonCreds)))
		}
		client, err := compute.NewInstancesRESTClient(ctx, opts...)
		if err != nil {
			return nil, err
		}
		return &Provider{
			client:        client,
			project:       project,
			zone:          zone,
			poolName:      pool.Name,
			instanceTypes: pool.Spec.InstanceTypes,
			boot:          boot,
		}, nil
	}
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) Quote(ctx context.Context, req capacity.GPURequest) ([]capacity.Offer, error) {
	instanceType := "a2-highgpu-1g"
	if len(p.instanceTypes) > 0 {
		instanceType = p.instanceTypes[0]
	}
	if len(req.InstanceTypes) > 0 {
		instanceType = req.InstanceTypes[0]
	}
	zone := p.zone
	if req.Constraints.Zone != "" {
		zone = req.Constraints.Zone
	}
	price := p.hourlyUSD
	if price == 0 {
		price = 2.5
	}
	return []capacity.Offer{{
		ID:            fmt.Sprintf("gcp-%s-%d", p.poolName, time.Now().UnixNano()),
		InstanceType:  instanceType,
		Zone:          zone,
		HourlyUSD:     price,
		ExpiresAt:     time.Now().Add(5 * time.Minute),
		Interruptible: true,
	}}, nil
}

func (p *Provider) Provision(ctx context.Context, offer capacity.Offer) (*capacity.NodeHandle, error) {
	name := fmt.Sprintf("coda-%s-%d", p.poolName, time.Now().UnixNano())
	startup := bootstrap.StartupScript(bootstrap.Config{
		APIServerURL: p.boot.APIServerURL,
		CABundle:     p.boot.CABundle,
		JoinToken:    p.boot.JoinToken,
		PoolName:     p.poolName,
		JoinMode:     "gke",
	})
	inst := &computepb.Instance{
		Name: proto.String(name),
		Scheduling: &computepb.Scheduling{
			ProvisioningModel: proto.String("SPOT"),
		},
		Disks: []*computepb.AttachedDisk{{
			InitializeParams: &computepb.AttachedDiskInitializeParams{
				SourceImage: proto.String("projects/debian-cloud/global/images/family/debian-12"),
			},
			AutoDelete: proto.Bool(true),
			Boot:       proto.Bool(true),
		}},
		NetworkInterfaces: []*computepb.NetworkInterface{{
			Network: proto.String("global/networks/default"),
			AccessConfigs: []*computepb.AccessConfig{{
				Name: proto.String("External NAT"),
				Type: proto.String("ONE_TO_ONE_NAT"),
			}},
		}},
		Metadata: &computepb.Metadata{
			Items: []*computepb.Items{
				{Key: proto.String("startup-script"), Value: proto.String(startup)},
				{Key: proto.String("opencoda-pool"), Value: proto.String(p.poolName)},
			},
		},
		GuestAccelerators: []*computepb.AcceleratorConfig{{
			AcceleratorCount: proto.Int32(1),
			AcceleratorType:  proto.String("zones/" + p.zone + "/acceleratorTypes/nvidia-tesla-a100"),
		}},
	}
	req := &computepb.InsertInstanceRequest{
		Project:          p.project,
		Zone:             p.zone,
		InstanceResource: inst,
	}
	op, err := p.client.Insert(ctx, req)
	if err != nil {
		if isGCPExhausted(err) {
			p.lastICE = append(p.lastICE, time.Now())
			return nil, capacity.ErrICE
		}
		return nil, err
	}
	if err := op.Wait(ctx); err != nil {
		if isGCPExhausted(err) {
			p.lastICE = append(p.lastICE, time.Now())
			return nil, capacity.ErrICE
		}
		return nil, err
	}
	providerID := fmt.Sprintf("gcp://%s/%s/%s", p.project, p.zone, name)
	return &capacity.NodeHandle{
		ProviderID: providerID,
		NodeName:   name,
		Labels: map[string]string{
			"opencoda.dev/provider": ProviderName,
			"opencoda.dev/pool":     p.poolName,
		},
		LaunchedAt: time.Now(),
	}, nil
}

func isGCPExhausted(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "ZONE_RESOURCE_POOL_EXHAUSTED") ||
		strings.Contains(msg, "RESOURCE_POOL_EXHAUSTED") ||
		strings.Contains(msg, "QUOTA_EXCEEDED")
}

func (p *Provider) Release(ctx context.Context, h *capacity.NodeHandle) error {
	name := parseInstanceName(h.ProviderID)
	if name == "" {
		name = h.NodeName
	}
	req := &computepb.DeleteInstanceRequest{
		Project:  p.project,
		Zone:     p.zone,
		Instance: name,
	}
	op, err := p.client.Delete(ctx, req)
	if err != nil {
		return err
	}
	return op.Wait(ctx)
}

func parseInstanceName(providerID string) string {
	parts := strings.Split(providerID, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (p *Provider) Capacity(ctx context.Context, pool string) (capacity.CapacityReport, error) {
	return capacity.CapacityReport{
		Available:         4,
		RecentICE:         append([]time.Time{}, p.lastICE...),
		ObservedHourlyUSD: p.hourlyUSD,
	}, nil
}
