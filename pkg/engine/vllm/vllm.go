package vllm

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/engine"
)

const EngineName = "vllm"

const defaultPort = 8000

// Engine implements vLLM serving (v1 only).
type Engine struct {
	Image string
}

func New(image string) *Engine {
	if image == "" {
		image = "vllm/vllm-openai:latest"
	}
	return &Engine{Image: image}
}

func (e *Engine) Name() string { return EngineName }

func (e *Engine) ServedModelID(ep *opencodav1alpha1.CodaEndpoint) string {
	return ep.Spec.Model.Source
}

func (e *Engine) MetricsEndpoint() string {
	return "/metrics"
}

func (e *Engine) ReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromInt(defaultPort),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
	}
}

func (e *Engine) RenderPodSpec(ep *opencodav1alpha1.CodaEndpoint, node engine.NodeProfile, kvPatch engine.PodPatch) (*corev1.PodSpec, error) {
	args := []string{
		"serve",
		ep.Spec.Model.Source,
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", defaultPort),
	}
	if ep.Spec.Resources.GPU > 1 {
		args = append(args, "--tensor-parallel-size", fmt.Sprintf("%d", ep.Spec.Resources.GPU))
	}
	for _, a := range ep.Spec.Engine.Args {
		args = append(args, a)
	}

	env := []corev1.EnvVar{
		{Name: "PYTHONHASHSEED", Value: "0"},
	}
	env = append(env, kvPatch.Env...)

	container := corev1.Container{
		Name:  "vllm",
		Image: e.Image,
		Env:   env,
		Ports: []corev1.ContainerPort{{
			Name:          "http",
			ContainerPort: int32(defaultPort),
		}},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{},
		},
		VolumeMounts:   kvPatch.VolumeMounts,
		ReadinessProbe: e.ReadinessProbe(),
	}
	if !strings.Contains(strings.ToLower(e.Image), "fakevllm") {
		container.Command = []string{"vllm"}
		container.Args = args
	}

	spec := &corev1.PodSpec{
		Containers:     []corev1.Container{container},
		InitContainers: kvPatch.InitContainers,
		Volumes:        kvPatch.Volumes,
	}

	// Merge additional sidecars from KV (e.g. lmcache server)
	if len(kvPatch.Containers) > 0 {
		spec.Containers = append(spec.Containers, kvPatch.Containers...)
	}

	return spec, nil
}

// ValidateEngineType returns an error if engine type is not supported in v1.
func ValidateEngineType(t string) error {
	if strings.ToLower(t) != EngineName {
		return fmt.Errorf("engine type %q not supported in v1 (only vllm)", t)
	}
	return nil
}
