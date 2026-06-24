package capacity

import (
	"context"
	"time"
)

// GPURequest describes capacity to provision.
type GPURequest struct {
	PoolName      string
	GPUType       string
	GPUCount      int
	NodeCount     int
	InstanceTypes []string
	Subnets       []string
	Constraints   Constraints
}

type Constraints struct {
	Region         string
	Zone           string
	CapacityType   string
	MaxHourlyUSD   float64
}

// Offer is a provider-quoted allocation option.
type Offer struct {
	ID            string
	InstanceType  string
	Zone          string
	HourlyUSD     float64
	ExpiresAt     time.Time
	Interruptible bool
}

// NodeHandle tracks a provisioned node.
type NodeHandle struct {
	ProviderID string
	NodeName   string
	Labels     map[string]string
	LaunchedAt time.Time
}

// CapacityReport is observed pool reality.
type CapacityReport struct {
	Available         int
	RecentICE         []time.Time
	ObservedHourlyUSD float64
}

// CapacityProvider is the portability boundary for compute (§14.1).
type CapacityProvider interface {
	Name() string
	Quote(ctx context.Context, req GPURequest) ([]Offer, error)
	Provision(ctx context.Context, offer Offer) (*NodeHandle, error)
	Release(ctx context.Context, h *NodeHandle) error
	Capacity(ctx context.Context, pool string) (CapacityReport, error)
}

// Registry holds registered providers by name.
type Registry struct {
	providers map[string]CapacityProvider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]CapacityProvider)}
}

func (r *Registry) Register(p CapacityProvider) {
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (CapacityProvider, bool) {
	p, ok := r.providers[name]
	return p, ok
}
