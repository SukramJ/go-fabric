// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster/threadbr"
	"thingfabric.dev/go-fabric/thread"
)

// DefaultPollInterval is how often an activation reads /node/state while
// it waits for the border router to attach.
const DefaultPollInterval = 500 * time.Millisecond

// BorderRouterConfig carries a [BorderRouter]'s settings.
type BorderRouterConfig struct {
	// Name is BorderRouterName, the border router's MeshCoP service name
	// (ThreadBorderRouterManagementDelegate.h:68); the REST surface does
	// not serve it, so the host names it. 1 to 63 bytes.
	Name string
	// ThreadVersion is the Thread version the border router reports when
	// its own diagnostics record carries no Version TLV (the Version TLV
	// mapping, Delegate.h:78; 4 is Thread 1.3, 5 Thread 1.4). Zero is
	// refused then.
	ThreadVersion uint16
	// PollInterval is how often an activation reads /node/state; 0 is
	// [DefaultPollInterval].
	PollInterval time.Duration
	// Clock paces the poll; the system clock when nil.
	Clock clock.Clock
	// Logger receives the diagnostics' translation notes (a field of a
	// /diagnostics entry skipped for its shape); slog.Default when nil.
	Logger *slog.Logger
}

// ErrNoThreadVersion is a border router whose diagnostics carry no
// Version and whose configuration names none.
var ErrNoThreadVersion = errors.New("otbr: the border router reports no Thread version")

// BorderRouter is an OpenThread Border Router reached over REST as the
// cluster servers' [threadbr.BorderRouter]: connectedhomeip's
// GenericOpenThreadBorderRouterDelegate
// (src/platform/OpenThread/GenericThreadBorderRouterDelegate.cpp at
// 6170af84) over the REST API instead of an in-process OpenThread
// instance. It holds what that delegate keeps in its storage — whether
// the active dataset is configured (kFailsafeActiveDatasetConfigured,
// :180-184) — in memory; a border router restarted with an activation
// pending is not reverted (Init, :56-64).
type BorderRouter struct {
	c   *Client
	cfg BorderRouterConfig

	// mu guards configured, activations and pending.
	mu sync.Mutex
	// configured is chip's ActiveDatasetConfigured, narrowed to this
	// delegate's own activations (review II, R3-fsm-3350; decision 545):
	// false only while an activation onto a border router without an
	// active dataset is uncommitted. A CommissioningComplete sets it true
	// when the border router is attached and otherwise leaves it as it
	// is; a clear returns it to true. chip saves IsThreadAttached() at
	// every CommissioningComplete (:186-189;
	// ThreadBorderRouterManagementCluster.cpp:319-322); over REST the border router
	// has other writers, so a commit while it is not attached must not arm
	// the fail-safe's revert against a dataset no activation of this node
	// set.
	configured bool
	// activations numbers the activations onto a border router without an
	// active dataset, and pending holds those not proven unwritten: an
	// activation adds its number, its own 4xx takes it off, and a commit
	// or a clear takes off those numbered before it started; configured
	// is true exactly when pending is empty (review II, R4-fsm-4303,
	// R5-fsm-5401).
	activations uint64
	pending     map[uint64]struct{}
}

var _ threadbr.BorderRouter = (*BorderRouter)(nil)

