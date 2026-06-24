package main

import (
	"context"
	"flag"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/capacityfactory"
	"github.com/immanuel-peter/opencoda/internal/controller/buffer"
	"github.com/immanuel-peter/opencoda/internal/controller/endpoint"
	"github.com/immanuel-peter/opencoda/internal/controller/health"
	"github.com/immanuel-peter/opencoda/internal/controller/pool"
	"github.com/immanuel-peter/opencoda/internal/metrics"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/pricesync"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opencodav1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var garageEndpoint string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Health probe bind address")
	flag.StringVar(&garageEndpoint, "garage-endpoint", "http://garage.opencoda-system.svc:3900", "Garage S3 endpoint")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	cfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	metrics.Register(prometheus.DefaultRegisterer)

	boot := capacity.BootstrapConfig{
		APIServerURL: cfg.Host,
		JoinToken:    os.Getenv("CODA_JOIN_TOKEN"),
	}
	providers := capacity.NewProviderCache(mgr.GetClient(), boot, capacityfactory.NewRegistry())

	priceJob := pricesync.NewSyncJob()
	go func() {
		if err := priceJob.Start(context.Background()); err != nil {
			setupLog.Error(err, "pricesync stopped")
		}
	}()

	if err := pool.NewReconciler(mgr.GetClient(), mgr.GetScheme(), providers).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "pool controller")
		os.Exit(1)
	}
	if err := buffer.NewReconciler(mgr.GetClient(), mgr.GetScheme(), providers).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "buffer controller")
		os.Exit(1)
	}
	if err := endpoint.NewReconciler(mgr.GetClient(), mgr.GetScheme(), garageEndpoint).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "endpoint controller")
		os.Exit(1)
	}
	checker := health.Checker(&health.ExecChecker{})
	if os.Getenv("CODA_FAKE_HEALTH") == "1" {
		checker = &health.FakeChecker{}
	}
	if err := health.NewReconciler(mgr.GetClient(), mgr.GetScheme(), checker, providers).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "health controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "readyz")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped")
		os.Exit(1)
	}
}
