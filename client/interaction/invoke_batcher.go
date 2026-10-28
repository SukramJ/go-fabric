// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/im"
)

// maxCommandRef is the highest command reference the batcher hands out
// before it wraps to 1 — matter.js MAX_COMMAND_REF
// (packages/protocol/src/action/client/ClientInteraction.ts:98 at 862ad75f).
const maxCommandRef = 0xffff

// pathsPerInvoker is a provider that knows the peer's MaxPathsPerInvoke —
// matter.js ExchangeProvider.maxPathsPerInvoke
// (packages/protocol/src/protocol/ExchangeProvider.ts:79, :130-132 for a
// dedicated session; packages/protocol/src/peer/PeerExchangeProvider.ts:30-32
// for a peer). A provider without it counts as 1 (ClientInteraction.ts:721).
type pathsPerInvoker interface {
	maxPathsPerInvoke() int
}

// connector is a provider that connects to its peer ahead of an exchange —
// matter.js ExchangeProvider.connect, which #invokeWithBatching awaits
// before it queues a command (ClientInteraction.ts:756-764).
type connector interface {
	connect(ctx context.Context, opts ExchangeOptions) error
}

// maxPathsPerInvoke is the peer's MaxPathsPerInvoke, at least 1 —
// `this.#exchangeProvider.maxPathsPerInvoke ?? 1` (ClientInteraction.ts:721).
func (c *Interaction) maxPathsPerInvoke() int {
	if p, ok := c.provider.(pathsPerInvoker); ok {
		return max(p.maxPathsPerInvoke(), 1)
	}
	return 1
}

// batchable reports whether a single command with opts may join an
// automatic batch — matter.js isBatchable (ClientInteraction.ts:185-214):
// a command for endpoint 0 (the root endpoint, the commissioning flow's)
// and one that sets anything the batch request does not rebuild —
// BatchDuration disabled, a timed or suppressed invoke, an expected
// processing time, the extended fail-safe timeout, an MRP delay, the
// Large Message quality — goes on its own. Network does not block: it
// picks the batch's lane.
func batchable(path im.ConcreteCommandPath, opts InvokeOptions) bool {
	return path.Endpoint != 0 && !opts.NoBatch && !opts.SuppressResponse &&
		!opts.Timed && opts.TimedTimeout == 0 && opts.ExpectedProcessingTime == 0 &&
		!opts.ExtendedFailSafeTimeout && opts.AdditionalMrpDelay == nil && !opts.large()
}

// batchLane is the commands queued for one network — matter.js BatchLane
// (ClientInteraction.ts:142-150): each network flushes and sends its
// batches on its own, so a command for a prioritized network never waits
// behind another network's batch.
type batchLane struct {
	network string
	// pending is the queue in call order (matter.js's Map keeps insertion
	// order, which #partitionBatch relies on).
	pending []*pendingCommand
	// send serializes the lane's batches — the lane's Mutex.
	send sync.Mutex
	// gen numbers the armed timer; a fire of an older generation is
	// stale. armed, deadline and timer are the lane's Timer.
	gen      uint64
	armed    bool
	deadline time.Time
	timer    clock.Timer
}

// pendingCommand is one queued command — matter.js PendingCommand
// (ClientInteraction.ts:129-140).
type pendingCommand struct {
	ref      uint16
	cmd      CommandRequest
	ctx      context.Context
	aborted  bool
	done     chan struct{}
	res      Result
	answered bool
	err      error
	finished bool
	// abandon, set while the command's batch is in flight, counts the
	// command out of it; the caller's abort runs it at once, so the batch
	// stops before the caller returns.
	abandon func()
}

// finish resolves the command once; the caller holds c.batchMu.
func (p *pendingCommand) finish(res Result, answered bool, err error) {
	if p.finished {
		return
	}
	p.finished = true
	p.res, p.answered, p.err = res, answered, err
	close(p.done)
}

