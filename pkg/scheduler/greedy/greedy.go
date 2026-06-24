package greedy

import (
	"sort"

	"github.com/immanuel-peter/opencoda/pkg/scheduler"
)

// Scheduler fills pools by (priority, observedHourlyUSD * ICEPenalty).
type Scheduler struct{}

func New() *Scheduler { return &Scheduler{} }

func (s *Scheduler) Fill(need int, pools []scheduler.PoolView) (scheduler.Plan, error) {
	if need <= 0 {
		return scheduler.Plan{}, nil
	}

	sorted := append([]scheduler.PoolView(nil), pools...)
	sort.Slice(sorted, func(i, j int) bool {
		ci := float64(sorted[i].Priority) + sorted[i].ObservedHourlyUSD*sorted[i].ICEPenalty
		cj := float64(sorted[j].Priority) + sorted[j].ObservedHourlyUSD*sorted[j].ICEPenalty
		return ci < cj
	})

	plan := scheduler.Plan{}
	remaining := need

	for _, pool := range sorted {
		if remaining <= 0 {
			break
		}
		capacity := pool.MaxNodes - pool.CurrentNodes
		if capacity <= 0 {
			continue
		}
		if pool.Available > 0 && pool.Available < capacity {
			capacity = pool.Available
		}
		n := remaining
		if n > capacity {
			n = capacity
		}
		if n <= 0 {
			continue
		}
		plan.Entries = append(plan.Entries, scheduler.PlanEntry{
			PoolName:  pool.Name,
			NodeCount: n,
		})
		remaining -= n
	}

	return plan, nil
}
