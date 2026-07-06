package buffer

import (
	"context"
	"fmt"
	"strings"
	"time"

	policyv1 "k8s.io/api/policy/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/constants"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/static"
	"github.com/immanuel-peter/opencoda/pkg/scheduler"
	"github.com/immanuel-peter/opencoda/pkg/scheduler/greedy"
)

const RequeueInterval = 15 * time.Second

// Reconciler maintains warm GPU buffer per BufferPolicy.
type Reconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Providers *capacity.ProviderCache
	Scheduler scheduler.Scheduler
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, providers *capacity.ProviderCache) *Reconciler {
	return &Reconciler{
		Client:    c,
		Scheme:    scheme,
		Providers: providers,
		Scheduler: greedy.New(),
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var policy opencodav1alpha1.BufferPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	current := r.countWarmGPUs(ctx, &policy)
	desired := r.desiredWarmGPUs(&policy)

	logger.Info("buffer reconcile", "desired", desired, "current", current)

	if desired > current {
		need := desired - current
		views := r.buildPoolViews(ctx, &policy)
		plan, err := r.Scheduler.Fill(need, views)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, entry := range plan.Entries {
			if err := r.provisionPool(ctx, &policy, entry.PoolName, entry.NodeCount); err != nil {
				if capacity.IsICE(err) {
					logger.Info("ICE during provision", "pool", entry.PoolName)
					continue
				}
				logger.Error(err, "provision failed", "pool", entry.PoolName)
			}
		}
	} else if desired < current {
		excess := current - desired
		if err := r.scaleDown(ctx, &policy, excess); err != nil {
			logger.Error(err, "scale down failed")
		}
	}

	policy.Status.CurrentWarmGPUs = r.countWarmGPUs(ctx, &policy)
	if err := r.Status().Update(ctx, &policy); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: RequeueInterval}, nil
}

func (r *Reconciler) countWarmGPUs(ctx context.Context, policy *opencodav1alpha1.BufferPolicy) int {
	total := 0
	for _, ref := range policy.Spec.Pools {
		var pool opencodav1alpha1.GPUPool
		if err := r.Get(ctx, client.ObjectKey{Name: ref.Name}, &pool); err != nil {
			continue
		}
		total += pool.Status.Nodes.Buffered + pool.Status.Nodes.Provisioning
	}
	return total
}

func (r *Reconciler) desiredWarmGPUs(policy *opencodav1alpha1.BufferPolicy) int {
	t := policy.Spec.Target
	if t.Mode == "dynamic" && t.Dynamic != nil {
		base := t.MinWarmGPUs
		demand := policy.Status.DemandEWMA
		if demand > 0 && t.Dynamic.K > 0 {
			base += int(t.Dynamic.K * demand)
		}
		if base > t.MaxWarmGPUs {
			return t.MaxWarmGPUs
		}
		if base < t.MinWarmGPUs {
			return t.MinWarmGPUs
		}
		return base
	}
	if t.MinWarmGPUs > 0 {
		return t.MinWarmGPUs
	}
	return 0
}

func (r *Reconciler) buildPoolViews(ctx context.Context, policy *opencodav1alpha1.BufferPolicy) []scheduler.PoolView {
	var views []scheduler.PoolView
	for _, ref := range policy.Spec.Pools {
		var pool opencodav1alpha1.GPUPool
		if err := r.Get(ctx, client.ObjectKey{Name: ref.Name}, &pool); err != nil {
			continue
		}
		penalty := 1.0
		if pool.Status.ObservedCapacity.LastICE.Time.After(time.Now().Add(-15 * time.Minute)) {
			penalty = 2.0
		}
		views = append(views, scheduler.PoolView{
			Name:              pool.Name,
			Priority:          pool.Spec.Priority,
			ObservedHourlyUSD: pool.Status.ObservedCapacity.ObservedHourlyUSD,
			ICEPenalty:        penalty,
			MaxNodes:          pool.Spec.Limits.MaxNodes,
			CurrentNodes:      pool.Status.Nodes.Active + pool.Status.Nodes.Buffered + pool.Status.Nodes.Provisioning,
			Available:         pool.Status.ObservedCapacity.Available,
		})
	}
	return views
}

func (r *Reconciler) spendAllowed(pool *opencodav1alpha1.GPUPool, addNodes int) bool {
	max := pool.Spec.Limits.MaxHourlyUSD
	if max <= 0 {
		return true
	}
	current := pool.Status.Nodes.Active + pool.Status.Nodes.Buffered + pool.Status.Nodes.Provisioning
	hourly := pool.Status.ObservedCapacity.ObservedHourlyUSD
	if hourly <= 0 {
		hourly = 1
	}
	return hourly*float64(current+addNodes) <= max
}

