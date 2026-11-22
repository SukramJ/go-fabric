// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The fail-safe bounds of [Commissioner.UpdateOperationalCertificate].
// Mirrors matter.js packages/protocol/src/peer/OperationalCertificateUpdate.ts:56
// (COMMISSIONING_COMPLETE_MARGIN), :59 (MIN_FAILSAFE), :62 (MAX_FAILSAFE)
// and :108 (DEFAULT_FAILSAFE) at b25ed960.
const (
	// DefaultCertificateUpdateFailsafe is the fail-safe armed for the whole
	// update when [CertificateUpdateOptions].Failsafe is 0.
	DefaultCertificateUpdateFailsafe = time.Minute
	// MinCertificateUpdateFailsafe is the shortest fail-safe: it leaves
	// time for the CASE reconnect after the margin kept back for
	// CommissioningComplete.
	MinCertificateUpdateFailsafe = 30 * time.Second
	// MaxCertificateUpdateFailsafe is the longest: ArmFailSafe carries the
	// expiry as uint16 seconds (§ 11.10.7.2).
	MaxCertificateUpdateFailsafe = 0xffff * time.Second

	// commissioningCompleteMargin is the fail-safe time kept back so
	// CommissioningComplete can still arrive after the reconnect.
	commissioningCompleteMargin = 10 * time.Second
)

// forgetDisconnectBound bounds forgetSessions' wait for the peer's
// connection process to stop: this module's, matter.js awaits
// Peer.disconnect without one. A variable for the tests.
var forgetDisconnectBound = 5 * time.Second

// CertificateUpdateOptions configures
// [Commissioner.UpdateOperationalCertificate] — matter.js
// OperationalCertificateUpdate.Options (OperationalCertificateUpdate.ts:121-138
// at b25ed960); the abort signal is the call's context.
type CertificateUpdateOptions struct {
	// Address is the peer whose certificate to replace. Required.
	Address handshake.PeerAddress
	// Fabric is the fabric of Address — matter.js peer.fabric; nil looks it
	// up among the commissioner's [Context].Fabrics by the address's
	// fabric index.
	Fabric *Fabric
	// Failsafe is the fail-safe armed for the whole update, including the
	// CASE reconnect with the new certificate: 0 is
	// [DefaultCertificateUpdateFailsafe]; it must lie between
	// [MinCertificateUpdateFailsafe] and [MaxCertificateUpdateFailsafe]. A
	// peer ends the fail-safe early at its MaxCumulativeFailsafeSeconds.
	Failsafe time.Duration
}

// CertificateUpdateResult is the result of a successful
// [Commissioner.UpdateOperationalCertificate] — matter.js
// OperationalCertificateUpdate.Result (OperationalCertificateUpdate.ts:145-154
// at b25ed960).
type CertificateUpdateResult struct {
	// FabricIndexOnPeer is the CurrentFabricIndex the peer reported for
	// our fabric before the update. An OK NOCResponse carries the same
	// index; after a lost NOCResponse it is not confirmed that way.
	FabricIndexOnPeer uint8
	// OperationalPublicKey is the public key of the new node operational
	// key pair.
	OperationalPublicKey []byte
}

// CertificateUpdateOutcome is which certificate the peer holds after a
// failed update, as far as the update could determine it. matter.js
// states it only in the error's message
// (OperationalCertificateUpdate.ts:281-284, :329-352 at b25ed960); this
// module carries it as a value, so a host decides retry or alarm without
// parsing English (the Ahead of matter.js register, owner decision W2).
type CertificateUpdateOutcome int

// The outcomes of a failed update. The zero value is unset: the error is
// the cause of another [*CertificateUpdateError], a step's failure whose
// outcome the outer error states.
const (
	// CertificateUpdatePreviousKept: the update changed nothing on the
	// peer, which keeps its previous certificate (:281-284).
	CertificateUpdatePreviousKept CertificateUpdateOutcome = iota + 1
	// CertificateUpdateReverted: the peer installed the new certificate
	// and reverted to its previous one (:340-344).
	CertificateUpdateReverted
	// CertificateUpdateNewInPlace: the new certificate is in place,
	// because another operation of the same fabric completed the
	// fail-safe after the update refused to commit it (:329-334).
	CertificateUpdateNewInPlace
	// CertificateUpdateUnknown: the update could not determine which
	// certificate the peer holds; the peer reverts to its previous one
	// when its fail-safe expires unless this update or another operation
	// of the fabric completed it (:346-351).
	CertificateUpdateUnknown
)

func (o CertificateUpdateOutcome) String() string {
	switch o {
	case CertificateUpdatePreviousKept:
		return "previous certificate kept"
	case CertificateUpdateReverted:
		return "reverted to the previous certificate"
	case CertificateUpdateNewInPlace:
		return "new certificate in place"
	case CertificateUpdateUnknown:
		return "unknown"
	}
	return "unset"
}

// The kinds of a refused start, which errors.Is finds in a
// [*CertificateUpdateError]. Nothing changed on the peer; the error's
// Outcome is [CertificateUpdatePreviousKept].
var (
	// ErrCertificateUpdateBusy: the fail-safe of the peer may be in use —
	// ArmFailSafe answered BusyWithOtherAdmin, or Breadcrumb was not 0
	// before arming. The caller may retry later. matter.js
	// OperationalCertificateUpdateBusyError (OperationalCertificateUpdate.ts:89-93
	// at b25ed960).
	ErrCertificateUpdateBusy = errors.New("commissioner: the fail-safe of the peer may be in use")
	// ErrCertificateUpdateRefused: the peer refused ArmFailSafe with an
	// error code other than BusyWithOtherAdmin, or the certificate it
	// reports for its fabric does not verify. A retry fails the same way
	// until the cause on the peer is fixed. matter.js
	// OperationalCertificateUpdateRefusedError (:95-100).
	ErrCertificateUpdateRefused = errors.New("commissioner: the peer refused the operational certificate update")
)

// CertificateUpdateError is a failed
// [Commissioner.UpdateOperationalCertificate] once the update reached the
// peer over CASE — matter.js OperationalCertificateUpdateError with its
// Busy and Refused subclasses (OperationalCertificateUpdate.ts:64-100 at
// b25ed960), the subclass an errors.Is kind ([ErrCertificateUpdateBusy],
// [ErrCertificateUpdateRefused]).
type CertificateUpdateError struct {
	// Outcome is which certificate the peer holds afterwards; zero on an
	// error that is the cause of another.
	Outcome CertificateUpdateOutcome
	// FabricIndexOnPeer is the CurrentFabricIndex the peer reported for
	// our fabric at the start of the update; 0 when the update failed
	// before or while reading it, or the peer reported an invalid value.
	FabricIndexOnPeer uint8
	// Message is matter.js's message.
	Message string
	// Cause is the underlying error, nil when there is none.
	Cause error

	// kind is ErrCertificateUpdateBusy or ErrCertificateUpdateRefused, nil
	// for the base class.
	kind error
}

