package engine

import (
	corev1 "k8s.io/api/core/v1"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// NodeProfile describes pinned memory and cache dirs on a GPU node.
type NodeProfile struct {
	PinnedMemoryGiB float64
	NVMeCacheDir    string
}

// PodPatch is a partial pod overlay from KV or other plugins.
type PodPatch struct {
	Containers []corev1.Container
	InitContainers []corev1.Container
	Env        []corev1.EnvVar
	Volumes    []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

// Engine renders inference engine pods for a CodaEndpoint.
type Engine interface {
	Name() string
	RenderPodSpec(ep *opencodav1alpha1.CodaEndpoint, node NodeProfile, kvPatch PodPatch) (*corev1.PodSpec, error)
	ReadinessProbe() *corev1.Probe
	MetricsEndpoint() string
	ServedModelID(ep *opencodav1alpha1.CodaEndpoint) string
}

// Registry holds engine implementations.
type Registry struct {
	engines map[string]Engine
}

func NewRegistry() *Registry {
	return &Registry{engines: make(map[string]Engine)}
}

func (r *Registry) Register(e Engine) {
	r.engines[e.Name()] = e
}

func (r *Registry) Get(name string) (Engine, bool) {
	e, ok := r.engines[name]
	return e, ok
}
