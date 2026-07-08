package capacity

import (
	"context"
	"fmt"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// BootstrapConfig is cluster-wide join configuration for node bootstrap.
type BootstrapConfig struct {
	APIServerURL string
	CABundle     string
	JoinToken    string
	ClusterName  string
}

// Factory constructs a CapacityProvider for a GPUPool.
type Factory func(ctx context.Context, pool *opencodav1alpha1.GPUPool, creds map[string]string, boot BootstrapConfig) (CapacityProvider, error)

// FactoryRegistry maps provider names to factories.
type FactoryRegistry struct {
	factories map[string]Factory
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{factories: make(map[string]Factory)}
}

func (r *FactoryRegistry) Register(name string, f Factory) {
	r.factories[name] = f
}

func (r *FactoryRegistry) ForPool(ctx context.Context, pool *opencodav1alpha1.GPUPool, creds map[string]string, boot BootstrapConfig) (CapacityProvider, error) {
	f, ok := r.factories[pool.Spec.Provider.Name]
	if !ok {
		return nil, fmt.Errorf("unknown capacity provider %q", pool.Spec.Provider.Name)
	}
	return f(ctx, pool, creds, boot)
}