func (e *CertificateUpdateError) Error() string {
	if e.Cause != nil {
		return "commissioner: " + e.Message + ": " + e.Cause.Error()
	}
	return "commissioner: " + e.Message
}

// Unwrap makes the kind and the cause match errors.Is and errors.As.
func (e *CertificateUpdateError) Unwrap() []error {
	var out []error
	if e.kind != nil {
		out = append(out, e.kind)
	}
	if e.Cause != nil {
		out = append(out, e.Cause)
	}
	return out
}

// armRefusal reports whether err is a Busy or Refused error: the update
// armed no fail-safe, so nothing on the peer needs to be undone. Mirrors
// matter.js OperationalCertificateUpdate.ts:842-847 at b25ed960
// (isArmRefusal).
func armRefusal(err error) bool {
	var e *CertificateUpdateError
	return errors.As(err, &e) && (e.kind == ErrCertificateUpdateBusy || e.kind == ErrCertificateUpdateRefused) //nolint:errorlint // the identity of the kind
}

// ErrFabricChanged reports that the fabric associated with the peer's session has
// changed — the cause an operational certificate update force-closes the
// peer's sessions with (forgetSessions). A request pending on one of
// them, or started on one before it closes, fails with an error that
// matches it under errors.Is; the error's text carries the update's
// reason. It is not transient: it does not match exchange.ErrTransient.
// Mirrors matter.js FabricChangedError, which extends PeerCommunicationError,
// not TransientPeerCommunicationError:
// packages/protocol/src/peer/PeerCommunicationError.ts:105-112 at b25ed960 (FabricChangedError)
// packages/protocol/src/peer/OperationalCertificateUpdate.ts:790 at b25ed960 (forgetSessions' disconnect cause)
var ErrFabricChanged = errors.New("commissioner: the fabric associated with this session has changed")

// fabricChangedError is the reason-carrying form of ErrFabricChanged the
// update force-closes the peer's sessions with, the update's reason as
// its text (OperationalCertificateUpdate.ts:790 passes the reason as the
// message).
type fabricChangedError struct{ reason string }

func (e fabricChangedError) Error() string { return "commissioner: " + e.reason }

// Is makes the error match ErrFabricChanged; it matches no other target,
// exchange.ErrTransient among them.
func (fabricChangedError) Is(target error) bool { return target == ErrFabricChanged }

// nocUpdateSeam is what the update reaches the peer through: its invokes
// and reads, the CASE connection, the resumption record's delete and the
// subscriptions' restart. A test replaces them to inject the faults
// matter.js injects in packages/node/test/node/OperationalCertificateUpdateTest.ts
// at b25ed960, by replacing
// ClientInteraction.prototype.invoke and read (:304-355,
// :465-498),
// Peer.prototype.connect (:741-749),
// SessionManager.prototype.deleteResumptionRecord (:1567-1570),
// ClientSubscriptions.prototype.closeForPeer (:1571-1576).
// disconnect is the peer's Disconnect; opened, when set, sees each
// interaction the update binds to a session.
type nocUpdateSeam struct {
	invoke                 func(ctx context.Context, ia *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error)
	read                   func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error)
	connect                func(ctx context.Context, p *peer.Peer, opts peer.ConnectOptions) (*exchange.Session, error)
	deleteResumptionRecord func(a handshake.PeerAddress) error
	closeForPeer           func(a handshake.PeerAddress)
	disconnect             func(ctx context.Context, p *peer.Peer, cause error)
	opened                 func(ia *interaction.Interaction)
}

// defaultNOCUpdateSeam is the seam of c without a test's replacement.
func (c *Commissioner) defaultNOCUpdateSeam() nocUpdateSeam {
	return nocUpdateSeam{
		invoke: func(ctx context.Context, ia *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
			return ia.Invoke(ctx, path, fields, opts)
		},
		read: func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error) {
			report, err := ia.Read(ctx, paths...)
			if err != nil {
				return nil, nil, err
			}
			values, ok := make([]spec.Node, len(paths)), make([]bool, len(paths))
			for i, p := range paths {
				values[i], ok[i] = report.Value(p.Endpoint, p.Cluster, p.Attribute)
			}
			return values, ok, nil
		},
		connect: func(ctx context.Context, p *peer.Peer, opts peer.ConnectOptions) (*exchange.Session, error) {
			return p.Connect(ctx, opts)
		},
		deleteResumptionRecord: func(a handshake.PeerAddress) error {
			if c.ctx.Resumption == nil {
				return nil
			}
			_, err := c.ctx.Resumption.DeleteResumptionRecord(a)
			return err
		},
		disconnect: func(ctx context.Context, p *peer.Peer, cause error) { p.Disconnect(ctx, cause) },
		closeForPeer: func(a handshake.PeerAddress) {
			if c.ctx.Subscriptions != nil {
				c.ctx.Subscriptions.CloseForPeer(a)
			}
		},
	}
}

// nocSeam is the seam the update runs on.
func (c *Commissioner) nocSeam() nocUpdateSeam {
	if s := c.nocSeamOverride.Load(); s != nil {
		return *s
	}
	return c.defaultNOCUpdateSeam()
}

