package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/constants"
	"github.com/immanuel-peter/opencoda/internal/metrics"
)

// K8sSync watches CodaEndpoints and Pods to populate router state and patch scaling annotations.
type K8sSync struct {
	client.Client
	router     *Router
	autoscaler *Autoscaler
	mu         sync.Mutex
	idleSince  map[string]time.Time
	kvHits     map[string]float64
}

func NewK8sSync(c client.Client, router *Router, autoscaler *Autoscaler) *K8sSync {
	return &K8sSync{
		Client:     c,
		router:     router,
		autoscaler: autoscaler,
		idleSince:  make(map[string]time.Time),
		kvHits:     make(map[string]float64),
	}
}

func (k *K8sSync) SyncEndpoint(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint) {
	logger := log.FromContext(ctx)
	var pods corev1.PodList
	if err := k.List(ctx, &pods, client.InNamespace(ep.Namespace), client.MatchingLabels{
		constants.LabelEndpoint: ep.Name,
	}); err != nil {
		logger.Error(err, "list pods")
		return
	}
	urls := make([]string, 0)
	ready := 0
	var kvHitRate float64
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			continue
		}
		readyCond := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				readyCond = true
				break
			}
		}
		if readyCond {
			urls = append(urls, fmt.Sprintf("http://%s:8000", pod.Status.PodIP))
			ready++
			if rate, ok := scrapeKVHitRate(ctx, pod.Status.PodIP); ok {
				kvHitRate = rate
			}
		}
	}
	modelID := ep.Spec.Model.Source
	k.router.RegisterEndpoint(ep.Name, modelID, urls)
	if kvHitRate > 0 {
		k.kvHits[ep.Name] = kvHitRate
		metrics.KVHitRate.WithLabelValues(ep.Name).Set(kvHitRate)
		k.patchEndpointKVHit(ctx, ep, kvHitRate)
	}

	inFlight := k.router.InFlight(ep.Name)
	queue := k.router.QueueDepth(ep.Name)
	desired := k.autoscaler.Evaluate(ep, inFlight, queue)
	k.patchDesiredReplicas(ctx, ep, desired)
	k.exportDemand(ctx, desired, inFlight+queue)
}

func (k *K8sSync) KVHitRate(endpoint string) float64 {
	return k.kvHits[endpoint]
}

func scrapeKVHitRate(ctx context.Context, podIP string) (float64, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:8000/metrics", podIP), nil)
	if err != nil {
		return 0, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	hits, queries, ok := parsePrefixCacheMetrics(resp.Body)
	if !ok || queries == 0 {
		return 0, false
	}
	return float64(hits) / float64(queries), true
}

func parsePrefixCacheMetrics(r io.Reader) (hits uint64, queries uint64, ok bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "vllm:gpu_prefix_cache_hits_total "):
			hits, _ = parseMetricCounter(line)
			ok = true
		case strings.HasPrefix(line, "vllm:gpu_prefix_cache_queries_total "):
			queries, _ = parseMetricCounter(line)
			ok = true
		case strings.HasPrefix(line, "coda_prefix_cache_hits_total "):
			hits, _ = parseMetricCounter(line)
			ok = true
		case strings.HasPrefix(line, "coda_prefix_cache_queries_total "):
			queries, _ = parseMetricCounter(line)
			ok = true
		}
	}
	return hits, queries, ok
}

func parseMetricCounter(line string) (uint64, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid metric line")
	}
	return strconv.ParseUint(parts[1], 10, 64)
}

func (k *K8sSync) patchEndpointKVHit(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint, rate float64) {
	fresh := ep.DeepCopy()
	if err := k.Get(ctx, types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace}, fresh); err != nil {
		return
	}
	fresh.Status.KVHitRate = rate
	if err := k.Status().Update(ctx, fresh); err != nil {
		log.FromContext(ctx).Error(err, "update kv hit rate")
	}
}

func (k *K8sSync) patchDesiredReplicas(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint, desired int) {
	patch := client.MergeFrom(ep.DeepCopy())
	if ep.Annotations == nil {
		ep.Annotations = map[string]string{}
	}
	ep.Annotations[constants.AnnotationDesiredReplicas] = fmt.Sprintf("%d", desired)
	if err := k.Patch(ctx, ep, patch); err != nil {
		log.FromContext(ctx).Error(err, "patch desired-replicas")
	}
}

func (k *K8sSync) exportDemand(ctx context.Context, desired, load int) {
	ewma := k.autoscaler.TotalDemand()
	if ewma == 0 {
		ewma = float64(load)
	}
	var policies opencodav1alpha1.BufferPolicyList
	if err := k.List(ctx, &policies); err != nil {
		return
	}
	for i := range policies.Items {
		p := &policies.Items[i]
		p.Status.DemandEWMA = ewma
		if err := k.Status().Update(ctx, p); err != nil {
			log.FromContext(ctx).Error(err, "update buffer demand")
		}
	}
}

func (k *K8sSync) RunAutoscalerLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var endpoints opencodav1alpha1.CodaEndpointList
			if err := k.List(ctx, &endpoints); err != nil {
				continue
			}
			for _, ep := range endpoints.Items {
				var fresh opencodav1alpha1.CodaEndpoint
				if err := k.Get(ctx, types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace}, &fresh); err != nil {
					continue
				}
				k.SyncEndpoint(ctx, &fresh)
			}
		}
	}
}
