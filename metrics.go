package mikrotik

import (
	"github.com/coredns/coredns/plugin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	writesCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "mikrotik",
		Name:      "writes_total",
		Help:      "Count of address-list write attempts by status.",
	}, []string{"device", "list", "status"})

	queueDroppedCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "mikrotik",
		Name:      "queue_dropped_total",
		Help:      "Count of items dropped due to full queue.",
	}, []string{"device"})
)