// UpdateOperationalCertificate replaces the node operational certificate
// (NOC) of a commissioned peer with one for a freshly generated key pair;
// the node id, the fabric, the CASE Authenticated Tags and the
// fabric-scoped data on the peer stay unchanged. Mirrors matter.js
// packages/protocol/src/peer/OperationalCertificateUpdate.ts:158-353 at
// b25ed960 (OperationalCertificateUpdate.update):
//
//	session 1: [read] ArmFailSafe ─ CertificateChainRequest(DAC) ─ CSRRequest(IsForUpdateNOC) ─ UpdateNOC
//	session 2: [read] CommissioningComplete
//
// CSRRequest and UpdateNOC share one CASE session (§ 11.18.6.7.2); the
// peer keeps that session only until the NOCResponse is delivered, so
// CommissioningComplete goes over a new CASE session that authenticates
// the new NOC. Each session's commands go through an interaction bound to
// exactly that session, closed after its steps.
//
// Before arming, the update reads Breadcrumb, the fabric's NOCs and
// CurrentFabricIndex (fabric-filtered); it arms nothing when Breadcrumb
// is not 0 ([ErrCertificateUpdateBusy]: another operation of the fabric
// may hold the fail-safe) or when the reported NOC does not verify
// against the fabric root, the peer's node id and the fabric id, or its
// CASE Authenticated Tags differ from those the peer authenticated with
// ([ErrCertificateUpdateRefused]). It arms with a random non-zero
// Breadcrumb and sends CommissioningComplete only over a session on which
// it has just read its own Breadcrumb and the new NOC; otherwise, and
// when CommissioningComplete fails, it disarms and decides from a read on
// another new session; an UpdateNOC or a later step that goes unanswered
// gets one recovery pass. The step table is matter.js's
// (OperationalCertificateUpdate.ts:185-214). After UpdateNOC, after a
// rollback and on an unknown outcome it gives up the peer's sessions,
// drops the resumption record and restarts the peer's subscriptions.
//
// ctx is matter.js's abort (W3): it ends the waits for a CASE session and
// decides whether a recovery pass runs (an abort after UpdateNOC skips
// it and reports [CertificateUpdateUnknown]). The commands, the reads and
// every disarm run under context.WithoutCancel(ctx) to their own response
// timeouts, so a cancel never leaves the peer's fail-safe armed by
// cutting a disarm or UpdateNOC short.
//
// The call is not serialized per peer: two concurrent updates of one peer
// would arm over each other. [client.Registry.UpdateOperationalCertificate]
// is the serialized entry, under the node's fabric-operation lock, as for
// a commission.
//
// Errors: a plain error for an unknown peer, a fabric whose CA the
// commissioner does not hold, a fail-safe out of range, no CASE session
// at the start or a cancel before the start read (the context's cause);
// otherwise a [*CertificateUpdateError] that states the outcome and
// carries the CurrentFabricIndex read at the start, 0 when that read
// failed.
func (c *Commissioner) UpdateOperationalCertificate(ctx context.Context, o CertificateUpdateOptions) (CertificateUpdateResult, error) {
	address := o.Address
	// OperationalCertificateUpdate.ts:249-263: the unknown peer, the CA and
	// the fail-safe range, each an ImplementationError.
	p := c.ctx.Peers.Get(address)
	if p == nil {
		return CertificateUpdateResult{}, fmt.Errorf("commissioner: cannot update the operational certificate of unknown peer %s", peerName(address))
	}
	f, err := c.fabricOf(address, o.Fabric)
	if err != nil {
		return CertificateUpdateResult{}, err
	}
	// The root of the fabric must be the CA's (:254-258): matter.js
	// compares the root certificates, this module the root public keys,
	// which a fabric keeps.
	if f.CA == nil || !bytes.Equal(f.CA.RootPublicKey(), f.RootPublicKey) {
		return CertificateUpdateResult{}, fmt.Errorf("commissioner: cannot update the operational certificate of %s because we are not the certificate authority of fabric %d", peerName(address), f.FabricIndex)
	}
	failsafe := o.Failsafe
	if failsafe == 0 {
		failsafe = DefaultCertificateUpdateFailsafe
	}
	if failsafe < MinCertificateUpdateFailsafe || failsafe > MaxCertificateUpdateFailsafe {
		return CertificateUpdateResult{}, fmt.Errorf("commissioner: fail-safe %s for the operational certificate update of %s is outside %s to %s",
			failsafe, peerName(address), MinCertificateUpdateFailsafe, MaxCertificateUpdateFailsafe)
	}

	u := &nocUpdate{c: c, seam: c.nocSeam(), peer: p, address: address, fabric: f, failsafe: failsafe}
	// :265-269: the first session, then the fail-safe's clock starts —
	// after the connect, before the reads.
	first, err := u.connectCase(ctx, 0)
	if err != nil {
		return CertificateUpdateResult{}, err
	}
	armedAt := c.ctx.Clock.Now()
	failsafeLeft := func() time.Duration {
		return max(failsafe-c.ctx.Clock.Now().Sub(armedAt), 0)
	}
	if u.breadcrumb, err = randomBreadcrumb(c.ctx.Rand); err != nil {
		first.ia.Close(nil)
		return CertificateUpdateResult{}, err
	}
	installed := u.installNewCertificate(ctx, first)
	// :273-275: the first interaction is closed however the steps ended.
	first.ia.Close(nil)

	// :277-285: a refused start throws a Busy or Refused error as it is,
	// anything else the base error: the previous certificate kept.
	if installed.kind == installRefused {
		if armRefusal(installed.cause) {
			var e *CertificateUpdateError
			errors.As(installed.cause, &e)
			e.Outcome = CertificateUpdatePreviousKept
			return CertificateUpdateResult{}, installed.cause
		}
		return CertificateUpdateResult{}, &CertificateUpdateError{
			Outcome: CertificateUpdatePreviousKept, FabricIndexOnPeer: installed.fabricIndex, Cause: installed.cause,
			Message: fmt.Sprintf("Updating the operational certificate of %s failed; it keeps its previous certificate", peerName(address)),
		}
	}
	return u.complete(ctx, installed, failsafeLeft)
}

// nocUpdate is one run of the update.
type nocUpdate struct {
	c          *Commissioner
	seam       nocUpdateSeam
	peer       *peer.Peer
	address    handshake.PeerAddress
	fabric     *Fabric
	failsafe   time.Duration
	breadcrumb uint64
}

