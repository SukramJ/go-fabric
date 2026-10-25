// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Ports of matter.js at 862ad75f around the ICD client's hold and errors:
// packages/node/test/behavior/system/icd/IcdMultiAdminErrorTest.ts,
// IcdPeerAsleepErrorTest.ts, holdUntilIcdPeerAwakeTest.ts,
// packages/node/test/node/client/ClientNodeInteractionIcdTest.ts and
// ClientNodePhysicalPropertiesIcdTest.ts. matter.js's ImplementationError is
// ErrICDOperation, its AbortedError the ended context's error, a request's
// icdAwaitTimeout WithICDAwaitTimeout, and its peer.interaction.read the
// node's Read.
package client_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/icd"
)

func TestIcdMultiAdminErrorParity(t *testing.T) {
	t.Parallel()
	trusted := client.TrustedEcosystemVendors
	t.Run("IcdMultiAdminError.assertSingleAdmin", func(t *testing.T) {
		t.Parallel()
		t.Run("passes with a single fabric", func(t *testing.T) {
			t.Parallel()
			if err := client.AssertSingleAdmin([]uint16{0xfff1}, trusted, false); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("throws IcdMultiAdminError with two admin fabrics from different vendors", func(t *testing.T) {
			t.Parallel()
			var e *client.ICDMultiAdminError
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0xfff2}, trusted, false); !errors.As(err, &e) {
				t.Fatal(err)
			}
		})
		t.Run("the thrown error is also an ImplementationError", func(t *testing.T) {
			t.Parallel()
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0xfff2}, trusted, false); !errors.Is(err, client.ErrICDOperation) {
				t.Fatal(err)
			}
		})
		t.Run("exposes the offending vendor ids on the error", func(t *testing.T) {
			t.Parallel()
			var e *client.ICDMultiAdminError
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0xfff2}, trusted, false); !errors.As(err, &e) || !slices.Equal(e.AdminVendorIDs, []uint16{0xfff1, 0xfff2}) {
				t.Fatal(err)
			}
		})
		t.Run("deduplicates vendor ids on the error", func(t *testing.T) {
			t.Parallel()
			var e *client.ICDMultiAdminError
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0xfff1}, trusted, false); !errors.As(err, &e) || !slices.Equal(e.AdminVendorIDs, []uint16{0xfff1}) {
				t.Fatal(err)
			}
		})
		t.Run("ignores a trusted ecosystem vendor", func(t *testing.T) {
			t.Parallel()
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0x1384}, trusted, false); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("allowMultiAdmin overrides the throw", func(t *testing.T) {
			t.Parallel()
			if err := client.AssertSingleAdmin([]uint16{0xfff1, 0xfff2}, trusted, true); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("defaults include Apple and Samsung SmartThings", func(t *testing.T) {
			t.Parallel()
			if !slices.Contains(trusted, 0x1384) || !slices.Contains(trusted, 0x110a) {
				t.Fatal(trusted)
			}
		})
	})
}

func TestIcdPeerAsleepErrorParity(t *testing.T) {
	t.Parallel()
	addr := client.PeerAddress{FabricIndex: 1, NodeID: 0x2f}
	t.Run("renders the peer address in the message", func(t *testing.T) {
		t.Parallel()
		msg := (&client.ICDPeerAsleepError{Peer: addr, Timeout: 5 * time.Second}).Error()
		if !strings.Contains(msg, "@1:2f") || strings.Contains(msg, "[object Object]") {
			t.Fatal(msg)
		}
	})
	t.Run("also renders correctly for an already-interned PeerAddress", func(t *testing.T) {
		t.Parallel()
		e := client.ICDPeerAsleepError{Peer: client.PeerAddress{FabricIndex: 1, NodeID: 0x2f}, Timeout: 5 * time.Second}
		if !strings.Contains(e.Error(), "@1:2f") {
			t.Fatal(e.Error())
		}
	})
}