// NewBorderRouter returns c as a border router.
func NewBorderRouter(c *Client, cfg BorderRouterConfig) (*BorderRouter, error) {
	if c == nil {
		return nil, errors.New("otbr: no client")
	}
	if n := len(cfg.Name); n == 0 || n > 63 {
		return nil, fmt.Errorf("otbr: a border router name of %d bytes, want 1 to 63", n)
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	cfg.Clock = clock.Or(cfg.Clock)
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &BorderRouter{c: c, cfg: cfg, configured: true, pending: map[uint64]struct{}{}}, nil
}

// Client is the border router's REST client.
func (b *BorderRouter) Client() *Client { return b.c }

// BorderRouterName implements [threadbr.BorderRouter].
func (b *BorderRouter) BorderRouterName() string { return b.cfg.Name }

// BorderAgentID implements [threadbr.BorderRouter]: /node/ba-id
// (GetBorderAgentId, :66-86).
func (b *BorderRouter) BorderAgentID(ctx context.Context) ([]byte, error) {
	return b.c.BorderAgentID(ctx)
}

// ThreadVersion implements [threadbr.BorderRouter]: the Version of the
// border router's own diagnostics record, the configured one without
// (GetThreadVersion, :88-91, reads otThreadGetVersion).
func (b *BorderRouter) ThreadVersion(ctx context.Context) (uint16, error) {
	if d, err := b.Diagnostics(ctx); err == nil && d.Self != nil && d.Self.Version != nil {
		return *d.Self.Version, nil
	}
	if b.cfg.ThreadVersion == 0 {
		return 0, ErrNoThreadVersion
	}
	return b.cfg.ThreadVersion, nil
}

// InterfaceEnabled implements [threadbr.BorderRouter]: the role is not
// disabled (GetInterfaceEnabled, :93-99, reads otIp6IsEnabled).
func (b *BorderRouter) InterfaceEnabled(ctx context.Context) (bool, error) {
	role, err := b.c.State(ctx)
	if err != nil {
		return false, err
	}
	return role != thread.RoleDisabled, nil
}

// Dataset implements [threadbr.BorderRouter]: /node/dataset/active or
// /pending, nil for none (GetDataset, :101-125).
func (b *BorderRouter) Dataset(ctx context.Context, pending bool) ([]byte, error) {
	if pending {
		return b.c.PendingDataset(ctx)
	}
	return b.c.ActiveDataset(ctx)
}

// SetActiveDataset implements [threadbr.BorderRouter]: chip's
// SetActiveDataset (:127-143) attaches the border router to the dataset
// and completes on kConnectivity_Established (:145-157). Over REST: an
// activation onto a border router without an active dataset marks the
// dataset not configured (SaveActiveDatasetConfigured(false), :131); the
// Thread interface is disabled first when a dataset is active, since the
// OTBR refuses to replace it in place (409, OtbrRestClient.ts:421-422);
// then PUT /node/dataset/active, PUT /node/state "enable", and /node/state
// is read every PollInterval until the role is attached — or ctx ends,
// which the fail-safe's expiry does. The empty dataset clears the active
// one, as chip attaches an empty dataset to clear it (:202-210). A PUT
// the border router refuses with a 4xx wrote nothing and takes only this
// activation off the uncommitted ones, ahead of chip (review II,
// R4-fsm-4302, R5-fsm-5401; decision 545); a transport failure or a 5xx
// leaves the revert owed, as chip leaves the flag on every error
// (:136-140).
func (b *BorderRouter) SetActiveDataset(ctx context.Context, dataset []byte) error {
	current, err := b.c.ActiveDataset(ctx)
	if err != nil {
		return err
	}
	if len(dataset) == 0 {
		return b.clear(ctx)
	}
	var activation uint64
	if current == nil {
		b.mu.Lock()
		b.activations++
		activation = b.activations
		b.pending[activation] = struct{}{}
		b.configured = false
		b.mu.Unlock()
	} else if bytes.Equal(current, dataset) {
		if role, err := b.c.State(ctx); err == nil && role.Attached() {
			return nil
		}
	}
	if current != nil {
		if err := b.c.setState(ctx, false); err != nil {
			return err
		}
	}
	if err := b.c.setDataset(ctx, false, dataset); err != nil {
		if current == nil && refused(err) {
			// The border router refused the PUT: nothing of this
			// activation was written, so it takes only itself off — an
			// other activation still uncommitted keeps its revert, and
			// overlapping refused ones each take themselves off (review
			// II, R4-fsm-4302, R5-fsm-5401).
			b.mu.Lock()
			delete(b.pending, activation)
			b.configured = len(b.pending) == 0
			b.mu.Unlock()
		}
		return b.reenable(ctx, current != nil, err)
	}
	if err := b.c.setState(ctx, true); err != nil {
		return b.reenable(ctx, current != nil, err)
	}
	return b.awaitAttached(ctx)
}

// refused reports whether err is a 4xx answer of the border router: the
// request was refused and nothing it carried was written. A transport
// failure, a timeout or a 5xx (which something in front of the border
// router may answer) does not prove that, and leaves the revert owed.
func refused(err error) bool {
	var oe *Error
	return errors.As(err, &oe) && oe.HTTPStatus >= 400 && oe.HTTPStatus < 500
}

// settle takes off the activations numbered up to since, those that
// started before the commit or the clear that read it; an activation that
// started after is still uncommitted and keeps the flag false (review II,
// R4-fsm-4303, R5-fsm-5401).
func (b *BorderRouter) settle(since uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for n := range b.pending {
		if n <= since {
			delete(b.pending, n)
		}
	}
	b.configured = len(b.pending) == 0
}

// reenable is the step back of an activation that disabled the Thread
// interface and then failed: the interface is enabled again — once, on
// the client's own bound, also when the activation's context has ended —
// so the border router is not left off the network it was on, which
// RevertActiveDataset would not restore while a dataset is configured
// (review II, R2-P6-err-2025). The error is the activation's, joined with
// the re-enable's.
func (b *BorderRouter) reenable(ctx context.Context, disabled bool, err error) error {
	if !disabled {
		return err
	}
	if rerr := b.c.setState(context.WithoutCancel(ctx), true); rerr != nil {
		return errors.Join(err, fmt.Errorf("otbr: re-enable the Thread interface: %w", rerr))
	}
	return err
}

// awaitAttached reads /node/state until the border router is attached
// (ADR 0027 decision 4: the REST surface has no attach callback).
func (b *BorderRouter) awaitAttached(ctx context.Context) error {
	tick := b.cfg.Clock.NewTicker(b.cfg.PollInterval)
	defer tick.Stop()
	for {
		role, err := b.c.State(ctx)
		if err == nil && role.Attached() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("otbr: the border router did not attach (role %q): %w", role, ctx.Err())
		case <-tick.C():
		}
	}
}