// complete runs the passes after UpdateNOC and turns their completion
// into the result. Mirrors matter.js OperationalCertificateUpdate.ts:287-352
// at b25ed960: pass 1 within the fail-safe left after the margin; one
// recovery pass unless ctx ended; the final switch.
func (u *nocUpdate) complete(ctx context.Context, installed installed, failsafeLeft func() time.Duration) (CertificateUpdateResult, error) {
	address, fabricIndex := u.address, installed.fabricIndex
	var cause error
	if installed.kind == installUnconfirmed {
		cause = installed.cause
	}
	var done *completion
	var err error
	if installed.kind == installInstalled {
		done, err = u.completeUpdate(ctx, installed, connectionTimeoutOf(failsafeLeft()-commissioningCompleteMargin))
		if err != nil {
			cause = err
			done = nil
		}
	}
	recovered := done == nil
	if done == nil && ctx.Err() != nil {
		done = &completion{kind: completionUnknown}
	}
	if done == nil {
		done, err = u.completeUpdate(ctx, installed, 0)
		if err != nil {
			if cause != nil {
				err = &aggregateError{
					message: fmt.Sprintf("Recovering the operational certificate update of %s failed after an earlier step failed", peerName(address)),
					errs:    []error{cause, err},
				}
			}
			done = &completion{kind: completionUnknown, cause: err}
		}
	}

	stepCause := done.cause
	if stepCause == nil {
		stepCause = cause
	}
	switch done.kind {
	case completionCompleted:
		u.c.log.Info("matter.controller.noc_update.updated", slog.String("peer", peerName(address)), slog.Bool("recovered", recovered))
		if installed.refusal != nil {
			return CertificateUpdateResult{}, &CertificateUpdateError{
				Outcome: CertificateUpdateNewInPlace, FabricIndexOnPeer: fabricIndex, Cause: installed.refusal,
				Message: fmt.Sprintf("Updating the operational certificate of %s failed; the new certificate is in place, because another operation of the same fabric completed the fail-safe", peerName(address)),
			}
		}
		return CertificateUpdateResult{FabricIndexOnPeer: fabricIndex, OperationalPublicKey: installed.operationalPublicKey}, nil
	case completionReverted:
		return CertificateUpdateResult{}, &CertificateUpdateError{
			Outcome: CertificateUpdateReverted, FabricIndexOnPeer: fabricIndex, Cause: stepCause,
			Message: fmt.Sprintf("Updating the operational certificate of %s failed; it reverted to its previous certificate", peerName(address)),
		}
	default:
		u.forgetSessions(ctx, "Outcome of the operational certificate update is unknown")
		return CertificateUpdateResult{}, &CertificateUpdateError{
			Outcome: CertificateUpdateUnknown, FabricIndexOnPeer: fabricIndex, Cause: stepCause,
			Message: fmt.Sprintf("Updating the operational certificate of %s failed and its outcome is unknown; the peer reverts to its previous certificate when its fail-safe expires, unless this update or another operation of the same fabric completed that fail-safe", peerName(address)),
		}
	}
}

// fabricOf is the fabric of address: f when given (it must be the
// address's), else the commissioner's fabric of that index.
func (c *Commissioner) fabricOf(address handshake.PeerAddress, f *Fabric) (*Fabric, error) {
	if f == nil {
		for _, cand := range c.ctx.Fabrics {
			if cand.FabricIndex == address.FabricIndex {
				f = cand
				break
			}
		}
		if f == nil {
			return nil, fmt.Errorf("commissioner: cannot update the operational certificate of %s: no fabric of index %d", peerName(address), address.FabricIndex)
		}
	}
	if f.FabricIndex != address.FabricIndex {
		return nil, fmt.Errorf("commissioner: cannot update the operational certificate of %s: fabric %d is not its fabric", peerName(address), f.FabricIndex)
	}
	return f, nil
}

// connectionTimeoutOf is the connection timeout of a fail-safe budget d.
// matter.js connects with max(d, 0) (OperationalCertificateUpdate.ts:292-295
// at b25ed960), where 0 gives up at once (Peer.ts:461-462); a zero
// ConnectionTimeout is the peer set's default here, so an exhausted
// budget is the shortest positive one.
func connectionTimeoutOf(d time.Duration) time.Duration {
	return max(d, time.Nanosecond)
}

// peerName is matter.js PeerAddress.toString, "@<fabric index>:<node id>"
// in hex (packages/protocol/src/peer/PeerAddress.ts:59-62 at b25ed960).
func peerName(a handshake.PeerAddress) string {
	return fmt.Sprintf("@%x:%x", a.FabricIndex, a.NodeID)
}

// randomBreadcrumb is a random non-zero uint64, so the Breadcrumb tells
// this update's fail-safe from any other. Mirrors matter.js
// OperationalCertificateUpdate.ts:540-544 at b25ed960 (randomBreadcrumb).
func randomBreadcrumb(r io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return max(binary.BigEndian.Uint64(b[:]), 1), nil
}

// aggregateError is matter.js MatterAggregateError: a message over the
// errors it holds, each found by errors.Is and errors.As.
type aggregateError struct {
	message string
	errs    []error
}

func (e *aggregateError) Error() string {
	parts := make([]string, 0, len(e.errs)+1)
	parts = append(parts, "commissioner: "+e.message)
	for _, err := range e.errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

func (e *aggregateError) Unwrap() []error { return e.errs }

// adminSession is a CASE session with the interaction bound to exactly
// it — matter.js AdministratorSession (OperationalCertificateUpdate.ts:802-805
// at b25ed960).
type adminSession struct {
	session *exchange.Session
	ia      *interaction.Interaction
}

// connectCase obtains a CASE session with the peer within timeout (0 the
// peer set's default) and binds an interaction to it; a cancel of ctx
// while or before the session is obtained returns its cause and sends
// nothing. Mirrors matter.js OperationalCertificateUpdate.ts:807-824 at
// b25ed960 (connectCase): the abort is checked again after the connect
// (:814).
func (u *nocUpdate) connectCase(ctx context.Context, timeout time.Duration) (adminSession, error) {
	s, err := u.seam.connect(ctx, u.peer, peer.ConnectOptions{ConnectionTimeout: timeout})
	if ctx.Err() != nil {
		return adminSession{}, context.Cause(ctx)
	}
	if err != nil {
		return adminSession{}, err
	}
	if !s.IsSecure() || s.FabricIndex() == 0 {
		return adminSession{}, fmt.Errorf("commissioner: peer %s did not yield a CASE session", peerName(u.address))
	}
	ia := u.c.sessionInteraction(u.peer.Exchanges(), s)
	if u.seam.opened != nil {
		u.seam.opened(ia)
	}
	return adminSession{session: s, ia: ia}, nil
}

// withCase runs fn over a session of connectCase and closes its
// interaction afterwards — OperationalCertificateUpdate.ts:826-839 at
// b25ed960 (withCase).
func withCase[T any](ctx context.Context, u *nocUpdate, timeout time.Duration, fn func(adminSession) (T, error)) (T, error) {
	s, err := u.connectCase(ctx, timeout)
	if err != nil {
		var zero T
		return zero, err
	}
	defer s.ia.Close(nil)
	return fn(s)
}

// forgetSessions gives up our sessions with a peer that cleared them
// without notice, drops the resumption record of the replaced certificate
// and restarts the peer's subscriptions instead of waiting for them to
// time out; a failed step is logged and the next runs. Mirrors matter.js
// OperationalCertificateUpdate.ts:784-800 at b25ed960 (forgetSessions).
//
// The disconnect waits for the peer's connection process to end after
// its cancel (client/peer stopConnecting) under a context no cancel of
// ctx ends, bounded by forgetDisconnectBound so a process slow to stop
// cannot hold the update; the sessions are given up either way.
func (u *nocUpdate) forgetSessions(ctx context.Context, reason string) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), forgetDisconnectBound)
	u.seam.disconnect(dctx, u.peer, fabricChangedError{reason: reason})
	cancel()
	if err := u.seam.deleteResumptionRecord(u.address); err != nil {
		u.c.log.Info("matter.controller.noc_update.resumption_delete_failed", slog.String("peer", peerName(u.address)), slog.Any("err", err))
	}
	u.seam.closeForPeer(u.address)
}