func (r *Reconciler) provisionPool(ctx context.Context, policy *opencodav1alpha1.BufferPolicy, poolName string, count int) error {
	var pool opencodav1alpha1.GPUPool
	if err := r.Get(ctx, client.ObjectKey{Name: poolName}, &pool); err != nil {
		return err
	}
	if !r.spendAllowed(&pool, count) {
		return fmt.Errorf("maxHourlyUSD ceiling for pool %s", poolName)
	}
	if pool.Spec.Provider.Name == static.ProviderName {
		var nodes corev1.NodeList
		if err := r.List(ctx, &nodes, client.MatchingLabels{constants.LabelPool: poolName}); err == nil {
			if len(nodes.Items) >= count {
				return nil
			}
		}
	}
	provider, err := r.Providers.ForPool(ctx, &pool)
	if err != nil {
		return err
	}
	req := capacity.GPURequest{
		PoolName:      poolName,
		GPUType:       pool.Spec.GPU.Type,
		GPUCount:      pool.Spec.GPU.PerNode,
		NodeCount:     count,
		InstanceTypes: pool.Spec.InstanceTypes,
		Subnets:       splitSubnets(pool.Spec.Provider.Params["subnets"]),
		Constraints: capacity.Constraints{
			Region:       pool.Spec.Provider.Params["region"],
			Zone:         pool.Spec.Provider.Params["zone"],
			CapacityType: pool.Spec.Provider.Params["capacityType"],
			MaxHourlyUSD: pool.Spec.Limits.MaxHourlyUSD,
		},
	}
	offers, err := provider.Quote(ctx, req)
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		offer := offers[0]
		if i < len(offers) {
			offer = offers[i]
		}
		handle, err := provider.Provision(ctx, offer)
		if err != nil {
			return err
		}
		pool.Status.NodeRecords = append(pool.Status.NodeRecords, opencodav1alpha1.NodeRecord{
			ProviderID: handle.ProviderID,
			NodeName:   handle.NodeName,
			State:      "provisioning",
			LaunchedAt: metav1.Time{Time: handle.LaunchedAt},
			PoolName:   poolName,
		})
		pool.Status.Nodes.Provisioning++
	}
	return r.Status().Update(ctx, &pool)
}

func splitSubnets(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (r *Reconciler) scaleDown(ctx context.Context, policy *opencodav1alpha1.BufferPolicy, excess int) error {
	stab := parseDuration(policy.Spec.ScaleDown.StabilizationWindow, 10*time.Minute)
	drainTimeout := parseDuration(policy.Spec.ScaleDown.DrainTimeout, 5*time.Minute)
	now := time.Now()

	for _, ref := range policy.Spec.Pools {
		var pool opencodav1alpha1.GPUPool
		if err := r.Get(ctx, client.ObjectKey{Name: ref.Name}, &pool); err != nil {
			continue
		}
		for i := range pool.Status.NodeRecords {
			if excess <= 0 {
				break
			}
			rec := &pool.Status.NodeRecords[i]
			if rec.State != "buffered" {
				continue
			}
			if rec.LaunchedAt.Time.Add(stab).After(now) {
				continue
			}
			if err := r.drainAndRelease(ctx, &pool, rec, drainTimeout); err != nil {
				return err
			}
			excess--
		}
		if err := r.Status().Update(ctx, &pool); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) drainAndRelease(ctx context.Context, pool *opencodav1alpha1.GPUPool, rec *opencodav1alpha1.NodeRecord, drainTimeout time.Duration) error {
	if rec.NodeName == "" {
		return nil
	}
	var node corev1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: rec.NodeName}, &node); err != nil {
		return client.IgnoreNotFound(err)
	}
	node.Spec.Unschedulable = true
	if err := r.Update(ctx, &node); err != nil {
		return err
	}
	var pods corev1.PodList
	if err := r.List(ctx, &pods); err == nil {
		for _, pod := range pods.Items {
			if pod.Spec.NodeName != rec.NodeName {
				continue
			}
			if pod.Namespace == "kube-system" {
				continue
			}
			_ = r.SubResource("eviction").Create(ctx, &pod, &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace},
			})
		}
	}
	time.Sleep(drainTimeout)

	provider, err := r.Providers.ForPool(ctx, pool)
	if err != nil {
		return err
	}
	handle := &capacity.NodeHandle{
		ProviderID: rec.ProviderID,
		NodeName:   rec.NodeName,
	}
	if err := provider.Release(ctx, handle); err != nil {
		return err
	}
	rec.State = "released"
	pool.Status.Nodes.Buffered--
	return nil
}

func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opencodav1alpha1.BufferPolicy{}).
		Complete(r)
}
