// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"sync"
)

// maxPendingJobs is the bound past which the report consumer waits for
// the node's jobs to drain — matter.js MAX_PENDING_JOBS
// (packages/node/src/node/client/ClientStructure.ts:57 at 862ad75f,
// applied at :469-471).
const maxPendingJobs = 100

// jobQueue is a node's serial FIFO of the store writes and hook deliveries
// of subscription reports, so a report's final StatusResponse is not gated
// on the store or on host hooks: matter.js ClientStructure.mutate's queue
// (ClientStructure.ts:417-439, "Serial FIFO for deferred persist/emit so
// Status.Success is not gated on internal and consumer data processing").
// One goroutine runs the jobs in insertion order while any are queued;
// none runs while the queue is empty.
type jobQueue struct {
	mu      sync.Mutex
	jobs    []func()
	running bool
	// drained is closed when the run that is under way ends with the
	// queue empty; nil while none runs.
	drained chan struct{}
}

func newJobQueue() *jobQueue { return &jobQueue{} }

// enqueue appends job; past maxPendingJobs it waits until the queue has
// drained — the back-pressure of ClientStructure.ts:469-471 (`if
// (pendingJobs > MAX_PENDING_JOBS) await queue`). That wait takes no
// context, because the callers that reach it have none to give: a job is
// queued from a subscription's callbacks (Updated, KeepaliveReceived,
// ActiveChanged), and the context of the request that established the
// subscription ended with that request.
//
//nolint:contextcheck // the back-pressure runs in subscription callbacks, which outlive the request context their closures were built under
func (q *jobQueue) enqueue(job func()) {
	if q.add(job) > maxPendingJobs {
		q.wait()
	}
}

// add appends job as the queue's last, starts the run when none is under
// way and returns the number of jobs pending, the running one included.
// It never waits: [Node.Stop] hands the queue the store's final flush
// with it once its context has ended.
func (q *jobQueue) add(job func()) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
	pending := len(q.jobs)
	if q.running {
		return pending + 1
	}
	q.running = true
	q.drained = make(chan struct{})
	go q.run()
	return pending
}

// run runs the queued jobs in order until none is left.
func (q *jobQueue) run() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			close(q.drained)
			q.drained = nil
			q.mu.Unlock()
			return
		}
		job := q.jobs[0]
		q.jobs[0] = nil
		q.jobs = q.jobs[1:]
		q.mu.Unlock()
		job()
	}
}

// wait returns once every queued job has run — the drain of
// ClientStructure.ts:482-486. A job must not call it.
func (q *jobQueue) wait() { _ = q.waitContext(context.Background()) }

// waitContext is wait within ctx: when ctx ends first it returns ctx's
// error and the queue goes on running its jobs. matter.js's drain has no
// deadline to break; a Go caller hands one in (Node.Stop), and a job runs
// host hooks that may block.
func (q *jobQueue) waitContext(ctx context.Context) error {
	for {
		q.mu.Lock()
		drained := q.drained
		q.mu.Unlock()
		if drained == nil {
			return nil
		}
		select {
		case <-drained:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