// installKind is the outcome of the steps over the first session —
// matter.js InstalledCertificate (OperationalCertificateUpdate.ts:356-360
// at b25ed960).
type installKind int

const (
	installRefused installKind = iota
	installUnconfirmed
	installInstalled
)

// installed is the result of installNewCertificate.
type installed struct {
	kind  installKind
	cause error
	// fabricIndex is the CurrentFabricIndex read before arming, 0 when
	// the read failed.
	fabricIndex uint8
	// nocValue, operationalPublicKey and previousNOC are the new NOC, its
	// key and the verified NOC the peer held before (set unless refused).
	nocValue, operationalPublicKey, previousNOC []byte
	// refusal is why the update must not commit the new NOC;
	// CommissioningComplete is then never sent.
	refusal error
}

// invoke sends one command on the root endpoint over ia, under a context
// no cancel ends (W3).
func (u *nocUpdate) invoke(ctx context.Context, ia *interaction.Interaction, cluster, command uint32, fields spec.Encodable, extended bool) (interaction.Result, error) {
	path := im.ConcreteCommandPath{Endpoint: rootEndpoint, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true}
	return u.seam.invoke(context.WithoutCancel(ctx), ia, path, fields, interaction.InvokeOptions{ExtendedFailSafeTimeout: extended})
}

// responseOf decodes a command's response fields; a bare success status
// in its place is unexpected data. Mirrors matter.js
// OperationalCertificateUpdate.ts:913-922 at b25ed960 (responseOf).
func responseOf(res interaction.Result, command string, decode func(spec.Node) error) error {
	if !res.HasFields {
		return fmt.Errorf("%w: %s was answered with a status instead of a response", errUnexpectedResponse, command)
	}
	if err := decode(res.Fields); err != nil {
		return fmt.Errorf("%w: %s: %w", errUnexpectedResponse, command, err)
	}
	return nil
}

// receivedStatus reports whether err is a status the peer sent in place
// of the response — matter.js ReceivedStatusResponseError
// (packages/protocol/src/action/response/ResultStatus.ts:26-35 at
// b25ed960): the client's *interaction.StatusError, which only a status
// decoded from the peer's message produces.
func receivedStatus(err error) bool {
	var se *interaction.StatusError
	return errors.As(err, &se)
}

// installNewCertificate reads Breadcrumb, CurrentFabricIndex and the
// current NOC, then runs ArmFailSafe, CertificateChainRequest, CSRRequest
// and UpdateNOC over s. The new NOC keeps the CASE Authenticated Tags of
// the current one. It does not arm when Breadcrumb is not 0 or the
// current NOC cannot be read or does not verify; on a failure after
// arming and before UpdateNOC reached the peer it disarms over s. Mirrors
// matter.js OperationalCertificateUpdate.ts:382-532 at b25ed960
// (installNewCertificate).
func (u *nocUpdate) installNewCertificate(ctx context.Context, s adminSession) installed {
	addr := peerName(u.address)
	// :388-395: the start read; nothing armed.
	state, err := u.certificateState(ctx, s.ia)
	if err == nil {
		err = state.startState(addr)
	}
	if err != nil {
		return installed{kind: installRefused, cause: err}
	}
	fabricIndex := state.fabricIndex
	// :397-408: the reported NOC verified first.
	previousNOC, previousCATs, err := u.verifiedCurrentNOC(state.nocs, s.session.CaseAuthenticatedTags())
	if err != nil {
		return installed{kind: installRefused, fabricIndex: fabricIndex, cause: &CertificateUpdateError{
			kind: ErrCertificateUpdateRefused, FabricIndexOnPeer: fabricIndex, Cause: err,
			Message: fmt.Sprintf("Cannot update the operational certificate of %s: the certificate it reports did not verify", addr),
		}}
	}
	// :409-419: Breadcrumb not 0 — another operation may hold the fail-safe.
	if *state.breadcrumb != 0 {
		return installed{kind: installRefused, fabricIndex: fabricIndex, cause: &CertificateUpdateError{
			kind: ErrCertificateUpdateBusy, FabricIndexOnPeer: fabricIndex,
			Message: fmt.Sprintf("Cannot update the operational certificate of %s: its Breadcrumb is %d, so another operation of the same fabric may hold the fail-safe; retry later", addr, *state.breadcrumb),
		}}
	}

	// :421-428: ArmFailSafe; an error code armed nothing, any other
	// failure owes a disarm.
	if err := u.armFailsafe(ctx, s.ia, fabricIndex); err != nil {
		if !armRefusal(err) {
			u.disarmFailsafe(ctx, s.ia)
		}
		return installed{kind: installRefused, fabricIndex: fabricIndex, cause: err}
	}

	// :430-480: the DAC, the CSR, its check against the DAC and the session's
	// challenge, the NOC; a failure disarms.
	nocValue, operationalKey, err := u.issueCertificate(ctx, s, previousCATs)
	if err != nil {
		u.disarmFailsafe(ctx, s.ia)
		return installed{kind: installRefused, fabricIndex: fabricIndex, cause: err}
	}
	cert := installed{fabricIndex: fabricIndex, nocValue: nocValue, operationalPublicKey: operationalKey, previousNOC: previousNOC}

	// :482-504: UpdateNOC; a status the peer sent refused it (disarm), any
	// other failure leaves it unconfirmed — the peer may have applied it
	// and then dropped the session or its NOCResponse.
	var icac *[]byte
	if v := u.fabric.CA.ICACCert(); v != nil {
		icac = &v
	}
	res, err := u.invoke(ctx, s.ia, opCredsCluster, opcredsdef.CmdUpdateNoc, opcredsdef.UpdateNocRequest{NocValue: nocValue, IcacValue: icac}, true)
	var nr nocResponse
	if err == nil {
		err = responseOf(res, "UpdateNOC", func(n spec.Node) (err error) {
			nr, err = decodeNOCResponse(n)
			return err
		})
	}
	if err != nil {
		if receivedStatus(err) {
			u.disarmFailsafe(ctx, s.ia)
			return installed{kind: installRefused, fabricIndex: fabricIndex, cause: err}
		}
		cert.kind, cert.cause = installUnconfirmed, err
		return cert
	}

	// :506-518: a status code other than OK refused it.
	if nr.status != nocStatusOk {
		u.disarmFailsafe(ctx, s.ia)
		msg := fmt.Sprintf("UpdateNOC failed with %s (%d)", nocStatusName(nr.status), nr.status)
		if nr.debugText != nil && *nr.debugText != "" {
			msg += ", " + deviceText(*nr.debugText)
		}
		return installed{kind: installRefused, fabricIndex: fabricIndex, cause: &CertificateUpdateError{FabricIndexOnPeer: fabricIndex, Message: msg}}
	}

	// :520-529, § 11.18.6.9: an OK NOCResponse carries the index of the
	// updated fabric, which UpdateNOC never changes.
	cert.kind = installInstalled
	if nr.fabricIndex == nil || *nr.fabricIndex != fabricIndex {
		answered := "none"
		if nr.fabricIndex != nil {
			answered = strconv.FormatUint(uint64(*nr.fabricIndex), 10)
		}
		cert.refusal = &CertificateUpdateError{
			FabricIndexOnPeer: fabricIndex,
			Message:           fmt.Sprintf("UpdateNOC answered OK for fabric index %s instead of %d, the CurrentFabricIndex read before", answered, fabricIndex),
		}
	}
	return cert
}

