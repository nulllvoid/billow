package billow

import "time"

// Metrics is an optional hook interface that lets callers plug in a
// Prometheus, Datadog, or any custom instrumentation backend.
// Pass a Metrics implementation via Options.Metrics; a nil value (the
// default) silently disables all instrumentation.
//
// All methods must be safe for concurrent use and must not block.
type Metrics interface {
	// DispatchDuration records how long a single webhook dispatch took,
	// along with the canonical event type and whether it succeeded.
	DispatchDuration(eventType string, success bool, d time.Duration)

	// UsageReportDuration records the duration of a single provider
	// usage-report attempt, along with the metric name and success flag.
	UsageReportDuration(metric string, success bool, d time.Duration)

	// PlanCacheHit records a plan-cache lookup outcome (hit=true / miss=false).
	PlanCacheHit(hit bool)

	// WorkerQueueDepth records the current depth of a dispatch worker queue.
	// workerIndex identifies which worker's queue is being sampled.
	WorkerQueueDepth(workerIndex int, depth int)
}

// noopMetrics is the zero-allocation default used when Options.Metrics is nil.
type noopMetrics struct{}

func (noopMetrics) DispatchDuration(_ string, _ bool, _ time.Duration)   {}
func (noopMetrics) UsageReportDuration(_ string, _ bool, _ time.Duration) {}
func (noopMetrics) PlanCacheHit(_ bool)                                   {}
func (noopMetrics) WorkerQueueDepth(_ int, _ int)                         {}
