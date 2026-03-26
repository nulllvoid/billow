package billow

import (
	"context"
	"sync"
	"time"

	"github.com/nulllvoid/billow/store"
)

const (
	// defaultReporterInterval is how often the sweeper wakes up to flush
	// unreported usage records to the payment provider.
	defaultReporterInterval = 30 * time.Second

	// defaultReporterBatchSize is the maximum number of records fetched and
	// attempted per sweep cycle.
	defaultReporterBatchSize = 100
)

// usageReporter is a background sweeper that picks up usage records that were
// persisted locally but not yet reported to the payment provider.
//
// This implements the persist-then-report pattern:
//  1. RecordUsage always writes to the local store first (survives crashes).
//  2. The sweeper periodically fetches records with ProviderReportedAt == nil
//     and pushes them to the provider, then marks them as reported.
//
// The reporter only activates when both a provider and a ReportableUsageStore
// are configured. When either is absent, the old fire-and-forget path in
// RecordUsage remains the only reporting mechanism.
type usageReporter struct {
	m        *Manager
	store    store.ReportableUsageStore
	interval time.Duration
	batch    int

	stopOnce sync.Once
	stopC    chan struct{}
	wg       sync.WaitGroup
}

// newUsageReporter creates and starts the sweeper goroutine.
func newUsageReporter(m *Manager, rs store.ReportableUsageStore, interval time.Duration, batch int) *usageReporter {
	if interval <= 0 {
		interval = defaultReporterInterval
	}
	if batch <= 0 {
		batch = defaultReporterBatchSize
	}
	r := &usageReporter{
		m:        m,
		store:    rs,
		interval: interval,
		batch:    batch,
		stopC:    make(chan struct{}),
	}
	r.wg.Add(1)
	go r.run()
	return r
}

// run is the sweeper loop. It ticks at r.interval and flushes pending records.
func (r *usageReporter) run() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.sweep()
		case <-r.stopC:
			// Final flush before exit — ensures records written just before
			// shutdown are not left unreported across restarts.
			r.sweep()
			return
		}
	}
}

// sweep fetches one batch of unreported records and reports each to the provider.
func (r *usageReporter) sweep() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	records, err := r.store.ListUnreportedUsage(ctx, r.batch)
	if err != nil || len(records) == 0 {
		return
	}

	for _, rec := range records {
		// Look up the provider subscription ID. Skip if not found — the
		// subscription may have been created without a provider.
		ss, err := r.m.subs.GetSubscription(ctx, rec.SubscriptionID)
		if err != nil || ss.ProviderID == "" {
			continue
		}

		start := time.Now()
		reportErr := r.m.provider.ReportUsage(ctx, ss.ProviderID, rec.Metric, rec.Quantity)
		r.m.metrics.UsageReportDuration(rec.Metric, reportErr == nil, time.Since(start))

		if reportErr != nil {
			// Leave ProviderReportedAt nil — sweeper will retry next cycle.
			continue
		}

		now := time.Now().UTC()
		// Best-effort mark; a failure here is non-fatal — the record will be
		// retried next sweep but the provider already accepted the report, so
		// the worst outcome is a duplicate report (idempotent for most providers).
		_ = r.store.MarkUsageReported(ctx, rec.ID, now)
	}
}

// close stops the sweeper and waits for the final flush to complete.
// Called exactly once by Manager.Close.
func (r *usageReporter) close() {
	r.stopOnce.Do(func() {
		close(r.stopC)
	})
	r.wg.Wait()
}