// issueCertificate is the DAC, the CSR and the NOC of
// OperationalCertificateUpdate.ts:430-475 at b25ed960: the DAC's key
// (:432-445), a CSRRequest with a 32-byte nonce and IsForUpdateNOC
// (:447-460), the CSR checked against the session's attestation
// challenge, the nonce and the DAC key (:461-464, NocsrValidator.validate),
// and a NOC for the CSR's key with the peer's node id, the fabric id and
// the previous NOC's CASE Authenticated Tags (:465-475).
func (u *nocUpdate) issueCertificate(ctx context.Context, s adminSession, cats []uint32) (noc, operationalKey []byte, err error) {
	res, err := u.invoke(ctx, s.ia, opCredsCluster, opcredsdef.CmdCertificateChainRequest,
		opcredsdef.CertificateChainRequestRequest{CertificateType: opcredsdef.CertificateChainTypeDacCertificate}, true)
	if err != nil {
		return nil, nil, err
	}
	var dac []byte
	if err := responseOf(res, "CertificateChainRequest", func(n spec.Node) (err error) {
		dac, err = decodeCertificateChainResponse(n)
		return err
	}); err != nil {
		return nil, nil, err
	}
	dacKey, err := dacKeyOf(dac)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, csrNonceSize)
	if _, err := io.ReadFull(u.c.ctx.Rand, nonce); err != nil {
		return nil, nil, err
	}
	forUpdate := true
	res, err = u.invoke(ctx, s.ia, opCredsCluster, opcredsdef.CmdCsrRequest, opcredsdef.CsrRequestRequest{CsrNonce: nonce, IsForUpdateNoc: &forUpdate}, true)
	if err != nil {
		return nil, nil, err
	}
	var csr attestation.NOCSRResponse
	if err := responseOf(res, "CSRRequest", func(n spec.Node) (err error) {
		csr, err = decodeCSRResponse(n)
		return err
	}); err != nil {
		return nil, nil, err
	}
	operationalKey, err = attestation.ValidateNOCSR(
		attestation.NOCSRContext{Challenge: s.session.AttestationChallenge(), CSRNonce: nonce, DACPublicKey: dacKey},
		csr,
	)
	if err != nil {
		return nil, nil, err
	}
	noc, err = u.fabric.CA.GenerateNOC(operationalKey, u.fabric.FabricID, u.address.NodeID, cats)
	if err != nil {
		return nil, nil, err
	}
	return noc, operationalKey, nil
}

// armFailsafe arms the update's fail-safe with its Breadcrumb; an error
// code is a Busy (BusyWithOtherAdmin) or Refused error carrying
// fabricIndex. Mirrors matter.js OperationalCertificateUpdate.ts:849-877
// at b25ed960 (armFailsafe): the expiry in whole seconds, rounded up, and
// no extended response timeout.
func (u *nocUpdate) armFailsafe(ctx context.Context, ia *interaction.Interaction, fabricIndex uint8) error {
	expiry := uint16((u.failsafe + time.Second - 1) / time.Second) //nolint:gosec // bounded by MaxCertificateUpdateFailsafe
	res, err := u.invoke(ctx, ia, gencomm.ClusterID, gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: expiry, Breadcrumb: u.breadcrumb}, false)
	if err != nil {
		return err
	}
	var resp gencomm.ArmFailSafeResponse
	if err := responseOf(res, "ArmFailSafe", resp.DecodeTLV); err != nil {
		return err
	}
	addr := peerName(u.address)
	if resp.ErrorCode == gencomm.CommissioningErrorOk {
		return nil
	}
	if resp.ErrorCode == gencomm.CommissioningErrorBusyWithOtherAdmin {
		return &CertificateUpdateError{
			kind: ErrCertificateUpdateBusy, FabricIndexOnPeer: fabricIndex,
			Message: fmt.Sprintf("Cannot update the operational certificate of %s: ArmFailSafe failed because another administrator holds the fail-safe or a commissioning window is open; retry later", addr),
		}
	}
	return &CertificateUpdateError{
		kind: ErrCertificateUpdateRefused, FabricIndexOnPeer: fabricIndex,
		Message: fmt.Sprintf("Cannot update the operational certificate of %s: ArmFailSafe failed with %s", addr, commissioningErrorText(resp.ErrorCode, resp.DebugText)),
	}
}

// disarmFailsafe expires the fail-safe at once, so the peer rolls back:
// ArmFailSafe(0, Breadcrumb 0), no extended response timeout. It reports
// whether the peer confirmed the disarm; a failure is logged, as it only
// delays the rollback until the fail-safe expires. Mirrors matter.js
// OperationalCertificateUpdate.ts:879-907 at b25ed960 (disarmFailsafe).
func (u *nocUpdate) disarmFailsafe(ctx context.Context, ia *interaction.Interaction) bool {
	res, err := u.invoke(ctx, ia, gencomm.ClusterID, gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{}, false)
	var resp gencomm.ArmFailSafeResponse
	if err == nil {
		err = responseOf(res, "ArmFailSafe", resp.DecodeTLV)
	}
	if err == nil {
		if resp.ErrorCode == gencomm.CommissioningErrorOk {
			return true
		}
		err = errors.New(commissioningErrorText(resp.ErrorCode, resp.DebugText))
	}
	u.c.log.Info("matter.controller.noc_update.disarm_failed", slog.String("peer", peerName(u.address)), slog.Any("err", err))
	return false
}

