// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import "sync"

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
	idle    sync.Cond
	jobs    []func()
	running bool
}

func newJobQueue() *jobQueue {
	q := &jobQueue{}
	q.idle.L = &q.mu
	return q
}

// enqueue appends job; past maxPendingJobs it waits until the queue has
// drained — the back-pressure of ClientStructure.ts:469-471 (`if
// (pendingJobs > MAX_PENDING_JOBS) await queue`).
func (q *jobQueue) enqueue(job func()) {
	q.mu.Lock()
	q.jobs = append(q.jobs, job)
	pending := len(q.jobs)
	if q.running {
		pending++
	} else {
		q.running = true
		go q.run()
	}
	q.mu.Unlock()
	if pending > maxPendingJobs {
		q.wait()
	}
}

// run runs the queued jobs in order until none is left.
func (q *jobQueue) run() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			q.idle.Broadcast()
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
func (q *jobQueue) wait() {
	q.mu.Lock()
	for q.running || len(q.jobs) > 0 {
		q.idle.Wait()
	}
	q.mu.Unlock()
}
