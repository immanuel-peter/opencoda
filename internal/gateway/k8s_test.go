package gateway

import (
	"strings"
	"testing"
)

func TestParsePrefixCacheMetrics(t *testing.T) {
	body := strings.NewReader(`
# HELP coda_prefix_cache_hits_total hits
# TYPE coda_prefix_cache_hits_total counter
coda_prefix_cache_hits_total 7
coda_prefix_cache_queries_total 10
`)
	hits, queries, ok := parsePrefixCacheMetrics(body)
	if !ok {
		t.Fatal("expected ok")
	}
	if hits != 7 || queries != 10 {
		t.Fatalf("hits=%d queries=%d", hits, queries)
	}
	rate := float64(hits) / float64(queries)
	if rate < 0.69 || rate > 0.71 {
		t.Fatalf("rate=%f", rate)
	}
}
