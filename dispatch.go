package billow

import (
	"hash/fnv"
	"sync"
	"time"
)

const (
	// defaultDispatchWorkers is the number of parallel dispatch goroutines.
	// Each worker owns its own channel — events for the same subscription
	// always land on the same worker, preserving per-subscription ordering.
	defaultDispatchWorkers = 16

	// defaultDispatchQueueDepth is the per-worker channel buffer.
	// Back-pressure: a full channel blocks the caller until space is free.
	defaultDispatchQueueDepth = 256
)

// dispatchJob is a single unit of async work.
type dispatchJob struct {
	event *WebhookEvent
	// errC is non-nil when a caller is synchronously awaiting the result
	// (used by HandleWebhook when Options.DispatchWorkers == 0, but kept
	// here so the pool can optionally surface handler errors to callers).
	errC chan<- error
}

// dispatchPool is a fixed-size set of goroutines each draining its own queue.
type dispatchPool struct {
	workers int
	queues  []chan dispatchJob
	wg      sync.WaitGroup
}

// newDispatchPool starts workers goroutines and returns the running pool.
func newDispatchPool(workers, queueDepth int, m *Manager) *dispatchPool {
	if workers <= 0 {
		workers = defaultDispatchWorkers
	}
	if queueDepth <= 0 {
		queueDepth = defaultDispatchQueueDepth
	}
	p := &dispatchPool{
		workers: workers,
		queues:  make([]chan dispatchJob, workers),
	}
	for i := range p.queues {
		p.queues[i] = make(chan dispatchJob, queueDepth)
		idx := i
		p.wg.Add(1)
		go p.runWorker(idx, m)
	}
	return p
}

// send routes job to the worker responsible for the given routing key.
// Blocks when the target queue is full (back-pressure).
func (p *dispatchPool) send(key string, job dispatchJob) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	p.queues[int(h.Sum32())%p.workers] <- job
}

// runWorker drains queue i until it is closed.
func (p *dispatchPool) runWorker(i int, m *Manager) {
	defer p.wg.Done()
	for job := range p.queues[i] {
		m.metrics.WorkerQueueDepth(i, len(p.queues[i]))
		start := time.Now()
		err := m.dispatchSync(job.event)
		m.metrics.DispatchDuration(string(job.event.Type), err == nil, time.Since(start))
		if job.errC != nil {
			job.errC <- err
		}
	}
}

// close drains all queues then waits for every worker to exit.
// Called exactly once by Manager.Close.
func (p *dispatchPool) close() {
	for _, q := range p.queues {
		close(q)
	}
	p.wg.Wait()
}

// ---------------------------------------------------------------------------
// Manager-level dispatch helpers
// ---------------------------------------------------------------------------

// dispatchSync calls all registered handlers for event.Type in the calling
// goroutine. It is used directly by workers and by HandleWebhook when no
// async pool is configured.
func (m *Manager) dispatchSync(event *WebhookEvent) error {
	ptr := m.handlersPtr.Load()
	if ptr == nil {
		return nil
	}
	for _, h := range (*ptr)[event.Type] {
		if err := h(event); err != nil {
			return err
		}
	}
	return nil
}

// dispatch is called by HandleWebhook. When a pool is running, the event is
// queued for async processing and nil is returned immediately. Otherwise the
// handlers are invoked synchronously and any error is returned to the caller.
func (m *Manager) dispatch(event *WebhookEvent) error {
	if m.pool != nil {
		// Route by ProviderSubID so all events for the same subscription
		// are serialised through one worker.
		key := event.ProviderSubID
		if key == "" {
			key = string(event.Type)
		}
		m.pool.send(key, dispatchJob{event: event})
		return nil
	}
	start := time.Now()
	err := m.dispatchSync(event)
	m.metrics.DispatchDuration(string(event.Type), err == nil, time.Since(start))
	return err
}