// commissioningErrorText is "<name> (<code>)[, <debug text>]" —
// OperationalCertificateUpdate.ts:909-911 at b25ed960.
func commissioningErrorText(code gencomm.CommissioningErrorEnum, debugText string) string {
	text := fmt.Sprintf("%s (%d)", enumName(gencomm.CommissioningErrorEnumDef, uint64(code)), code)
	if debugText != "" {
		text += ", " + deviceText(debugText)
	}
	return text
}

// completionKind is matter.js Completion's kind
// (OperationalCertificateUpdate.ts:547 at b25ed960).
type completionKind int

const (
	completionCompleted completionKind = iota
	completionReverted
	completionUnknown
)

// completion is the result of completeUpdate.
type completion struct {
	kind  completionKind
	cause error
}

// disarmed is a step that disarmed after UpdateNOC, so only another read
// tells the outcome (:549-554).
type disarmed struct {
	confirmed bool
	cause     error
}

// completeUpdate sends CommissioningComplete over a new CASE session, but
// only after reading on that session this update's Breadcrumb and the new
// NOC; otherwise, and when CommissioningComplete fails, it disarms and
// decides from a read on another new session. It fails when the peer
// cannot be reached, a read fails or CommissioningComplete is not
// answered; when the read after a refused CommissioningComplete fails it
// fails with an aggregate of the refusal and the read's error. timeout is
// the connection timeout, 0 the default. Mirrors matter.js
// OperationalCertificateUpdate.ts:556-628 at b25ed960 (completeUpdate).
func (u *nocUpdate) completeUpdate(ctx context.Context, cert installed, timeout time.Duration) (*completion, error) {
	u.forgetSessions(ctx, "Operational certificate of the peer was updated")
	type step struct {
		done     *completion
		disarmed *disarmed
	}
	st, err := withCase(ctx, u, timeout, func(s adminSession) (step, error) {
		state, err := u.certificateState(ctx, s.ia)
		if err != nil {
			return step{}, err
		}
		held := state.heldNOC(cert)
		if state.breadcrumb != nil && *state.breadcrumb == u.breadcrumb && held == heldNew {
			// :576-578: the NOCResponse's refusal never commits.
			if cert.refusal != nil {
				return step{disarmed: &disarmed{cause: cert.refusal, confirmed: u.disarmFailsafe(ctx, s.ia)}}, nil //nolint:nilerr // the refusal is the step's cause, not its failure
			}
			// :579-596.
			resp, refusal, err := u.commissioningComplete(ctx, s.ia)
			if err != nil {
				return step{}, err
			}
			if refusal == nil && resp.ErrorCode == gencomm.CommissioningErrorOk {
				return step{done: &completion{kind: completionCompleted}}, nil
			}
			if refusal == nil {
				refusal = &CertificateUpdateError{
					FabricIndexOnPeer: cert.fabricIndex,
					Message:           "CommissioningComplete failed with " + commissioningErrorText(resp.ErrorCode, resp.DebugText),
				}
			}
			return step{disarmed: &disarmed{cause: refusal, confirmed: u.disarmFailsafe(ctx, s.ia)}}, nil
		}
		// :598-600: the fail-safe ended and the previous NOC is back.
		if state.breadcrumb != nil && *state.breadcrumb == 0 && held == heldPrevious {
			return step{done: &completion{kind: completionReverted}}, nil
		}
		// :601.
		return step{disarmed: &disarmed{confirmed: u.disarmFailsafe(ctx, s.ia)}}, nil
	})
	if err != nil {
		return nil, err
	}
	if st.done != nil {
		return st.done, nil
	}

	// :606-627: decided from a read on another new session. As matter.js,
	// this pass only reads: an unconfirmed disarm is not sent again here.
	u.forgetSessions(ctx, "Peer may have reverted to its previous operational certificate")
	state, err := withCase(ctx, u, timeout, func(s adminSession) (certificateState, error) {
		return u.certificateState(ctx, s.ia)
	})
	if err != nil {
		if st.disarmed.cause == nil {
			return nil, err
		}
		return nil, &aggregateError{
			message: fmt.Sprintf("CommissioningComplete failed and the certificate state of %s could not be read again", peerName(u.address)),
			errs:    []error{st.disarmed.cause, err},
		}
	}
	held := state.heldNOC(cert)
	zero := state.breadcrumb != nil && *state.breadcrumb == 0
	switch {
	case zero && held == heldPrevious:
		return &completion{kind: completionReverted, cause: st.disarmed.cause}, nil
	case zero && held == heldNew && st.disarmed.confirmed:
		return &completion{kind: completionCompleted}, nil
	}
	return &completion{kind: completionUnknown, cause: st.disarmed.cause}, nil
}

// commissioningComplete sends CommissioningComplete with the extended
// response timeout; a status the peer sent in place of the response is
// returned as refusal, not as error. It fails when no answer arrives, the
// answer lacks the command's response or is a bare success status.
// Mirrors matter.js OperationalCertificateUpdate.ts:630-651 at b25ed960
// (commissioningComplete).
func (u *nocUpdate) commissioningComplete(ctx context.Context, ia *interaction.Interaction) (resp gencomm.CommissioningCompleteResponse, refusal, err error) {
	res, err := u.invoke(ctx, ia, gencomm.ClusterID, gencomm.CmdCommissioningComplete, gencomm.CommissioningCompleteRequest{}, true)
	if err == nil {
		err = responseOf(res, "CommissioningComplete", resp.DecodeTLV)
	}
	if err != nil && receivedStatus(err) {
		return resp, err, nil
	}
	return resp, nil, err
}

// certificateState is what the update reads of the accessing fabric:
// Breadcrumb, the NOCs and CurrentFabricIndex, each nil when the peer
// reported no value — matter.js CertificateState
// (OperationalCertificateUpdate.ts:659-668 at b25ed960).
type certificateState struct {
	breadcrumb  *uint64
	nocs        []operationalCertificate
	hasNOCs     bool
	fabricIndex uint8
	// hasFabricIndex is CurrentFabricIndex reported as a number.
	hasFabricIndex bool
}

// operationalCertificate is one entry of the NOCs attribute.
type operationalCertificate struct {
	noc, icac []byte
}

