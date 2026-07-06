package pool

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/constants"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
)

const RequeueInterval = 30 * time.Second

// Reconciler updates GPUPool observed capacity and reconciles node records.
type Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Providers *capacity.ProviderCache
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, providers *capacity.ProviderCache) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme, Providers: providers}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pool opencodav1alpha1.GPUPool
	if err := r.Get(ctx, req.NamespacedName, &pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	provider, err := r.Providers.ForPool(ctx, &pool)
	if err != nil {
		logger.Error(err, "provider init failed")
		return ctrl.Result{RequeueAfter: RequeueInterval}, nil
	}

	report, err := provider.Capacity(ctx, pool.Name)
	if err != nil {
		logger.Error(err, "capacity report failed")
	} else {
		pool.Status.ObservedCapacity.Available = report.Available
		pool.Status.ObservedCapacity.ObservedHourlyUSD = report.ObservedHourlyUSD
		if len(report.RecentICE) > 0 {
			last := report.RecentICE[len(report.RecentICE)-1]
			pool.Status.ObservedCapacity.LastICE = metav1.NewTime(last)
		}
	}

	r.reconcileNodeRecords(ctx, &pool)

	if err := r.Status().Update(ctx, &pool); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: RequeueInterval}, nil
}

func (r *Reconciler) reconcileNodeRecords(ctx context.Context, pool *opencodav1alpha1.GPUPool) {
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes, client.MatchingLabels{
		constants.LabelPool: pool.Name,
	}); err != nil {
		return
	}
	nodeByName := make(map[string]corev1.Node, len(nodes.Items))
	for _, n := range nodes.Items {
		nodeByName[n.Name] = n
	}

	known := make(map[string]struct{}, len(pool.Status.NodeRecords))
	for _, rec := range pool.Status.NodeRecords {
		if rec.NodeName != "" {
			known[rec.NodeName] = struct{}{}
		}
	}
	for name := range nodeByName {
		if _, ok := known[name]; ok {
			continue
		}
		pool.Status.NodeRecords = append(pool.Status.NodeRecords, opencodav1alpha1.NodeRecord{
			ProviderID: fmt.Sprintf("static://%s/%s", pool.Name, name),
			NodeName:   name,
			State:      "discovered",
			PoolName:   pool.Name,
		})
	}

	active, buffered, provisioning := 0, 0, 0
	for i := range pool.Status.NodeRecords {
		rec := &pool.Status.NodeRecords[i]
		if rec.NodeName != "" {
			if n, ok := nodeByName[rec.NodeName]; ok {
				if n.Spec.Unschedulable {
					rec.State = "draining"
				} else if n.Labels[constants.LabelBufferEligible] == "true" {
					rec.State = "buffered"
					buffered++
				} else {
					rec.State = "active"
					active++
				}
				continue
			}
		}
		if rec.State == "provisioning" {
			provisioning++
		}
	}
	pool.Status.Nodes.Active = active
	pool.Status.Nodes.Buffered = buffered
	pool.Status.Nodes.Provisioning = provisioning
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opencodav1alpha1.GPUPool{}).
		Complete(r)
}
