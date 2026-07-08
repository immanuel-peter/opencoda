package main

import (
	"flag"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/gateway"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opencodav1alpha1.AddToScheme(scheme))
}

func main() {
	var addr string
	flag.StringVar(&addr, "addr", ":8090", "listen address")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	if err != nil {
		log.Fatal(err)
	}

	client := mgr.GetClient()
	router := gateway.NewRouter()
	autoscaler := gateway.NewAutoscaler(router)
	syncer := gateway.NewK8sSync(client, router, autoscaler)
	validator := gateway.NewTokenValidator(client)
	auth := gateway.NewTokenAuth(validator)
	srv := gateway.NewServer(addr, auth)
	srv.SetRouter(router)
	srv.SetK8sSync(syncer)

	ctx := ctrl.SetupSignalHandler()
	go syncer.RunAutoscalerLoop(ctx, 10*time.Second)

	go func() {
		log.Printf("coda-gateway listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	if err := mgr.Start(ctx); err != nil {
		log.Fatal(err)
	}
}