func TestHoldUntilIcdPeerAwakeParity(t *testing.T) {
	t.Parallel()
	addr := client.PeerAddress{FabricIndex: 1, NodeID: 1}
	// sleepingPeer is holdUntilIcdPeerAwakeTest.ts:15-25.
	sleepingPeer := func(t *testing.T) (*icd.PeerWakefulness, *clocktest.Fake) {
		t.Helper()
		fake := clocktest.New(time.Unix(1_000_000, 0))
		w := icd.NewPeerWakefulness(fake)
		w.SetTimings(icd.Timings{ActiveModeThreshold: 4 * time.Second, IdleModeDuration: 30 * time.Second})
		w.SetRequiresAwait(true)
		if w.Awake() {
			t.Fatal("awake")
		}
		return w, fake
	}
	hold := func(ctx context.Context, w *icd.PeerWakefulness, timeout time.Duration) <-chan error {
		out := make(chan error, 1)
		go func() { out <- client.HoldUntilICDPeerAwake(ctx, w, addr, timeout, nil) }()
		return out
	}
	armed := func(fake *clocktest.Fake) {
		for fake.Pending() == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	t.Run("holdUntilIcdPeerAwake", func(t *testing.T) {
		t.Parallel()
		t.Run("does not hold without a wakefulness, or for a peer that needs no awaiting", func(t *testing.T) {
			t.Parallel()
			if err := client.HoldUntilICDPeerAwake(context.Background(), nil, addr, 0, nil); err != nil {
				t.Fatal(err)
			}
			if err := client.HoldUntilICDPeerAwake(context.Background(), icd.NewPeerWakefulness(nil), addr, 0, nil); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("holds a sleeping peer until it wakes", func(t *testing.T) {
			t.Parallel()
			w, fake := sleepingPeer(t)
			ch := hold(context.Background(), w, 0)
			armed(fake)
			w.NoteActive()
			if err := <-ch; err != nil {
				t.Fatal(err)
			}
		})
		t.Run("rejects with IcdPeerAsleepError when the peer does not wake in time", func(t *testing.T) {
			t.Parallel()
			w, fake := sleepingPeer(t)
			ch := hold(context.Background(), w, 5*time.Second)
			armed(fake)
			fake.Advance(5 * time.Second)
			var e *client.ICDPeerAsleepError
			if err := <-ch; !errors.As(err, &e) {
				t.Fatal(err)
			}
		})
		t.Run("rejects with the abort error when aborted", func(t *testing.T) {
			t.Parallel()
			w, fake := sleepingPeer(t)
			ctx, cancel := context.WithCancel(context.Background())
			ch := hold(ctx, w, 0)
			armed(fake)
			cancel()
			if err := <-ch; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	})
}

func TestClientNodePhysicalPropertiesIcdParity(t *testing.T) {
	t.Run("ClientNodePhysicalProperties ICD operating mode", func(t *testing.T) {
		t.Run("reports isLongIdleTimeOperating true for a peer operating in LIT mode", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true), forceLIT: true})
			if pr := p.node.Properties(); !pr.IntermittentlyConnected || !pr.LongIdleTimeOperating {
				t.Fatalf("%+v", pr)
			}
		})
		t.Run("reports isLongIdleTimeOperating false for a LIT-capable peer still operating in SIT mode", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{cfg: icdLITConfig(true)})
			if pr := p.node.Properties(); !pr.IntermittentlyConnected || pr.LongIdleTimeOperating {
				t.Fatalf("%+v", pr)
			}
		})
		t.Run("reports isLongIdleTimeOperating false for a CIP-only (SIT) ICD peer", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			if pr := p.node.Properties(); !pr.IntermittentlyConnected || pr.LongIdleTimeOperating {
				t.Fatalf("%+v", pr)
			}
		})
		t.Run("reports isLongIdleTimeOperating false for a non-ICD peer", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{noICD: true})
			if pr := p.node.Properties(); pr.IntermittentlyConnected || pr.LongIdleTimeOperating {
				t.Fatalf("%+v", pr)
			}
		})
	})
}

