package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const Namespace = "coda"

var (
	AllocationUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "allocation_utilization",
			Help:      "GPU-seconds running app code / GPU-seconds provisioned",
		},
		[]string{"endpoint"},
	)

	ColdStartSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "cold_start_seconds",
			Help:      "Cold start latency by path",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 12),
		},
		[]string{"path", "endpoint"},
	)

	KVHitRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "kv_hit_rate",
			Help:      "LMCache prefix hit rate",
		},
		[]string{"endpoint"},
	)

	PoolObservedHourlyUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "pool_observed_hourly_usd",
			Help:      "Observed hourly USD per pool",
		},
		[]string{"pool"},
	)

	PoolICE = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "pool_ice_total",
			Help:      "Insufficient capacity errors per pool",
		},
		[]string{"pool"},
	)

	BufferSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "buffer_size",
			Help:      "Warm GPU buffer size",
		},
		[]string{"policy"},
	)

	EndpointCostUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "endpoint_cost_usd_hourly",
			Help:      "Estimated hourly cost per endpoint",
		},
		[]string{"endpoint", "namespace"},
	)
)

func Register(reg prometheus.Registerer) {
	reg.MustRegister(
		AllocationUtilization,
		ColdStartSeconds,
		KVHitRate,
		PoolObservedHourlyUSD,
		PoolICE,
		BufferSize,
		EndpointCostUSD,
	)
}