// clear disables the Thread interface and deletes the active dataset; on
// success nothing is left armed: the dataset is deleted or, at a 405,
// left in place with the interface disabled. An activation that started
// while the clear ran keeps its flag (review II, R4-doc-4304,
// R4-fsm-4303).
func (b *BorderRouter) clear(ctx context.Context) error {
	b.mu.Lock()
	since := b.activations
	b.mu.Unlock()
	if err := b.c.setState(ctx, false); err != nil {
		return err
	}
	if err := b.c.deleteActiveDataset(ctx); err != nil {
		// ot-br-posix at main registers only GET and PUT on
		// /node/dataset/active (rest_web_server.cpp, RegisterGet/RegisterPut
		// of OT_REST_RESOURCE_PATH_NODE_DATASET_ACTIVE) and answers DELETE
		// with 405; matter.js's deleteActiveDataset (OtbrRestClient.ts:428-433)
		// targets a firmware that had the route. chip's revert attaches an
		// empty dataset (GenericThreadBorderRouterDelegate.cpp:191-208), which
		// REST cannot express, so the revert ends with the interface disabled
		// and the dataset left in place.
		var oe *Error
		if !errors.As(err, &oe) || oe.Code != CodeNotAllowed {
			return err
		}
	}
	// Nothing is left armed: the dataset is deleted or, at a 405, left
	// in place with the interface disabled.
	b.settle(since)
	return nil
}

// CommitActiveDataset implements [threadbr.BorderRouter]: an uncommitted
// activation is committed when the border router is attached
// (CommitActiveDataset, :186-189). A border router found not attached
// leaves the flag as it is: only an activation of this delegate leaves
// the dataset to the fail-safe's revert (review II, R3-fsm-3350). An
// activation that started after the commit read the border router's state
// stays uncommitted (review II, R5-fsm-5401).
func (b *BorderRouter) CommitActiveDataset(ctx context.Context) error {
	b.mu.Lock()
	since := b.activations
	b.mu.Unlock()
	role, err := b.c.State(ctx)
	if err != nil {
		return err
	}
	if !role.Attached() {
		return nil
	}
	b.settle(since)
	return nil
}

// RevertActiveDataset implements [threadbr.BorderRouter]: a dataset an
// activation of this delegate set onto a border router that had none, and
// that no CommissioningComplete committed, is cleared (RevertActiveDataset,
// :191-212); otherwise nothing changes. A clear that succeeded leaves
// nothing armed, unless an activation started while it ran.
func (b *BorderRouter) RevertActiveDataset(ctx context.Context) error {
	b.mu.Lock()
	configured := b.configured
	b.mu.Unlock()
	if configured {
		return nil
	}
	return b.clear(ctx)
}

// SetPendingDataset implements [threadbr.BorderRouter]: PUT
// /node/dataset/pending (SetPendingDataset, :214-230).
func (b *BorderRouter) SetPendingDataset(ctx context.Context, dataset []byte) error {
	return b.c.setDataset(ctx, true, dataset)
}

// Diagnostics implements [threadbr.BorderRouter]: the node from /node,
// the mesh from /diagnostics (each entry translated, one that is not an
// object skipped) and the border router's own record among them, found by
// its extended address or its RLOC16. A border router that serves no
// /diagnostics has no records; /node failing is the error.
func (b *BorderRouter) Diagnostics(ctx context.Context) (thread.NodeDiagnostics, error) {
	node, err := b.c.Node(ctx)
	if err != nil {
		return thread.NodeDiagnostics{}, err
	}
	out := thread.NodeDiagnostics{
		Role: thread.DeviceRole(node.State), ExtAddress: node.ExtAddress, Rloc16: node.Rloc16, LeaderData: node.LeaderData,
	}
	entries, err := b.c.Diagnostics(ctx)
	if err != nil {
		return out, nil //nolint:nilerr // a border router without /diagnostics still reports its node
	}
	for _, e := range entries {
		r, err := translateEntry(e, b.cfg.Logger)
		if err != nil {
			continue
		}
		out.Mesh = append(out.Mesh, r)
	}
	for i := range out.Mesh {
		r := &out.Mesh[i]
		if bytes.Equal(r.ExtMACAddress, node.ExtAddress) || (len(r.ExtMACAddress) == 0 && r.Rloc16 != nil && *r.Rloc16 == node.Rloc16) {
			self := *r
			out.Self = &self
			break
		}
	}
	return out, nil
}
