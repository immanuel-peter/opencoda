package endpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/constants"
	"github.com/immanuel-peter/opencoda/internal/metrics"
	"github.com/immanuel-peter/opencoda/pkg/engine"
	"github.com/immanuel-peter/opencoda/pkg/engine/vllm"
	"github.com/immanuel-peter/opencoda/pkg/kv"
	"github.com/immanuel-peter/opencoda/pkg/kv/lmcache"
	"github.com/immanuel-peter/opencoda/pkg/kv/null"
)

const rolloutCondition = "RolloutProgressing"

// Reconciler manages CodaEndpoint replica pods.
type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Engines        *engine.Registry
	KVProviders    *kv.Registry
	GarageEndpoint string
	idleSince      map[string]time.Time
	coldSamples    map[string][]float64
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, garageEndpoint string) *Reconciler {
	engines := engine.NewRegistry()
	engines.Register(vllm.New(os.Getenv("CODA_ENGINE_IMAGE")))

	kvReg := kv.NewRegistry()
	kvReg.Register(lmcache.New(garageEndpoint))
	kvReg.Register(null.New())

	return &Reconciler{
		Client:         c,
		Scheme:         scheme,
		Engines:        engines,
		KVProviders:    kvReg,
		GarageEndpoint: garageEndpoint,
		idleSince:      make(map[string]time.Time),
		coldSamples:    make(map[string][]float64),
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ep opencodav1alpha1.CodaEndpoint
	if err := r.Get(ctx, req.NamespacedName, &ep); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := vllm.ValidateEngineType(ep.Spec.Engine.Type); err != nil {
		logger.Error(err, "invalid engine")
		return ctrl.Result{}, nil
	}

	specHash := r.computeSpecHash(&ep)
	desired := r.desiredReplicas(ctx, &ep)
	ready, starting := r.countReplicaPods(ctx, &ep, specHash)
	oldReady, _ := r.countReplicaPods(ctx, &ep, "")

	ep.Status.Replicas.Ready = ready + oldReady
	ep.Status.Replicas.Starting = starting
	r.setRolloutCondition(&ep, specHash, ready, oldReady, desired)
	r.recordColdStarts(ctx, &ep)

	if err := r.Status().Update(ctx, &ep); err != nil {
		return ctrl.Result{}, err
	}

	if desired == 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for i := ready + starting; i < desired; i++ {
		pod, err := r.buildPod(&ep, i, specHash)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, pod); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
		}
	}

	if ready >= desired && oldReady > 0 {
		r.drainOldHashPods(ctx, &ep, specHash)
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *Reconciler) computeSpecHash(ep *opencodav1alpha1.CodaEndpoint) string {
	raw := fmt.Sprintf("%s|%s|%s|%s",
		ep.Spec.Engine.Version,
		ep.Spec.Model.Source,
		ep.Spec.Model.Quantization,
		strings.Join(ep.Spec.Engine.Args, ","),
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func (r *Reconciler) desiredReplicas(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint) int {
	key := ep.Namespace + "/" + ep.Name
	if ann, ok := ep.Annotations[constants.AnnotationDesiredReplicas]; ok {
		if n, err := strconv.Atoi(ann); err == nil {
			if n == 0 && ep.Spec.Scaling.ScaleToZeroAfter != "" {
				d, err := time.ParseDuration(ep.Spec.Scaling.ScaleToZeroAfter)
				if err == nil {
					if _, ok := r.idleSince[key]; !ok {
						r.idleSince[key] = time.Now()
					}
					if time.Since(r.idleSince[key]) < d {
						return ep.Spec.Scaling.MinReplicas
					}
				}
			} else {
				delete(r.idleSince, key)
				if n < ep.Spec.Scaling.MinReplicas {
					return ep.Spec.Scaling.MinReplicas
				}
				if n > ep.Spec.Scaling.MaxReplicas {
					return ep.Spec.Scaling.MaxReplicas
				}
				return n
			}
		}
	}
	if ep.Spec.Scaling.MinReplicas > 0 {
		return ep.Spec.Scaling.MinReplicas
	}
	return 0
}

func (r *Reconciler) countReplicaPods(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint, specHash string) (ready, total int) {
	current := r.computeSpecHash(ep)
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(ep.Namespace), client.MatchingLabels{
		constants.LabelEndpoint: ep.Name,
	}); err != nil {
		return 0, 0
	}
	for _, p := range pods.Items {
		podHash := p.Annotations[constants.AnnotationSpecHash]
		if specHash != "" {
			if podHash != specHash {
				continue
			}
		} else {
			if podHash == current {
				continue
			}
		}
		total++
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}
	return ready, total
}

func (r *Reconciler) setRolloutCondition(ep *opencodav1alpha1.CodaEndpoint, specHash string, newReady, oldReady, desired int) {
	status := metav1.ConditionFalse
	msg := "rollout complete"
	if oldReady > 0 {
		status = metav1.ConditionTrue
		msg = "draining old generation"
	} else if newReady < desired {
		status = metav1.ConditionTrue
		msg = "surging new generation"
	}
	cond := metav1.Condition{
		Type:               rolloutCondition,
		Status:             status,
		Reason:             "RollingUpdate",
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
	ep.Status.Conditions = []metav1.Condition{cond}
}

func (r *Reconciler) drainOldHashPods(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint, specHash string) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(ep.Namespace), client.MatchingLabels{
		constants.LabelEndpoint: ep.Name,
	}); err != nil {
		return
	}
	for _, p := range pods.Items {
		if p.Annotations[constants.AnnotationSpecHash] == specHash || p.Annotations[constants.AnnotationSpecHash] == "" {
			continue
		}
		_ = r.Delete(ctx, &p)
	}
}

