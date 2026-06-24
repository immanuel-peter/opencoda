package capacity

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// ProviderCache constructs and caches per-pool CapacityProviders from GPUPool credentials.
type ProviderCache struct {
	mu        sync.Mutex
	factories *FactoryRegistry
	boot      BootstrapConfig
	client    client.Client
	cache     map[string]CapacityProvider
}

func NewProviderCache(c client.Client, boot BootstrapConfig, factories *FactoryRegistry) *ProviderCache {
	return &ProviderCache{
		factories: factories,
		boot:      boot,
		client:    c,
		cache:     make(map[string]CapacityProvider),
	}
}

func (p *ProviderCache) ForPool(ctx context.Context, pool *opencodav1alpha1.GPUPool) (CapacityProvider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if prov, ok := p.cache[pool.Name]; ok {
		return prov, nil
	}
	creds, err := p.loadCredentials(ctx, pool)
	if err != nil {
		return nil, err
	}
	prov, err := p.factories.ForPool(ctx, pool, creds, p.boot)
	if err != nil {
		return nil, err
	}
	p.cache[pool.Name] = prov
	return prov, nil
}

func (p *ProviderCache) loadCredentials(ctx context.Context, pool *opencodav1alpha1.GPUPool) (map[string]string, error) {
	ref := pool.Spec.Provider.CredentialsRef
	if ref.SecretName == "" {
		return map[string]string{}, nil
	}
	var secret corev1.Secret
	key := client.ObjectKey{Namespace: "opencoda-system", Name: ref.SecretName}
	if err := p.client.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("credentials secret %s: %w", ref.SecretName, err)
	}
	out := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		out[k] = string(v)
	}
	return out, nil
}
