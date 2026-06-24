package lmcache

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/engine"
	"github.com/immanuel-peter/opencoda/pkg/kv"
)

const ProviderName = "lmcache"

const mpPort = 5555

// Provider wires LMCache MP mode for vLLM endpoints.
type Provider struct {
	garageEndpoint string
}

func New(garageEndpoint string) *Provider {
	return &Provider{garageEndpoint: garageEndpoint}
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) Capabilities() kv.KVCapabilities {
	return kv.KVCapabilities{
		WarmRestore:      true,
		AffinityHints:    true,
		SharedRemoteTier: true,
		TierSpill:        true,
	}
}

func (p *Provider) RenderPodPatch(ep *opencodav1alpha1.CodaEndpoint, node engine.NodeProfile) (engine.PodPatch, error) {
	if ep.Spec.KV.LMCache == nil || !ep.Spec.KV.LMCache.Enabled {
		return engine.PodPatch{}, nil
	}

	remoteEndpoint := ep.Spec.KV.LMCache.RemoteRef.Endpoint
	if remoteEndpoint == "" {
		remoteEndpoint = p.garageEndpoint
	}

	lmcacheServer := corev1.Container{
		Name:  "lmcache-server",
		Image: "lmcache/lmcache:latest",
		Command: []string{"lmcache", "server"},
		Args: []string{
			"--host", "0.0.0.0",
			"--port", fmt.Sprintf("%d", mpPort),
		},
		Env: []corev1.EnvVar{
			{Name: "LMCACHE_CHUNK_SIZE", Value: "256"},
			{Name: "PYTHONHASHSEED", Value: "0"},
		},
		Ports: []corev1.ContainerPort{{
			Name:          "mp",
			ContainerPort: int32(mpPort),
		}},
	}

	vllmEnv := []corev1.EnvVar{
		{Name: "PYTHONHASHSEED", Value: "0"},
		{Name: "LMCACHE_CHUNK_SIZE", Value: "256"},
	}

	kvTransfer := fmt.Sprintf(
		`{"kv_connector":"LMCacheMPConnector","kv_role":"kv_both","kv_connector_extra_config":{"lmcache.mp.host":"tcp://localhost","lmcache.mp.port":%d}}`,
		mpPort,
	)

	// vLLM receives kv-transfer-config via engine args in endpoint controller;
	// env documents LMCache settings for local tiers.
	_ = kvTransfer
	_ = remoteEndpoint

	return engine.PodPatch{
		Containers: []corev1.Container{lmcacheServer},
		Env:        vllmEnv,
	}, nil
}

func (p *Provider) OnRestore(ctx context.Context, replica kv.ReplicaRef) error {
	return nil
}

func (p *Provider) Fingerprint(tokens []int) (uint64, bool) {
	if len(tokens) == 0 {
		return 0, false
	}
	h := fnv.New64a()
	for _, t := range tokens {
		var b [4]byte
		b[0] = byte(t >> 24)
		b[1] = byte(t >> 16)
		b[2] = byte(t >> 8)
		b[3] = byte(t)
		h.Write(b[:])
	}
	return h.Sum64(), true
}

func (p *Provider) Metrics() []prometheus.Collector {
	return nil
}
