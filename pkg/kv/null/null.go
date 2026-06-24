package null

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/engine"
	"github.com/immanuel-peter/opencoda/pkg/kv"
)

const ProviderName = "null"

// Provider is a no-op KV backend for testing.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) Capabilities() kv.KVCapabilities {
	return kv.KVCapabilities{}
}

func (p *Provider) RenderPodPatch(ep *opencodav1alpha1.CodaEndpoint, node engine.NodeProfile) (engine.PodPatch, error) {
	return engine.PodPatch{}, nil
}

func (p *Provider) OnRestore(ctx context.Context, replica kv.ReplicaRef) error {
	return nil
}

func (p *Provider) Fingerprint(tokens []int) (uint64, bool) {
	return 0, false
}

func (p *Provider) Metrics() []prometheus.Collector {
	return nil
}