// certificateState reads Breadcrumb, the NOCs and CurrentFabricIndex of
// the accessing fabric in one fabric-filtered request, under a context no
// cancel ends. Values of another type count as not reported; a NOCs
// entry without a NOC octet string is skipped. Mirrors matter.js
// OperationalCertificateUpdate.ts:736-775 at b25ed960 (certificateState).
func (u *nocUpdate) certificateState(ctx context.Context, ia *interaction.Interaction) (certificateState, error) {
	values, ok, err := u.seam.read(context.WithoutCancel(ctx), ia, []im.ConcreteAttributePath{
		rootAttr(gencomm.ClusterID, gencomm.AttrBreadcrumb),
		rootAttr(opCredsCluster, opcredsdef.AttrNocs),
		rootAttr(opCredsCluster, opcredsdef.AttrCurrentFabricIndex),
	})
	if err != nil {
		return certificateState{}, err
	}
	var st certificateState
	if ok[0] {
		if v, err := spec.DecodeUint[uint64](64)(values[0]); err == nil {
			st.breadcrumb = &v
		}
	}
	if ok[1] && (values[1].El.Type == tlv.TypeArray || values[1].El.Type == tlv.TypeList) {
		st.hasNOCs = true
		st.nocs = []operationalCertificate{}
		for _, entry := range values[1].Children {
			if e, ok := nocEntry(entry); ok {
				st.nocs = append(st.nocs, e)
			}
		}
	}
	if ok[2] {
		if v, err := spec.DecodeUint[uint64](64)(values[2]); err == nil {
			st.hasFabricIndex = true
			// An index out of a fabric index's range is reported as
			// invalid by startState.
			st.fabricIndex = uint8(min(v, 0xFF)) //nolint:gosec // bounded
		}
	}
	return st, nil
}

// nocEntry is a NOCs entry whose field 1 is an octet string, with its ICAC
// when field 2 is one.
func nocEntry(n spec.Node) (operationalCertificate, bool) {
	if n.El.Type != tlv.TypeStructure {
		return operationalCertificate{}, false
	}
	var e operationalCertificate
	var found bool
	for _, f := range n.Children {
		b, err := spec.DecodeBytes(f)
		if f.El.Tag.Kind != tlv.TagKindContext || err != nil {
			continue
		}
		switch f.El.Tag.Number {
		case 1:
			e.noc, found = b, true
		case 2:
			e.icac = b
		}
	}
	return e, found
}

// startState checks the start read: Breadcrumb and the NOCs reported and
// CurrentFabricIndex a valid fabric index. Mirrors matter.js
// OperationalCertificateUpdate.ts:688-701 at b25ed960 (readStartState).
func (st certificateState) startState(addr string) error {
	switch {
	case st.breadcrumb == nil:
		return fmt.Errorf("%w: %s reported no Breadcrumb value", errUnexpectedResponse, addr)
	case !st.hasNOCs:
		return fmt.Errorf("%w: %s reported no NOCs value", errUnexpectedResponse, addr)
	case !st.hasFabricIndex:
		return fmt.Errorf("%w: %s reported CurrentFabricIndex none", errUnexpectedResponse, addr)
	case st.fabricIndex < 1 || st.fabricIndex > 254:
		return fmt.Errorf("%w: %s reported CurrentFabricIndex %d", errUnexpectedResponse, addr, st.fabricIndex)
	}
	return nil
}

// held is which certificate a state shows as the fabric's only NOC.
type held int

const (
	heldOther held = iota
	heldNew
	heldPrevious
)

// heldNOC is which of the two certificates st shows as the only NOC of
// the fabric; heldOther for anything else — no NOCs value, an empty list,
// a NOC of another rotation. Mirrors matter.js
// OperationalCertificateUpdate.ts:670-686 at b25ed960 (heldNoc).
func (st certificateState) heldNOC(cert installed) held {
	if len(st.nocs) != 1 {
		return heldOther
	}
	switch noc := st.nocs[0].noc; {
	case bytes.Equal(noc, cert.nocValue):
		return heldNew
	case bytes.Equal(noc, cert.previousNOC):
		return heldPrevious
	}
	return heldOther
}

// verifiedCurrentNOC is the one NOC the peer reports for the accessing
// fabric and its CASE Authenticated Tags. The NOCs value is the peer's
// data, so the NOC must chain to the fabric's root and name the peer's
// node id and the fabric id before our CA copies its tags — else a peer
// could obtain tags it never held — and its tags must equal sessionCATs,
// those of the NOC the peer proved to hold in CASE, because an older NOC
// our CA issued to the same node verifies too and may carry tags a later
// rotation removed. Mirrors matter.js OperationalCertificateUpdate.ts:703-734
// at b25ed960 (verifiedCurrentNoc), with Fabric.verifyCredentials
// (packages/protocol/src/fabric/Fabric.ts:359-369) as the fabric's
// verifier.
func (u *nocUpdate) verifiedCurrentNOC(nocs []operationalCertificate, sessionCATs []uint32) (noc []byte, cats []uint32, err error) {
	if len(nocs) != 1 {
		return nil, nil, fmt.Errorf("%w: peer reported %d NOCs for its fabric instead of one", errUnexpectedResponse, len(nocs))
	}
	noc, icac := nocs[0].noc, nocs[0].icac
	v := u.fabric.Verifier
	if v == nil {
		return nil, nil, errors.New("commissioner: the fabric has no certificate verifier")
	}
	if _, err := v.VerifyAndExtractPubKey(noc, icac); err != nil {
		return nil, nil, err
	}
	nodeID, err := v.PeerNodeIDFromNOC(noc)
	if err != nil {
		return nil, nil, err
	}
	fabricID, err := v.PeerFabricIDFromNOC(noc)
	if err != nil {
		return nil, nil, err
	}
	if nodeID != u.address.NodeID || fabricID != u.fabric.FabricID {
		return nil, nil, fmt.Errorf("%w: reported NOC is for node %016X of fabric %d, not node %016X of fabric %d",
			errUnexpectedResponse, nodeID, fabricID, u.address.NodeID, u.fabric.FabricID)
	}
	cats, err = v.PeerCATsFromNOC(noc)
	if err != nil {
		return nil, nil, err
	}
	if len(cats) != len(sessionCATs) || slices.ContainsFunc(cats, func(t uint32) bool { return !slices.Contains(sessionCATs, t) }) {
		return nil, nil, fmt.Errorf("%w: reported NOC carries CASE Authenticated Tags %s, not %s of the NOC the node authenticated with",
			errUnexpectedResponse, catList(cats), catList(sessionCATs))
	}
	return noc, cats, nil
}

// catList is a list of CASE Authenticated Tags for a message, "none" for
// none.
func catList(cats []uint32) string {
	if len(cats) == 0 {
		return "none"
	}
	parts := make([]string, len(cats))
	for i, t := range cats {
		parts[i] = fmt.Sprintf("0x%08X", t)
	}
	return strings.Join(parts, ", ")
}
