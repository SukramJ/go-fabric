// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// heldFlush is the persister's interval in the tests below, on the
// scripted registry's fake clock.
const heldFlush = 5 * time.Second

// heldVendorID is the value the held report writes to BasicInformation's
// VendorID (attribute 2).
const heldVendorID = 0xFFF4

var errStoreClosed = errors.New("the test's store is closed")

// closableStore is a [client.Store] that counts the writes of the root's
// BasicInformation cluster, keeps the last VendorID stored and, once
// closed, refuses every write.
type closableStore struct {
	client.Store
	closed atomic.Bool
	saves  atomic.Int64

	mu     sync.Mutex
	vendor []byte
}

func (s *closableStore) Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	held := ep == 0 && cluster == basicinfo.ClusterID
	if held {
		s.saves.Add(1)
	}
	if s.closed.Load() {
		return errStoreClosed
	}
	if v, ok := values[2]; held && ok {
		s.mu.Lock()
		s.vendor = bytes.Clone(v)
		s.mu.Unlock()
	}
	return s.Store.Save(nodeID, ep, cluster, values, version)
}

// storedHeld reports whether the held report's VendorID is in the store.
func (s *closableStore) storedHeld(t *testing.T) bool {
	t.Helper()
	e := tlv.NewEncoder()
	e.PutUint(tlv.AnonymousTag(), heldVendorID)
	want, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Equal(s.vendor, want)
}

// heldQueue is a started node with a buffered store whose job queue is
// held by a report's Changed hook: the report's value is dirty in the
// cache, the flush timer armed, and the hook returns at release.
type heldQueue struct {
	sr       *scriptedRegistry
	n        *client.Node
	store    *closableStore
	release  func()
	returned *atomic.Bool
}

func holdJobQueue(t *testing.T) *heldQueue {
	t.Helper()
	var armed, returned atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := &heldQueue{returned: &returned, release: sync.OnceFunc(func() { close(release) })}
	h.sr = newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) {
		h.store = &closableStore{Store: c.Store}
		c.Store = h.store
		c.Node.FlushInterval = heldFlush
		c.HooksFor = func(string) client.Hooks {
			return client.Hooks{
				Changed: func() {
					if !armed.Load() {
						return
					}
					select {
					case entered <- struct{}{}:
						<-release
						returned.Store(true)
					default:
					}
				},
			}
		}
	})
	// A test that fails with the hook held still ends its bubble.
	t.Cleanup(h.release)
	sr := h.sr
	h.n = sr.reg.Get("peer1")
	const id = 0x54
	sr.subscribe(t, h.n, sr.imageWithOpCreds("ours"), id)
	client.WaitRegistry(sr.reg)
	synctest.Wait()
	armed.Store(true)

	sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: 0x7104, ProtocolID: im.InteractionModelProtocolID},
		encode(t, func(e *tlv.Encoder) {
			im.ReportData{SubscriptionID: id, HasSubscription: true, Reports: []im.AttributeReport{value(0, basicinfo.ClusterID, 2, 10, tlvNode(uintNode(heldVendorID)))}}.MarshalTLV(e,
				func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
					interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
				})
		}))
	status := sr.next(t)
	sr.send(t, message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: status.hdr.MessageCounter, Opcode: 0x10, ExchangeID: 0x7104}, nil)
	<-entered // the hook holds the node's job queue
	if h.store.storedHeld(t) {
		t.Fatal("the held report's value is in the store before any flush")
	}
	return h
}

