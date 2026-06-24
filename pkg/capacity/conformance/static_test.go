package conformance

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/static"
)

func TestStaticProviderIdempotentProvision(t *testing.T) {
	ctx := context.Background()
	pool := &opencodav1alpha1.GPUPool{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	p, err := static.NewFactory()(ctx, pool, nil, capacity.BootstrapConfig{})
	if err != nil {
		t.Fatal(err)
	}
	offers, err := p.Quote(ctx, capacity.GPURequest{PoolName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	h1, err := p.Provision(ctx, offers[0])
	if err != nil {
		t.Fatal(err)
	}
	h2, err := p.Provision(ctx, offers[0])
	if err != nil {
		t.Fatal(err)
	}
	if h1.NodeName == h2.NodeName {
		t.Fatalf("expected distinct nodes on repeat provision")
	}
	if err := p.Release(ctx, h1); err != nil {
		t.Fatal(err)
	}
}