// invokeBatched queues one command for the batch of its lane and waits for
// its answer — matter.js ClientInteraction.#invokeWithBatching
// (ClientInteraction.ts:744-818 at 862ad75f):
//
//   - a closed interaction refuses, and an already-aborted ctx fails
//     before anything is queued (:745-751);
//   - the peer is connected first, where a command addressed to an
//     unreachable peer spends its time (:753-764); a close during the
//     connect rejects with the close reason;
//   - the command takes a CommandRef unique in its lane (:822-843) and
//     waits for the lane's timer: BatchDuration, 0 Instant (:796); a timer
//     already running restarts with the shorter duration when the new
//     command needs a faster flush (:798-806);
//   - an abort of ctx rejects this command only and leaves the batch to
//     the others (:780-792).
func (c *Interaction) invokeBatched(ctx context.Context, cmd CommandRequest, opts InvokeOptions) (Result, bool, error) {
	c.closeMu.Lock()
	closed := c.closed
	c.closeMu.Unlock()
	if closed != nil {
		return Result{}, false, closed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, false, err
	}
	if p, ok := c.provider.(connector); ok {
		if err := p.connect(ctx, opts.exchangeOptions()); err != nil {
			return Result{}, false, c.closeReasonOr(err)
		}
	}
	c.batchMu.Lock()
	c.closeMu.Lock()
	closed = c.closed
	c.closeMu.Unlock()
	if closed != nil {
		c.batchMu.Unlock()
		return Result{}, false, closed
	}
	lane := c.laneFor(opts.Network)
	ref, err := c.allocateCommandRef(lane)
	if err != nil {
		c.batchMu.Unlock()
		return Result{}, false, err
	}
	p := &pendingCommand{ref: ref, cmd: cmd, ctx: ctx, done: make(chan struct{})}
	lane.pending = append(lane.pending, p)
	c.armLane(lane, max(opts.BatchDuration, 0))
	c.batchMu.Unlock()

	select {
	case <-p.done:
		return p.res, p.answered, p.err
	case <-ctx.Done():
		c.batchMu.Lock()
		defer c.batchMu.Unlock()
		if p.finished {
			// The answer and the abort raced; the answer stands.
			return p.res, p.answered, p.err
		}
		p.aborted = true
		lane.remove(p)
		p.finish(Result{}, false, ctx.Err())
		if p.abandon != nil {
			p.abandon()
		}
		return Result{}, false, ctx.Err()
	}
}

// closeReasonOr is the close reason once the interaction closed, else err.
func (c *Interaction) closeReasonOr(err error) error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed != nil {
		return c.closed
	}
	return err
}

// laneFor is #batchLaneFor (ClientInteraction.ts:820-827); the caller
// holds c.batchMu.
func (c *Interaction) laneFor(network string) *batchLane {
	if c.lanes == nil {
		c.lanes = make(map[string]*batchLane)
	}
	lane := c.lanes[network]
	if lane == nil {
		lane = &batchLane{network: network}
		c.lanes[network] = lane
	}
	return lane
}

// allocateCommandRef is #allocateCommandRef (ClientInteraction.ts:829-843):
// the next CommandRef from 1 up to 0xFFFF, wrapping, that no command
// queued in the lane holds. The caller holds c.batchMu.
func (c *Interaction) allocateCommandRef(lane *batchLane) (uint16, error) {
	if c.nextCommandRef == 0 {
		c.nextCommandRef = 1
	}
	start := c.nextCommandRef
	for {
		ref := c.nextCommandRef
		if c.nextCommandRef >= maxCommandRef {
			c.nextCommandRef = 1
		} else {
			c.nextCommandRef++
		}
		if !lane.holds(ref) {
			return ref, nil
		}
		if c.nextCommandRef == start {
			return 0, errors.New("interaction: no available commandRef values")
		}
	}
}

func (l *batchLane) holds(ref uint16) bool {
	for _, p := range l.pending {
		if p.ref == ref {
			return true
		}
	}
	return false
}

func (l *batchLane) remove(p *pendingCommand) {
	for i, q := range l.pending {
		if q == p {
			l.pending = append(l.pending[:i], l.pending[i+1:]...)
			return
		}
	}
}

// armLane starts the lane's timer for d, or restarts a running one with d
// when d ends sooner (ClientInteraction.ts:796-815). Instant (d of 0) is
// not a clock timer: the flush runs on a goroutine of its own after a
// yield, so every command another goroutine queues meanwhile shares the
// batch — Go's stand-in for matter.js's next macrotask. The caller holds
// c.batchMu.
func (c *Interaction) armLane(lane *batchLane, d time.Duration) {
	now := c.clock.Now()
	if lane.armed {
		if !now.Add(d).Before(lane.deadline) {
			return
		}
		if lane.timer != nil {
			lane.timer.Stop()
		}
	}
	lane.gen++
	gen := lane.gen
	lane.armed, lane.deadline, lane.timer = true, now.Add(d), nil
	fire := func() { c.flushLane(lane, gen) }
	if d == 0 {
		go func() {
			runtime.Gosched()
			fire()
		}()
		return
	}
	// The flush runs apart from the timer's callback: a test clock fires
	// inside its Advance, which must not wait for the batch's exchange.
	lane.timer = c.clock.AfterFunc(d, func() { go fire() })
}

// flushLane is #flushBatch (ClientInteraction.ts:849-876): the lane's
// queue is taken as one batch and cleared for the next, partitioned into
// sub-batches of unique command paths, and the sub-batches go one after
// the other under the lane's send lock. A sub-batch reached after the
// interaction closed is rejected with the close reason (:868-875).
func (c *Interaction) flushLane(lane *batchLane, gen uint64) {
	c.batchMu.Lock()
	if lane.gen != gen || !lane.armed {
		c.batchMu.Unlock()
		return
	}
	lane.armed, lane.timer = false, nil
	cmds := lane.pending
	lane.pending = nil
	c.batchMu.Unlock()
	if len(cmds) == 0 {
		return
	}
	c.closeMu.Lock()
	if closed := c.closed; closed != nil {
		c.closeMu.Unlock()
		c.rejectAll(cmds, closed)
		return
	}
	c.inflight.Add(1)
	c.closeMu.Unlock()
	c.flushing.Add(1)
	defer c.flushing.Add(-1)
	defer c.inflight.Done()
	for _, batch := range partitionBatch(cmds) {
		lane.send.Lock()
		c.closeMu.Lock()
		closed := c.closed
		c.closeMu.Unlock()
		if closed != nil {
			c.rejectAll(batch, closed)
		} else {
			c.executeBatch(batch, lane.network)
		}
		lane.send.Unlock()
	}
}