// shortContext ends 50 ms into the bubble's time.
func shortContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// TestStopThatTimedOutIsCompletedByTheQueue (review II, R2-P0-rv-C03): a
// Stop that returns its context's error leaves no timer of the persister
// armed, and the store's final flush is the job queue's last job — it
// runs when the hook returns, whether or not a second Stop asks. Before,
// the flush timer stayed armed and the final flush waited for a second
// Stop.
func TestStopThatTimedOutIsCompletedByTheQueue(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		if err := h.n.Stop(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
		}
		// No timer of the half-stopped node flushes.
		before := h.store.saves.Load()
		for range 3 {
			h.sr.clock.Advance(heldFlush)
			synctest.Wait()
		}
		if got := h.store.saves.Load(); got != before {
			t.Fatalf("%d store writes by the flush timer after the Stop that timed out, want none", got-before)
		}
		if h.store.storedHeld(t) {
			t.Fatal("the final flush ran beside the queue's jobs")
		}

		// The hook returns: the queue runs the final flush as its last job.
		h.release()
		synctest.Wait()
		if !h.store.storedHeld(t) {
			t.Fatal("the queue did not run the final flush the Stop left it")
		}
		// A second Stop is safe and finds the stop complete.
		after := h.store.saves.Load()
		if err := h.n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := h.store.saves.Load(); got != after {
			t.Fatalf("the second Stop wrote %d more times, want none: nothing was dirty", got-after)
		}
		client.WaitRegistry(h.sr.reg)
	})
}

// TestSecondStopWaitsForTheFinalFlush (review II, R2-P0-rv-C03): a second
// Stop with a live context returns once the drain and the final flush are
// done — the value is in the store when it returns.
func TestSecondStopWaitsForTheFinalFlush(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		if err := h.n.Stop(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
		}
		stopped := make(chan error, 1)
		go func() {
			err := h.n.Stop(context.Background())
			if err == nil && !h.store.storedHeld(t) {
				err = errors.New("the second Stop returned before the final flush")
			}
			stopped <- err
		}()
		synctest.Wait()
		select {
		case err := <-stopped:
			t.Fatalf("the second Stop returned %v while the hook still held the queue", err)
		default:
		}
		h.release()
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if !h.returned.Load() {
			t.Fatal("the second Stop returned before the queued hook did")
		}
		client.WaitRegistry(h.sr.reg)
	})
}

// TestRegistryCloseCalledAgainCompletesTheStop (review II, R2-P0-rv-C03):
// a Registry.Close whose context ended while a node's hook blocked is
// completed by calling Close again — the second call closes the nodes
// again and answers for them. Before, it returned nil at once: the
// registry was closed, and nothing waited for the node.
func TestRegistryCloseCalledAgainCompletesTheStop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		if err := h.sr.reg.Close(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close = %v, want context.DeadlineExceeded", err)
		}
		// Still held: a second Close says so.
		if err := h.sr.reg.Close(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the second Close = %v with the hook still blocked, want context.DeadlineExceeded", err)
		}
		if h.store.storedHeld(t) {
			t.Fatal("the final flush ran beside the queue's jobs")
		}
		closed := make(chan error, 1)
		go func() {
			err := h.sr.reg.Close(context.Background())
			if err == nil && !h.store.storedHeld(t) {
				err = errors.New("Close returned before the final flush")
			}
			closed <- err
		}()
		synctest.Wait()
		h.release()
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		// Idempotent: a closed registry with every node stopped.
		if err := h.sr.reg.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		// Registry.Stop takes the same way.
		if err := h.sr.reg.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestStopWithAnEndedContextLeavesNoTimerOnAClosedStore (review II,
// R2-P0-rv-C03): the shutdown with the signal's already-cancelled context
// while a report job runs — Stop returns the context's error at once, the
// host closes its database, and the node writes to it once more, when the
// queue runs the final flush, and never again. Before, the flush timer
// stayed armed, each of its flushes failed, marked the cache dirty and
// armed the next: a write to the closed store per interval for the life
// of the process.
func TestStopWithAnEndedContextLeavesNoTimerOnAClosedStore(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := h.sr.reg.Close(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Close = %v, want context.Canceled", err)
		}
		h.store.closed.Store(true)
		before := h.store.saves.Load()
		h.release()
		synctest.Wait()
		if got := h.store.saves.Load() - before; got != 1 {
			t.Errorf("%d writes to the closed store when the queue ended, want 1: the final flush", got)
		}
		for range 5 {
			h.sr.clock.Advance(heldFlush)
			synctest.Wait()
		}
		if got := h.store.saves.Load() - before; got != 1 {
			t.Fatalf("%d writes to the closed store, want 1: no timer retries on a closed node", got)
		}
	})
}
