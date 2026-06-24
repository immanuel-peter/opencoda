package capacityfactory

import (
	"github.com/immanuel-peter/opencoda/pkg/capacity"
	"github.com/immanuel-peter/opencoda/pkg/capacity/aws"
	"github.com/immanuel-peter/opencoda/pkg/capacity/gcp"
	"github.com/immanuel-peter/opencoda/pkg/capacity/static"
)

// NewRegistry registers all in-tree capacity provider factories.
func NewRegistry() *capacity.FactoryRegistry {
	r := capacity.NewFactoryRegistry()
	r.Register(static.ProviderName, static.NewFactory())
	r.Register(aws.ProviderName, aws.NewFactory())
	r.Register(gcp.ProviderName, gcp.NewFactory())
	return r
}