// rejectAll rejects every command of cmds not yet resolved with err.
func (c *Interaction) rejectAll(cmds []*pendingCommand, err error) {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()
	for _, p := range cmds {
		p.finish(Result{}, false, err)
	}
}

// partitionBatch is #partitionBatch (ClientInteraction.ts:882-912): a
// greedy fill where each command goes into the first sub-batch that does
// not yet hold its path, so no InvokeRequest names one path twice.
func partitionBatch(cmds []*pendingCommand) [][]*pendingCommand {
	if len(cmds) <= 1 {
		return [][]*pendingCommand{cmds}
	}
	type sub struct {
		paths map[im.ConcreteCommandPath]bool
		cmds  []*pendingCommand
	}
	var subs []*sub
	for _, p := range cmds {
		key := p.cmd.Path
		placed := false
		for _, s := range subs {
			if !s.paths[key] {
				s.paths[key] = true
				s.cmds = append(s.cmds, p)
				placed = true
				break
			}
		}
		if !placed {
			subs = append(subs, &sub{paths: map[im.ConcreteCommandPath]bool{key: true}, cmds: []*pendingCommand{p}})
		}
	}
	out := make([][]*pendingCommand, len(subs))
	for i, s := range subs {
		out[i] = s.cmds
	}
	return out
}

// executeBatch sends one sub-batch and hands each command its answer —
// matter.js #executeBatch (ClientInteraction.ts:914-1005):
//
//   - commands aborted between the snapshot and the send are dropped
//     (:917-922);
//   - the batch's exchange stops only once every command in it has been
//     abandoned — it outlives the caller of any one command (:934-958);
//   - a batch above the peer's MaxPathsPerInvoke is split as an invoke of
//     several commands is (:963-967), each part on an exchange of its own
//     and a part of one command without its CommandRef;
//   - the answers are demultiplexed by CommandRef (:969-987); a command
//     left without an answer resolves unanswered (:990-995), and an
//     exchange error rejects the commands of its part (:996-1001).
func (c *Interaction) executeBatch(batch []*pendingCommand, network string) {
	c.batchMu.Lock()
	live := batch[:0:0]
	for _, p := range batch {
		if !p.aborted && !p.finished {
			live = append(live, p)
		}
	}
	c.batchMu.Unlock()
	if len(live) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unabandoned := int32(len(live)) //nolint:gosec // a lane holds at most 0xFFFF commands
	stops := make([]func() bool, 0, len(live))
	c.batchMu.Lock()
	for _, p := range live {
		var once sync.Once
		abandon := func() {
			once.Do(func() {
				if atomic.AddInt32(&unabandoned, -1) == 0 {
					cancel()
				}
			})
		}
		p.abandon = abandon
		stops = append(stops, context.AfterFunc(p.ctx, abandon))
	}
	c.batchMu.Unlock()
	defer func() {
		c.batchMu.Lock()
		for _, p := range live {
			p.abandon = nil
		}
		c.batchMu.Unlock()
		for _, stop := range stops {
			stop()
		}
	}()
	opts := InvokeOptions{ClientRequest: ClientRequest{Network: network}}
	maxPaths := c.maxPathsPerInvoke()
	var wg sync.WaitGroup
	for start := 0; start < len(live); start += maxPaths {
		part := live[start:min(start+maxPaths, len(live))]
		wg.Go(func() {
			cmds := make([]CommandRequest, len(part))
			refs := make([]uint16, len(part))
			for i, p := range part {
				cmds[i], refs[i] = p.cmd, p.ref
			}
			out := make([]Result, len(part))
			answered := make([]bool, len(part))
			err := c.invokeBatch(ctx, cmds, refs, out, answered, opts)
			if err != nil {
				err = c.closeReasonOr(err)
			}
			c.batchMu.Lock()
			defer c.batchMu.Unlock()
			for i, p := range part {
				if p.aborted {
					continue
				}
				if err != nil {
					p.finish(Result{}, false, err)
					continue
				}
				p.finish(out[i], answered[i], nil)
			}
		})
	}
	wg.Wait()
}

// closeBatches rejects every queued command with reason and stops the
// lanes' timers — the batching half of ClientInteraction.close
// (ClientInteraction.ts:284-291).
func (c *Interaction) closeBatches(reason error) {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()
	for _, lane := range c.lanes {
		if lane.timer != nil {
			lane.timer.Stop()
		}
		lane.armed, lane.timer = false, nil
		lane.gen++
		for _, p := range lane.pending {
			p.finish(Result{}, false, reason)
		}
		lane.pending = nil
	}
}
