package kv

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/engine"
)

// KVCapabilities advertises provider features.
type KVCapabilities struct {
	WarmRestore      bool
	AffinityHints    bool
	SharedRemoteTier bool
	TierSpill        bool
}

// ReplicaRef identifies a running replica for restore hooks.
type ReplicaRef struct {
	Namespace   string
	Endpoint    string
	PodName     string
	NodeName    string
}

// KVProvider is the control-plane KV boundary (§14.2).
type KVProvider interface {
	Name() string
	Capabilities() KVCapabilities
	RenderPodPatch(ep *opencodav1alpha1.CodaEndpoint, node engine.NodeProfile) (engine.PodPatch, error)
	OnRestore(ctx context.Context, replica ReplicaRef) error
	Fingerprint(tokens []int) (uint64, bool)
	Metrics() []prometheus.Collector
}

// Registry holds KV providers.
type Registry struct {
	providers map[string]KVProvider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]KVProvider)}
}

func (r *Registry) Register(p KVProvider) {
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (KVProvider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// PodPatchFromEngine re-exports engine.PodPatch for callers.
type PodPatch = engine.PodPatch

// EnvVar helper.
type EnvVar = corev1.EnvVar