func (r *Reconciler) buildPod(ep *opencodav1alpha1.CodaEndpoint, index int, specHash string) (*corev1.Pod, error) {
	eng, ok := r.Engines.Get(ep.Spec.Engine.Type)
	if !ok {
		return nil, fmt.Errorf("engine %q not registered", ep.Spec.Engine.Type)
	}

	nodeProfile := engine.NodeProfile{PinnedMemoryGiB: 8, NVMeCacheDir: "/var/cache/opencoda"}

	kvProv := r.selectKV(ep)
	kvPatch, err := kvProv.RenderPodPatch(ep, nodeProfile)
	if err != nil {
		return nil, err
	}

	podSpec, err := eng.RenderPodSpec(ep, nodeProfile, kvPatch)
	if err != nil {
		return nil, err
	}

	if ep.Spec.KV.LMCache != nil && ep.Spec.KV.LMCache.Enabled {
		for i := range podSpec.Containers {
			if podSpec.Containers[i].Name == "vllm" {
				podSpec.Containers[i].Args = append(podSpec.Containers[i].Args,
					"--kv-transfer-config",
					`{"kv_connector":"LMCacheMPConnector","kv_role":"kv_both","kv_connector_extra_config":{"lmcache.mp.host":"tcp://localhost","lmcache.mp.port":5555}}`,
				)
				break
			}
		}
	}

	r.applyGPUScheduling(ep, podSpec)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-replica-%d", ep.Name, index),
			Namespace: ep.Namespace,
			Labels: map[string]string{
				constants.LabelEndpoint:    ep.Name,
				"app.kubernetes.io/name":   "opencoda",
			},
			Annotations: map[string]string{
				constants.AnnotationSpecHash:     specHash,
				constants.AnnotationPodCreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
		Spec: *podSpec,
	}
	return pod, nil
}

func (r *Reconciler) applyGPUScheduling(ep *opencodav1alpha1.CodaEndpoint, podSpec *corev1.PodSpec) {
	if ep.Spec.Resources.GPU <= 0 {
		return
	}
	if podSpec.NodeSelector == nil {
		podSpec.NodeSelector = map[string]string{}
	}
	podSpec.NodeSelector[constants.LabelGPU] = "true"
	podSpec.Tolerations = append(podSpec.Tolerations, corev1.Toleration{
		Key:      "opencoda.io/gpu",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	})
	gpuQty := resource.MustParse(strconv.Itoa(ep.Spec.Resources.GPU))
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name != "vllm" {
			continue
		}
		if podSpec.Containers[i].Resources.Limits == nil {
			podSpec.Containers[i].Resources.Limits = corev1.ResourceList{}
		}
		if podSpec.Containers[i].Resources.Requests == nil {
			podSpec.Containers[i].Resources.Requests = corev1.ResourceList{}
		}
		podSpec.Containers[i].Resources.Limits[corev1.ResourceName("nvidia.com/gpu")] = gpuQty
		podSpec.Containers[i].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")] = gpuQty
	}
}

func (r *Reconciler) selectKV(ep *opencodav1alpha1.CodaEndpoint) kv.KVProvider {
	if ep.Spec.KV.LMCache != nil && ep.Spec.KV.LMCache.Enabled {
		p, ok := r.KVProviders.Get(lmcache.ProviderName)
		if ok {
			return p
		}
	}
	p, _ := r.KVProviders.Get(null.ProviderName)
	return p
}

func (r *Reconciler) recordColdStarts(ctx context.Context, ep *opencodav1alpha1.CodaEndpoint) {
	key := ep.Namespace + "/" + ep.Name
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(ep.Namespace), client.MatchingLabels{
		constants.LabelEndpoint: ep.Name,
	}); err != nil {
		return
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Annotations[constants.AnnotationColdStartRecorded] == "true" {
			continue
		}
		ready := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		createdAt := pod.Annotations[constants.AnnotationPodCreatedAt]
		if createdAt == "" {
			createdAt = pod.CreationTimestamp.UTC().Format(time.RFC3339Nano)
		}
		start, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			start = pod.CreationTimestamp.Time
		}
		elapsed := time.Since(start).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		metrics.ColdStartSeconds.WithLabelValues("cold", ep.Name).Observe(elapsed)
		r.coldSamples[key] = append(r.coldSamples[key], elapsed)
		if len(r.coldSamples[key]) > 50 {
			r.coldSamples[key] = r.coldSamples[key][len(r.coldSamples[key])-50:]
		}
		ep.Status.ColdStart.P50Ms, ep.Status.ColdStart.P95Ms = percentileMs(r.coldSamples[key], 0.50, 0.95)

		patch := client.MergeFrom(pod.DeepCopy())
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[constants.AnnotationColdStartRecorded] = "true"
		_ = r.Patch(ctx, pod, patch)
	}
}

func percentileMs(samples []float64, ps ...float64) (int64, int64) {
	if len(samples) == 0 {
		return 0, 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	idx := func(p float64) int64 {
		if len(sorted) == 1 {
			return int64(sorted[0] * 1000)
		}
		rank := p * float64(len(sorted)-1)
		lo := int(rank)
		hi := lo + 1
		if hi >= len(sorted) {
			return int64(sorted[len(sorted)-1] * 1000)
		}
		frac := rank - float64(lo)
		val := sorted[lo]*(1-frac) + sorted[hi]*frac
		return int64(val * 1000)
	}
	p50 := idx(ps[0])
	p95 := idx(ps[1])
	return p50, p95
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opencodav1alpha1.CodaEndpoint{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