// registeredLITPair is ClientNodeInteractionIcdTest.ts:51-71: a LIT-operating
// DSLS device, re-registered under a subject the subscription does not
// cover, the awake window of the registration lapsed.
func registeredLITPair(t *testing.T) *icdPair {
	t.Helper()
	p := litOperatingPair(t)
	if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
		t.Fatal(err)
	}
	if err := p.do(t, func(ctx context.Context) error {
		if err := p.icd().Register(ctx, uncovered); err != nil && !errors.Is(err, client.ErrICDOperation) {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p.eventually(t, "registered", p.icd().Registered)
	// The awake window the registration seeded lapses; the site moves its
	// clock to the window's end once the mode-flip recreates settled.
	p.eventually(t, "the peer asleep", func() bool { w := p.wakefulness(t); return w != nil && w.RequiresAwait() && !w.Awake() })
	return p
}

// descriptorRead is ClientNodeInteractionIcdTest.ts:46-48.
var descriptorRead = client.ReadRequest{ReadRequest: client.ReadPaths{
	Attributes: []client.AttributePath{client.AttributeAt(0, descriptor.ClusterID, descriptor.AttrServerList)},
}}

// readAsync starts a read under the site and returns its outcome.
func (p *icdPair) readAsync(ctx context.Context, req client.ReadRequest) <-chan error {
	out := make(chan error, 1)
	go func() {
		_, err := p.node.Read(ctx, req)
		out <- err
	}()
	return out
}

func TestClientNodeInteractionIcdParity(t *testing.T) {
	t.Run("ClientNodeInteraction ICD hold", func(t *testing.T) {
		t.Run("holds a read for an idle LIT peer and completes it once a check-in wakes the peer", func(t *testing.T) {
			p := registeredLITPair(t)
			if p.wakefulness(t).Awake() {
				t.Fatal("awake")
			}
			read := p.readAsync(parityCtx(t), descriptorRead)
			time.Sleep(20 * time.Millisecond)
			select {
			case err := <-read:
				t.Fatalf("the read went out: %v", err)
			default:
			}
			p.wakeDevice(t)
			var err error
			p.eventually(t, "the held read", func() bool {
				select {
				case err = <-read:
					return true
				default:
					return false
				}
			})
			if err != nil {
				t.Fatal(err)
			}
		})
		t.Run("rejects with IcdPeerAsleepError when no check-in arrives within the default window", func(t *testing.T) {
			p := registeredLITPair(t)
			p.silence(true)
			read := p.readAsync(parityCtx(t), descriptorRead)
			time.Sleep(20 * time.Millisecond)
			p.advance(t, 3700*time.Second)
			var e *client.ICDPeerAsleepError
			if err := <-read; !errors.As(err, &e) {
				t.Fatal(err)
			}
		})
		t.Run("passes a read through immediately when the LIT peer is awake", func(t *testing.T) {
			p := registeredLITPair(t)
			got := &checkIns{}
			p.icd().OnCheckIn(got.record)
			p.wakeDevice(t)
			p.eventually(t, "the Check-In", func() bool { return got.n() > 0 })
			if !p.wakefulness(t).Awake() {
				t.Fatal("asleep")
			}
			if err := p.do(t, func(ctx context.Context) error { _, err := p.node.Read(ctx, descriptorRead); return err }); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("passes a read through immediately for a non-LIT peer", func(t *testing.T) {
			p := newICDPair(t, icdPairOptions{})
			p.register(t, client.ICDRegisterOptions{})
			if p.wakefulness(t).RequiresAwait() {
				t.Fatal("awaited")
			}
			if err := p.do(t, func(ctx context.Context) error { _, err := p.node.Read(ctx, descriptorRead); return err }); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("breaks the hold deadlock after forget(): a read completes without a check-in", func(t *testing.T) {
			p := registeredLITPair(t)
			if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
				t.Fatal(err)
			}
			if p.icd().Registered() || p.wakefulness(t) != nil {
				t.Fatal("still registered")
			}
			if err := p.do(t, func(ctx context.Context) error { _, err := p.node.Read(ctx, descriptorRead); return err }); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("routes a woken LIT read through the icdLit network profile", func(t *testing.T) {
			t.Skip("matter.js NetworkProfiles: this module has no network profiles and throttles no connection until milestone 5 phase 25 (notes/parity/matter_behaviour_findings.md, \"Network profiles\"; the register's NetworkProfileTest); the priority intent travels with the sustained subscription's connection (TestSustainedSubscriptionICD)")
		})
		t.Run("honors a per-call timeout override that expires before the default window", func(t *testing.T) {
			p := registeredLITPair(t)
			p.silence(true)
			read := p.readAsync(client.WithICDAwaitTimeout(parityCtx(t), 10*time.Second), descriptorRead)
			time.Sleep(20 * time.Millisecond)
			p.advance(t, 15*time.Second)
			var e *client.ICDPeerAsleepError
			if err := <-read; !errors.As(err, &e) || e.Timeout != 10*time.Second {
				t.Fatal(err)
			}
		})
		t.Run("abandons a held read when its request aborts", func(t *testing.T) {
			p := registeredLITPair(t)
			p.silence(true)
			ctx, cancel := context.WithCancel(parityCtx(t))
			read := p.readAsync(ctx, descriptorRead)
			time.Sleep(20 * time.Millisecond)
			cancel()
			if err := <-read; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
		t.Run("abandons a held write when its request aborts", func(t *testing.T) {
			p := registeredLITPair(t)
			p.silence(true)
			ctx, cancel := context.WithCancel(parityCtx(t))
			out := make(chan error, 1)
			go func() {
				_, err := p.node.Write(ctx, []client.WriteRequest{{Path: client.AttributeAt(0, 0x0028, 0x0005), Value: spec.String("x")}}, client.WriteOptions{})
				out <- err
			}()
			time.Sleep(20 * time.Millisecond)
			cancel()
			if err := <-out; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
		t.Run("abandons a held command when its acting context aborts, so the device never receives it", func(t *testing.T) {
			p := registeredLITPair(t)
			p.silence(true)
			ctx, cancel := context.WithCancel(parityCtx(t))
			out := make(chan error, 1)
			go func() {
				_, err := p.icd().StayActive(ctx, time.Second)
				out <- err
			}()
			time.Sleep(20 * time.Millisecond)
			cancel()
			if err := <-out; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			p.silence(false)
			p.wakeDevice(t)
			p.advance(t, time.Second)
			if p.device().ICD().Active() != true {
				t.Fatal("the device is not active after its wake")
			}
		})
	})
}
