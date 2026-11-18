// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

const (
	hostPasscode   = uint32(20202021)
	hostIterations = 1000
)

var hostSalt = []byte("SPAKE2P Key Salt")

// hostRig is a bridge a host wired without the node facade, from the
// list in bridge/bridge.go's package doc: ACL lister, session lookup and
// registry (a secure/operational Manager), ACK tracker, a per-exchange
// PASE acceptor, and the root's GeneralCommissioning on a fake clock. The
// PASE acceptor's established-session callback installs the session and
// arms the fail-safe for it, as the node facade's does
// (node/security.go buildPaseAdapterFromContext).
type hostRig struct {
	b        *bridge.Bridge
	sessions *operational.Manager
	gc       *core.GeneralCommissioning
	clk      *clocktest.Fake
}

func newHostRig(t *testing.T) *hostRig {
	t.Helper()
	r := &hostRig{clk: clocktest.New(time.Unix(1_700_000_000, 0))}
	b, err := bridge.New(bridge.NewEmptySnapshotter(), mdns.NewNoop(), bridge.Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.b = b
	r.sessions = operational.NewManager(nil)
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600, Clock: r.clk,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.gc = gc

	b.AttachACLLister(endpoint.UnenforcedACL{})
	sessions := r.sessions
	b.AttachSessionLookup(bridge.NewOperationalSessionLookup(func(id uint16) (*channel.Session, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return nil, false
		}
		return e.Session, true
	}).WithFabricResolver(func(id uint16) (uint8, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return 0, false
		}
		return e.FabricIndex(), true
	}).WithPASEResolver(func(id uint16) (bool, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return false, false
		}
		return e.IsPASE(), true
	}))
	b.AttachAckTracker(mrp.NewAckTracker(mrp.DefaultStandaloneAckDelay))
	b.AttachSessionRegistry(sessions)
	b.AttachRootClusters([]contract.ClusterServer{gc})

	vc, err := spake2.NewVerifierContext(hostPasscode, hostSalt, hostIterations)
	if err != nil {
		t.Fatal(err)
	}
	life := t.Context()
	provider := bridge.NewPerExchangePaseProvider(func() *bridge.PaseAdapter {
		stake, err := sessions.AllocateStake()
		if err != nil {
			t.Errorf("allocate a PASE session id: %v", err)
			return nil
		}
		a := bridge.NewPaseAdapterWithFactory(func(transcript []byte) *spake2.Verifier {
			return spake2.NewVerifier(vc, nil, nil, transcript)
		})
		a.SetPBKDFParams(hostIterations, hostSalt, stake.ID)
		a.SetOnUnfinished(func(uint16) { sessions.ReleaseStake(stake) })
		a.SetOnSessionEstablished(func(secret []byte, peerSessionID uint16) error {
			defer clear(secret)
			e, err := sessions.OpenFromPaseWithStake(stake, 0, 0, peerSessionID, secret)
			if err != nil {
				sessions.ReleaseStake(stake)
				return err
			}
			// The bridge closes the PASE sessions when this fail-safe
			// expires.
			if err := gc.ArmOnPaseSession(life, stake.ID); err != nil {
				_ = sessions.Close(e.SessionID)
				return err
			}
			return nil
		})
		return a
	})
	b.AttachPaseHandlerProvider(provider.Resolve)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	return r
}

func (r *hostRig) commissioner(t *testing.T, fabricID uint64) *bridgetest.Commissioner {
	t.Helper()
	return bridgetest.Dial(t, r.b.LocalAddr(), bridgetest.Options{
		Passcode: hostPasscode, FabricID: fabricID, NodeID: 0x1234,
	})
}

// waitFor yields until cond holds, failing after 10 s of real time.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// TestHostWithoutTheFacadeBoundsAPASESession: a host that wires the bridge
// itself, with the ports bridge/bridge.go's package doc lists and no node
// facade, gets the PASE session's bounds from the bridge: while a PASE
// session is established no second PASE is accepted (the registry lists
// it, Manager.PASESessionIDs), and when the session's fail-safe expires
// the bridge closes it (AttachRootClusters wires the expiry, the registry
// names the session) and accepts a new PASE again (TM R2.1 rest).
func TestHostWithoutTheFacadeBoundsAPASESession(t *testing.T) {
	t.Parallel()
	r := newHostRig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	first, err := r.commissioner(t, 1).PASE(ctx)
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	if ids := r.sessions.PASESessionIDs(); len(ids) != 1 {
		t.Fatalf("PASE sessions after the handshake: %v, want one", ids)
	}
	if !r.gc.FailSafeArmed() {
		t.Fatal("no fail-safe armed for the PASE session")
	}
	breadcrumb := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0030, Attribute: 0x0000}
	if _, err := first.Read(ctx, breadcrumb); err != nil {
		t.Fatalf("read over the PASE session: %v", err)
	}

	// A second commissioner while the session exists: ignored, its
	// handshake runs out.
	short, cancelShort := context.WithTimeout(ctx, 3*time.Second)
	_, err = r.commissioner(t, 2).PASE(short)
	cancelShort()
	if err == nil {
		t.Fatal("a second PASE was accepted while a PASE session existed")
	}

	// The fail-safe expires: the bridge closes the PASE session.
	r.clk.Advance(time.Duration(core.PaseSessionAutoArmSeconds+1) * time.Second)
	waitFor(t, "the bridge to close the PASE session at the fail-safe's expiry", func() bool {
		return len(r.sessions.PASESessionIDs()) == 0
	})
	late, cancelLate := context.WithTimeout(ctx, 2*time.Second)
	_, err = first.Read(late, breadcrumb)
	cancelLate()
	if err == nil {
		t.Fatal("the PASE session answered after its fail-safe expired")
	}

	// And accepts a new PASE.
	if _, err := r.commissioner(t, 3).PASE(ctx); err != nil {
		t.Fatalf("PASE after the first session closed: %v", err)
	}
}
