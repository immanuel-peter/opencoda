package static

import (
	"context"
	"fmt"
	"time"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
)

const ProviderName = "static"

// Provider claims pre-joined nodes via labels/taints (BYO hardware).
type Provider struct {
	poolName string
}

func NewFactory() capacity.Factory {
	return func(ctx context.Context, pool *opencodav1alpha1.GPUPool, creds map[string]string, boot capacity.BootstrapConfig) (capacity.CapacityProvider, error) {
		return &Provider{poolName: pool.Name}, nil
	}
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) Quote(ctx context.Context, req capacity.GPURequest) ([]capacity.Offer, error) {
	return []capacity.Offer{{
		ID:            fmt.Sprintf("static-%s", p.poolName),
		InstanceType:  "static",
		Zone:          "local",
		HourlyUSD:     0,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		Interruptible: false,
	}}, nil
}

func (p *Provider) Provision(ctx context.Context, offer capacity.Offer) (*capacity.NodeHandle, error) {
	// Static: nodes are pre-joined; record a placeholder until pool controller binds real node name.
	nodeName := fmt.Sprintf("static-pending-%d", time.Now().UnixNano())
	return &capacity.NodeHandle{
		ProviderID: fmt.Sprintf("static://%s/%s", p.poolName, nodeName),
		NodeName:   nodeName,
		Labels: map[string]string{
			"opencoda.dev/provider": ProviderName,
			"opencoda.dev/pool":     p.poolName,
		},
		LaunchedAt: time.Now(),
	}, nil
}

func (p *Provider) Release(ctx context.Context, h *capacity.NodeHandle) error {
	return nil
}

func (p *Provider) Capacity(ctx context.Context, pool string) (capacity.CapacityReport, error) {
	return capacity.CapacityReport{
		Available:         100,
		ObservedHourlyUSD: 0,
	}, nil
}
