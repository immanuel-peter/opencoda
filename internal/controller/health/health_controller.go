package health

import (
	"context"
	"os/exec"
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

// Checker runs GPU health diagnostics.
type Checker interface {
	BootCheck(ctx context.Context) error
	DeepCheck(ctx context.Context) error
}

// ExecChecker uses dcgmi when available.
type ExecChecker struct{}

func (e *ExecChecker) BootCheck(ctx context.Context) error {
	if _, err := exec.LookPath("dcgmi"); err != nil {
		return nil
	}
	return exec.CommandContext(ctx, "dcgmi", "diag", "-r", "1").Run()
}

func (e *ExecChecker) DeepCheck(ctx context.Context) error {
	if _, err := exec.LookPath("dcgmi"); err != nil {
		return nil
	}
	return exec.CommandContext(ctx, "dcgmi", "diag", "-r", "3").Run()
}

// FakeChecker always passes (CI).
type FakeChecker struct{}

func (f *FakeChecker) BootCheck(ctx context.Context) error { return nil }
func (f *FakeChecker) DeepCheck(ctx context.Context) error { return nil }

// Reconciler marks GPU nodes buffer-eligible and handles Xid-critical automation.
type Reconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Checker   Checker
	Providers *capacity.ProviderCache
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, checker Checker, providers *capacity.ProviderCache) *Reconciler {
	return &Reconciler{Client: c, Scheme: scheme, Checker: checker, Providers: providers}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if _, ok := node.Labels[constants.LabelGPU]; !ok {
		return ctrl.Result{}, nil
	}

	if node.Annotations[constants.AnnotationXidCritical] == "true" {
		return r.handleXidCritical(ctx, &node)
	}

	if node.Labels[constants.LabelBufferEligible] != "true" {
		if err := r.Checker.BootCheck(ctx); err != nil {
			logger.Error(err, "boot check failed", "node", node.Name)
			node.Labels[constants.LabelBufferEligible] = "false"
		} else {
			if node.Labels == nil {
				node.Labels = map[string]string{}
			}
			node.Labels[constants.LabelBufferEligible] = "true"
		}
		if err := r.Update(ctx, &node); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Periodic deep check on idle buffered nodes
	if node.Labels[constants.LabelBufferEligible] == "true" && !node.Spec.Unschedulable {
		if err := r.Checker.DeepCheck(ctx); err != nil {
			logger.Error(err, "deep check failed", "node", node.Name)
		}
	}

	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

func (r *Reconciler) handleXidCritical(ctx context.Context, node *corev1.Node) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	poolName := node.Labels[constants.LabelPool]
	node.Spec.Unschedulable = true
	if err := r.Update(ctx, node); err != nil {
		return ctrl.Result{}, err
	}

	var pods corev1.PodList
	if err := r.List(ctx, &pods); err == nil {
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node.Name && pod.Namespace != "kube-system" {
				_ = r.Delete(ctx, &pod)
			}
		}
	}

	if poolName != "" {
		var pool opencodav1alpha1.GPUPool
		if err := r.Get(ctx, client.ObjectKey{Name: poolName}, &pool); err == nil {
			provider, err := r.Providers.ForPool(ctx, &pool)
			if err == nil {
				for i := range pool.Status.NodeRecords {
					rec := &pool.Status.NodeRecords[i]
					if rec.NodeName == node.Name {
						handle := &capacity.NodeHandle{ProviderID: rec.ProviderID, NodeName: node.Name}
						if err := provider.Release(ctx, handle); err != nil {
							logger.Error(err, "release after xid")
						}
						rec.State = "released"
					}
				}
				pool.Status.ObservedCapacity.LastICE = metav1.Time{Time: time.Now()}
				_ = r.Status().Update(ctx, &pool)
			}
		}
	}

	delete(node.Annotations, constants.AnnotationXidCritical)
	node.Labels[constants.LabelBufferEligible] = "false"
	if err := r.Update(ctx, node); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
