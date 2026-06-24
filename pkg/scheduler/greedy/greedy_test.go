package greedy

import (
	"testing"

	"github.com/immanuel-peter/opencoda/pkg/scheduler"
)

func TestGreedyFillOrdersByPriority(t *testing.T) {
	s := New()
	plan, err := s.Fill(3, []scheduler.PoolView{
		{Name: "expensive", Priority: 20, ObservedHourlyUSD: 50, ICEPenalty: 1, MaxNodes: 10, CurrentNodes: 0, Available: 10},
		{Name: "cheap", Priority: 10, ObservedHourlyUSD: 5, ICEPenalty: 1, MaxNodes: 10, CurrentNodes: 0, Available: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) == 0 {
		t.Fatal("expected plan entries")
	}
	if plan.Entries[0].PoolName != "cheap" {
		t.Fatalf("expected cheap pool first, got %s", plan.Entries[0].PoolName)
	}
}
