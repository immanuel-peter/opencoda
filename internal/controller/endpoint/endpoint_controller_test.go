package endpoint

import "testing"

func TestPercentileMs(t *testing.T) {
	p50, p95 := percentileMs([]float64{10, 20, 30, 40, 50}, 0.50, 0.95)
	if p50 < 25000 || p50 > 35000 {
		t.Fatalf("p50=%d", p50)
	}
	if p95 < 45000 || p95 > 51000 {
		t.Fatalf("p95=%d", p95)
	}
}
