package scheduler

// PoolView is scheduler input for a GPUPool.
type PoolView struct {
	Name              string
	Priority          int
	ObservedHourlyUSD float64
	ICEPenalty        float64
	MaxNodes          int
	CurrentNodes      int
	Available         int
}

// PlanEntry assigns nodes to a pool.
type PlanEntry struct {
	PoolName  string
	NodeCount int
}

// Plan is the output of Fill.
type Plan struct {
	Entries []PlanEntry
}

// Scheduler decides how to source GPUs across pools (§15.2).
type Scheduler interface {
	Fill(need int, pools []PoolView) (Plan, error)
}
