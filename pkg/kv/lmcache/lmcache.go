package lmcache

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/engine"
	"github.com/immanuel-peter/opencoda/pkg/kv"
)

const ProviderName = "lmcache"

const (
	mpPort          = 5555
	lmcacheImage    = "lmcache/standalone:v0.5.0"
	credentialsName = "garage-s3-credentials"
	garageAccessKey = "opencoda"
	garageSecretKey = "opencoda-garage-secret"
)

type l2AdapterSpec struct {
	Type              string  `json:"type"`
	S3Endpoint        string  `json:"s3_endpoint"`
	S3Region          string  `json:"s3_region"`
	DisableTLS        bool    `json:"disable_tls"`
	S3PreferHTTP2     bool    `json:"s3_prefer_http2"`
	S3EnableS3Express bool    `json:"s3_enable_s3express"`
	MaxCapacityGB     float64 `json:"max_capacity_gb"`
	AWSAccessKeyID     string `json:"aws_access_key_id,omitempty"`
	AWSSecretAccessKey string `json:"aws_secret_access_key,omitempty"`
}

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
	bucket := ep.Spec.KV.LMCache.RemoteRef.Bucket
	if bucket == "" {
		bucket = "coda-kv"
	}

	s3Host := strings.TrimPrefix(remoteEndpoint, "http://")
	s3Host = strings.TrimPrefix(s3Host, "https://")

	l2 := l2AdapterSpec{
		Type:              "s3",
		S3Endpoint:        s3Host,
		S3Region:          "us-east-1",
		DisableTLS:        strings.HasPrefix(remoteEndpoint, "http://"),
		S3PreferHTTP2:     false,
		S3EnableS3Express: false,
		MaxCapacityGB:     8,
		AWSAccessKeyID:     garageAccessKey,
		AWSSecretAccessKey: garageSecretKey,
	}
	l2JSON, err := json.Marshal(l2)
	if err != nil {
		return engine.PodPatch{}, err
	}

	lmcacheServer := corev1.Container{
		Name:  "lmcache-server",
		Image: lmcacheImage,
		Command: []string{"lmcache", "server"},
		Args: []string{
			"--host", "0.0.0.0",
			"--port", fmt.Sprintf("%d", mpPort),
			"--l1-size-gb", "4",
			"--eviction-policy", "LRU",
			"--l2-adapter", string(l2JSON),
		},
		Env: []corev1.EnvVar{
			{Name: "LMCACHE_CHUNK_SIZE", Value: "256"},
			{Name: "PYTHONHASHSEED", Value: "0"},
			{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: credentialsName},
						Key:                  "AWS_ACCESS_KEY_ID",
						Optional:             boolPtr(true),
					},
				},
			},
			{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: credentialsName},
						Key:                  "AWS_SECRET_ACCESS_KEY",
						Optional:             boolPtr(true),
					},
				},
			},
		},
		Ports: []corev1.ContainerPort{{
			Name:          "mp",
			ContainerPort: int32(mpPort),
		}},
	}

	vllmEnv := []corev1.EnvVar{
		{Name: "PYTHONHASHSEED", Value: "0"},
		{Name: "LMCACHE_CHUNK_SIZE", Value: "256"},
		{Name: "LMCACHE_REMOTE_URL", Value: fmt.Sprintf("s3://%s", bucket)},
		{Name: "LMCACHE_REMOTE_SERDE", Value: "naive"},
	}

	return engine.PodPatch{
		Containers: []corev1.Container{lmcacheServer},
		Env:        vllmEnv,
	}, nil
}

func boolPtr(v bool) *bool { return &v }

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
