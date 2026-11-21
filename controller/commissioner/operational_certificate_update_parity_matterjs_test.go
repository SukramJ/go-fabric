// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/OperationalCertificateUpdateTest.ts
// at b25ed960: the controller replacing the NOC of a commissioned peer —
// the success path against the module's own node device (UpdateNOC, a
// fresh CASE session, CommissioningComplete), the refusals before arming,
// the rollbacks, the recovery pass and the unknown outcomes.
//
// matter.js drives a ClientNode of a MockSite pair; the port drives the
// registry's [client.Registry.UpdateOperationalCertificate] of a
// fabrictest pair, the device the module's node. matter.js injects its
// faults by replacing ClientInteraction.prototype.invoke and read,
// Peer.prototype.connect and SessionManager.prototype.deleteResumptionRecord
// (:304-355, :465-498); the port replaces the same calls through the
// update's seam (commissioner.PatchNOCUpdate). Where matter.js patches a
// command handler of the device (patchDeviceCommand, :243) the port
// answers or rewrites the command at that seam: the controller sees the
// same answer, and a handler that never calls the original changes
// nothing on the device in either. countCommissioningComplete counts the
// CommissioningComplete commands that reach the device. A device's
// setStateOf(GeneralCommissioningServer, {breadcrumb}) is the node's
// Breadcrumb write, expireFailsafe a disarm of the device's own
// GeneralCommissioning server.
//
// The facade's stored fabric index is asserted against the node's
// record; the ports that first store a stale index (STALE_FABRIC_INDEX)
// and the refusal of an uncommissioned node live in controller/client,
// which can set the record. Not ported: the two "after another
// transaction releases the commissioning state" cases (:1064, :1077) —
// matter.js's transaction lock on the behaviour state has no Go
// counterpart, the record write takes no such lock.
package commissioner_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// errUnresponsive is matter.js PeerUnresponsiveError of the injected
// faults: a failure that is not a status the peer sent.
var errUnresponsive = errors.New("test: peer unresponsive")

// ocuCtx bounds one port on the wall clock.
func ocuCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// ocuPair is MockSite.addCommissionedPair with subscribedPeer(controller,
// "peer1"): a controller and the module's node, commissioned through the
// registry, the node's default subscription active.
type ocuPair struct {
	t    *testing.T
	site *fabrictest.Site
	pair *fabrictest.Pair
	node *client.Node
	com  *commissioner.Commissioner
}

type ocuOptions struct {
	controller fabrictest.ControllerOptions
	commission client.CommissionOptions
}

func newOCUPair(t *testing.T, o ocuOptions) *ocuPair {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	pair := site.UncommissionedPair(fabrictest.PairOptions{Controller: o.controller})
	n, err := pair.Controller.CommissionNode(ocuCtx(t), pair.Device, o.commission)
	if err != nil {
		t.Fatal(err)
	}
	p := &ocuPair{t: t, site: site, pair: pair, node: n, com: pair.Controller.Commissioner()}
	p.eventually("the default subscription", n.SubscriptionActive)
	return p
}

func (p *ocuPair) eventually(what string, cond func() bool) {
	p.t.Helper()
	if err := p.site.Eventually(ocuCtx(p.t), what, cond); err != nil {
		p.t.Fatal(err)
	}
}

// advance moves the site's clock by d in steps, so the work each step's
// timers start runs before the next.
func (p *ocuPair) advance(d time.Duration) {
	p.t.Helper()
	unit := max(2*time.Second, d/60)
	for d > 0 {
		step := min(d, unit)
		d -= step
		if err := p.site.Step(ocuCtx(p.t), step); err != nil {
			p.t.Fatal(err)
		}
	}
}

// patch applies a seam patch until the test ends.
func (p *ocuPair) patch(fn func(commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam) {
	p.t.Cleanup(commissioner.PatchNOCUpdate(p.com, fn))
}

// updateWith runs the registry's update of the node under the site.
func (p *ocuPair) updateWith(o client.CertificateUpdateOptions, ctxOf func(context.Context) context.Context) error {
	p.t.Helper()
	var uerr error
	if err := p.site.Run(ocuCtx(p.t), func(ctx context.Context) error {
		if ctxOf != nil {
			ctx = ctxOf(ctx)
		}
		uerr = p.pair.Controller.Registry().UpdateOperationalCertificate(ctx, p.node, o)
		return nil
	}); err != nil {
		p.t.Fatal(err)
	}
	return uerr
}

// update is matter.js update(): the update must succeed.
func (p *ocuPair) update() {
	p.t.Helper()
	if err := p.updateWith(client.CertificateUpdateOptions{}, nil); err != nil {
		p.t.Fatalf("update: %v", err)
	}
}

// failure is matter.js updateFailure: the update must fail with a
// *CertificateUpdateError.
func (p *ocuPair) failure() *commissioner.CertificateUpdateError {
	p.t.Helper()
	return asUpdateError(p.t, p.updateWith(client.CertificateUpdateOptions{}, nil))
}

func asUpdateError(t *testing.T, err error) *commissioner.CertificateUpdateError {
	t.Helper()
	var ue *commissioner.CertificateUpdateError
	if !errors.As(err, &ue) {
		t.Fatalf("got %v, want a *CertificateUpdateError", err)
	}
	return ue
}

// rejected is matter.js rejectedUpdate: a failure after the start read,
// the fail-safe disarmed, the index reported and stored.
func (p *ocuPair) rejected(message string) *commissioner.CertificateUpdateError {
	p.t.Helper()
	f := p.failure()
	if message != "" && !strings.Contains(f.Error(), message) {
		p.t.Fatalf("failure %q, want %q", f.Error(), message)
	}
	if p.armed() {
		p.t.Fatal("the fail-safe is still armed")
	}
	p.expectIndexReported(f)
	return f
}

// expectIndexReported is expectFabricIndexReported.
func (p *ocuPair) expectIndexReported(f *commissioner.CertificateUpdateError) {
	p.t.Helper()
	idx := p.fabricIndex()
	if f.FabricIndexOnPeer != idx {
		p.t.Fatalf("FabricIndexOnPeer %d, want %d", f.FabricIndexOnPeer, idx)
	}
	if got := p.node.Record().FabricIndexOnPeer; got != idx {
		p.t.Fatalf("stored FabricIndexOnPeer %d, want %d", got, idx)
	}
}

// expectIndexUnread is expectFabricIndexUnread.
func (p *ocuPair) expectIndexUnread(f *commissioner.CertificateUpdateError) {
	p.t.Helper()
	if f.FabricIndexOnPeer != 0 {
		p.t.Fatalf("FabricIndexOnPeer %d, want none", f.FabricIndexOnPeer)
	}
}

// expectCauseIndexReported is expectCauseFabricIndexReported.
func (p *ocuPair) expectCauseIndexReported(f *commissioner.CertificateUpdateError) {
	p.t.Helper()
	var cause *commissioner.CertificateUpdateError
	if !errors.As(f.Cause, &cause) || cause.FabricIndexOnPeer != p.fabricIndex() {
		p.t.Fatalf("cause %v, want an update error with index %d", f.Cause, p.fabricIndex())
	}
}

func (p *ocuPair) dev() *node.Node { return p.pair.Device.Node() }

// peer is the controller's peer of the device.
func (p *ocuPair) peer() *peer.Peer {
	p.t.Helper()
	pr := fabrictestseam.Peers(p.pair.Controller).Get(*p.node.Record().PeerAddress)
	if pr == nil {
		p.t.Fatal("no peer")
	}
	return pr
}

// fabricIndex is the device's index of the fabric (deviceFabric).
func (p *ocuPair) fabricIndex() uint8 {
	p.t.Helper()
	fs, err := p.dev().FabricInfos(ocuCtx(p.t))
	if err != nil || len(fs) != 1 {
		p.t.Fatalf("device fabrics %+v (%v)", fs, err)
	}
	return fs[0].FabricIndex
}

// identity is the device's NOC and ICAC of the fabric.
func (p *ocuPair) identity() (noc, icac []byte) {
	p.t.Helper()
	rec, err := p.dev().Store().GetIdentity(ocuCtx(p.t), p.fabricIndex())
	if err != nil {
		p.t.Fatal(err)
	}
	return rec.NOC, rec.ICAC
}

func (p *ocuPair) noc() []byte {
	p.t.Helper()
	noc, _ := p.identity()
	return noc
}

func (p *ocuPair) expectNOC(before []byte, same bool) {
	p.t.Helper()
	if bytes.Equal(p.noc(), before) != same {
		p.t.Fatalf("device NOC unchanged = %v, want %v", !same, same)
	}
}

func (p *ocuPair) gc() *mattercore.GeneralCommissioning { return p.dev().Root().GeneralCommissioning }

func (p *ocuPair) armed() bool { return p.gc().FailSafeArmed() }

func (p *ocuPair) breadcrumb() uint64 {
	v, _ := p.gc().MatterRead(gencomm.AttrBreadcrumb)
	b, _ := v.(uint64)
	return b
}

func (p *ocuPair) setBreadcrumb(v uint64) {
	p.t.Helper()
	if err := p.gc().MatterWrite(context.Background(), gencomm.AttrBreadcrumb, v); err != nil {
		p.t.Fatal(err)
	}
}

// expireFailsafe ends the device's fail-safe as an expiry does: the
// device rolls back the pending NOC.
func (p *ocuPair) expireFailsafe(ctx context.Context) {
	ctx = im.WithFabricFilter(ctx, true, p.fabricIndex())
	if _, err := p.gc().MatterInvoke(ctx, gencomm.CmdArmFailSafe, mattercore.ArmFailSafeRequest{}); err != nil {
		p.t.Error(err)
	}
}

// nodeLabelReaches is nodeLabelReachesController: a NodeLabel changed on
// the device reaches the controller's node over its subscription.
func (p *ocuPair) nodeLabelReaches(label string) {
	p.t.Helper()
	if err := p.pair.Device.Act(func(n *node.Node) error { return n.SetNodeLabel(context.Background(), label) }); err != nil {
		p.t.Fatal(err)
	}
	p.eventually("the NodeLabel "+label, func() bool {
		v, err := p.node.Endpoint(0).Cluster(basicinfo.ClusterID).Decoded(basicinfo.AttrNodeLabel)
		s, _ := v.(string)
		return err == nil && s == label
	})
}

// fault is CommandFault (:304-313).
type fault struct {
	delivered, answered bool
	afterDelivery       func(ctx context.Context)
}

// intercept is interceptCommand (:315-355): the number of attempts to
// send the command.
func (p *ocuPair) intercept(cluster, command uint32, f fault) *atomic.Int32 {
	sent := new(atomic.Int32)
	p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
		next := s.Invoke
		s.Invoke = func(ctx context.Context, ia *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
			if path.Cluster != cluster || path.Command != command {
				return next(ctx, ia, path, fields, opts)
			}
			sent.Add(1)
			if !f.delivered {
				return interaction.Result{}, errUnresponsive
			}
			res, err := next(ctx, ia, path, fields, opts)
			if f.afterDelivery != nil {
				f.afterDelivery(ctx)
			}
			if f.answered {
				return res, err
			}
			return interaction.Result{}, errUnresponsive
		}
		return s
	})
	return sent
}

// answer replaces the device's handling of a command: fn returns the
// answer the controller sees; next delivers the command to the device.
func (p *ocuPair) answer(cluster, command uint32, fn func(fields spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error)) {
	p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
		inner := s.Invoke
		s.Invoke = func(ctx context.Context, ia *interaction.Interaction, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
			if path.Cluster != cluster || path.Command != command {
				return inner(ctx, ia, path, fields, opts)
			}
			return fn(fields, func(f spec.Encodable) (interaction.Result, error) { return inner(ctx, ia, path, f, opts) })
		}
		return s
	})
}

// countCommissioningComplete counts the CommissioningComplete commands
// that reach the device (:395-410).
func (p *ocuPair) countCommissioningComplete() *atomic.Int32 {
	handled := new(atomic.Int32)
	p.answer(gencomm.ClusterID, gencomm.CmdCommissioningComplete, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
		handled.Add(1)
		return next(f)
	})
	return handled
}

// refuseDisarm answers ArmFailSafe with expiry 0 with ValueOutsideRange
// (:270-290).
func (p *ocuPair) refuseDisarm() {
	p.answer(gencomm.ClusterID, gencomm.CmdArmFailSafe, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
		if req, ok := f.(gencomm.ArmFailSafeRequest); ok && req.ExpiryLengthSeconds == 0 {
			return response(gencomm.ArmFailSafeResponse{ErrorCode: gencomm.CommissioningErrorValueOutsideRange, DebugText: "test refusal"}), nil
		}
		return next(f)
	})
}

// missingResponse is reportMissingResponse (:369-400): the first matching
// command's answer lacks its entry; delivered says whether the device
// executed it.
func (p *ocuPair) missingResponse(cluster, command uint32, delivered bool) {
	var faulted atomic.Bool
	p.answer(cluster, command, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
		if faulted.Swap(true) {
			return next(f)
		}
		if delivered {
			_, _ = next(f)
		}
		return interaction.Result{}, interaction.ErrNoResponse
	})
}

// response is a command's answer with fields v.
func response(v spec.Encodable) interaction.Result {
	return interaction.Result{HasFields: true, Fields: nodeOf(v), Status: im.StatusSuccess}
}

// nodeOf is v encoded and decoded as one TLV element.
func nodeOf(v spec.Encodable) spec.Node {
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		panic(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		panic(err)
	}
	return n
}

// nocsPath and the read's other paths.
const (
	readBreadcrumb = iota
	readNOCs
	readFabricIndex
)

// dropAttribute is dropAttributeReports (:451-466): the reads report no
// value of the path at index while active holds.
func (p *ocuPair) dropAttribute(index int, active func() bool) {
	p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
		next := s.Read
		s.Read = func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error) {
			values, ok, err := next(ctx, ia, paths)
			if err == nil && (active == nil || active()) {
				ok[index] = false
			}
			return values, ok, err
		}
		return s
	})
}

// reportNOCs is reportNocs (:483-498): a read that starts while active
// holds reports the NOCs forge builds from the device's.
func (p *ocuPair) reportNOCs(forge func([]opcreds.NOCStruct) []opcreds.NOCStruct, active func() bool) {
	p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
		next := s.Read
		s.Read = func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error) {
			forging := active()
			values, ok, err := next(ctx, ia, paths)
			if err != nil || !forging || !ok[readNOCs] {
				return values, ok, err
			}
			entries, derr := spec.DecodeList(spec.DecodeStruct[opcreds.NOCStruct])(values[readNOCs])
			if derr != nil {
				p.t.Error(derr)
			}
			values[readNOCs] = nodeOf(nocList(forge(entries)))
			return values, ok, nil
		}
		return s
	})
}

// reportNOCsOnce is reportNocsOnce (:501-508).
func (p *ocuPair) reportNOCsOnce(forge func([]opcreds.NOCStruct) []opcreds.NOCStruct) {
	var first atomic.Bool
	first.Store(true)
	p.reportNOCs(forge, func() bool { return first.Swap(false) })
}

// forged is forgedNocs (:511-514).
func forged(noc []byte) func([]opcreds.NOCStruct) []opcreds.NOCStruct {
	return func(entries []opcreds.NOCStruct) []opcreds.NOCStruct {
		out := make([]opcreds.NOCStruct, len(entries))
		for i, e := range entries {
			out[i] = opcreds.NOCStruct{Noc: noc, Icac: spec.NullOf[[]byte](), FabricIndex: e.FabricIndex}
		}
		return out
	}
}

type nocList []opcreds.NOCStruct

func (l nocList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, e := range l {
		e.EncodeTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

// devicePublicKey is the public key of the device's NOC.
func (p *ocuPair) devicePublicKey() []byte {
	p.t.Helper()
	noc, icac := p.identity()
	pub, err := p.pair.Controller.Fabric().Verifier.VerifyAndExtractPubKey(noc, icac)
	if err != nil {
		p.t.Fatal(err)
	}
	return keyBytes(p.t, pub)
}

func keyBytes(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	k, err := pub.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	return k.Bytes()
}

// expectForgedRefused is expectForgedNocsRefused (:520-552): refused
// before any fail-safe was armed — so before the CA issued a NOC, which
// follows the arm.
func (p *ocuPair) expectForgedRefused(forge func([]opcreds.NOCStruct) []opcreds.NOCStruct) {
	p.t.Helper()
	arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true})
	p.reportNOCsOnce(forge)
	f := p.failure()
	if !errors.Is(f, commissioner.ErrCertificateUpdateRefused) || !strings.Contains(f.Error(), "the certificate it reports did not verify") {
		p.t.Fatalf("failure %v, want refused: the certificate it reports did not verify", f)
	}
	if f.Outcome != commissioner.CertificateUpdatePreviousKept {
		p.t.Fatalf("outcome %v", f.Outcome)
	}
	p.expectIndexReported(f)
	if arms.Load() != 0 || p.armed() {
		p.t.Fatalf("%d arms, armed %v", arms.Load(), p.armed())
	}
}

// expectOutcome fails unless f states the outcome and the message part.
func expectOutcome(t *testing.T, f *commissioner.CertificateUpdateError, o commissioner.CertificateUpdateOutcome, message string) {
	t.Helper()
	if f.Outcome != o || !strings.Contains(f.Error(), message) {
		t.Fatalf("failure %v (outcome %v), want %v: %q", f, f.Outcome, o, message)
	}
}

func nocsrFailureOf(err error) attestation.NOCSRCheck {
	var ne *attestation.NOCSRError
	if errors.As(err, &ne) {
		return ne.Check
	}
	return ""
}

const (
	keepsPrevious = "keeps its previous certificate"
	reverted      = "reverted to its previous certificate"
	unknown       = "outcome is unknown"
)

// TestOperationalCertificateUpdate is the port. It runs alone among the
// package's tests (not parallel): a fake site clock moves only while the
// process is quiet.
func TestOperationalCertificateUpdate(t *testing.T) {
	// The end-to-end path against the module's own device: UpdateNOC, a
	// fresh CASE session that authenticates the new NOC,
	// CommissioningComplete.
	t.Run("installs a NOC for a new key pair and keeps identity, fabric and ACLs", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		idx := p.fabricIndex()
		fabricsBefore, _ := p.dev().FabricInfos(ocuCtx(t))
		aclBefore, err := p.dev().Store().ListACL(ocuCtx(t), idx)
		if err != nil {
			t.Fatal(err)
		}
		before, keyBefore := p.noc(), p.devicePublicKey()
		storedIndex := p.node.Record().FabricIndexOnPeer

		p.update()

		if p.armed() {
			t.Fatal("the fail-safe is still armed")
		}
		if bytes.Equal(p.noc(), before) || bytes.Equal(p.devicePublicKey(), keyBefore) {
			t.Fatal("the NOC or its key did not change")
		}
		fabricsAfter, _ := p.dev().FabricInfos(ocuCtx(t))
		if len(fabricsAfter) != 1 || fabricsAfter[0].FabricIndex != fabricsBefore[0].FabricIndex ||
			fabricsAfter[0].FabricID != fabricsBefore[0].FabricID || fabricsAfter[0].NodeID != fabricsBefore[0].NodeID {
			t.Fatalf("fabric %+v, was %+v", fabricsAfter, fabricsBefore)
		}
		aclAfter, _ := p.dev().Store().ListACL(ocuCtx(t), idx)
		if len(aclAfter) != len(aclBefore) {
			t.Fatalf("ACL %+v, was %+v", aclAfter, aclBefore)
		}
		if p.node.Record().FabricIndexOnPeer != storedIndex {
			t.Fatal("the stored fabric index changed")
		}
		// CommissioningComplete disarmed the fail-safe, so the update
		// survives its expiry.
		after := p.noc()
		p.advance(120 * time.Second)
		p.expectNOC(after, true)
		p.eventually("the default subscription after the rotation", p.node.SubscriptionActive)
		p.nodeLabelReaches("after rotation")
	})

	t.Run("replaces the session that carried UpdateNOC with a new CASE session", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		pr := p.peer()
		former := pr.NewestSession()
		if former == nil {
			t.Fatal("no session")
		}
		p.update()
		s := pr.NewestSession()
		if s == nil || s == former {
			t.Fatalf("session %p, former %p", s, former)
		}
		if !former.IsClosing() && !former.IsPeerLost() && !former.IsClosed() {
			t.Fatal("the former session is still open")
		}
	})

	t.Run("rolls back when the device refuses UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(opcreds.ClusterID, opcreds.CmdUpdateNoc, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			return response(opcreds.NocResponse{StatusCode: opcreds.NodeOperationalCertStatusInvalidNoc}), nil
		})
		f := p.rejected("")
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectNOC(before, true)
		p.nodeLabelReaches("after refused update")
	})

	t.Run("reports the previous certificate kept when the device answers UpdateNOC with an error status", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(opcreds.ClusterID, opcreds.CmdUpdateNoc, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			return interaction.Result{Status: im.StatusConstraintError}, &interaction.StatusError{Status: im.StatusConstraintError}
		})
		expectOutcome(t, p.rejected(keepsPrevious), commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectNOC(before, true)
	})

	t.Run("rolls back when the CSR does not verify against the DAC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(opcreds.ClusterID, opcreds.CmdCsrRequest, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			res, err := next(f)
			if err != nil {
				return res, err
			}
			var csr opcreds.CsrResponse
			if err := csr.DecodeTLV(res.Fields); err != nil {
				t.Error(err)
			}
			csr.AttestationSignature = make([]byte, 64)
			return response(csr), nil
		})
		f := p.rejected("")
		if c := nocsrFailureOf(f); c != attestation.NOCSRAttestationSignatureInvalid {
			t.Fatalf("NOCSR check %q", c)
		}
		p.expectNOC(before, true)
		p.nodeLabelReaches("after CSR mismatch")
	})

	refuseCommissioningComplete := func(p *ocuPair) {
		p.answer(gencomm.ClusterID, gencomm.CmdCommissioningComplete, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			return response(gencomm.CommissioningCompleteResponse{ErrorCode: gencomm.CommissioningErrorValueOutsideRange, DebugText: "test refusal"}), nil
		})
	}

	t.Run("rolls back when CommissioningComplete fails after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		refuseCommissioningComplete(p)
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		p.expectNOC(before, true)
		p.nodeLabelReaches("after failed completion")
	})

	t.Run("keeps the CommissioningComplete refusal in the cause when the read after the disarm fails", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		refuseCommissioningComplete(p)
		var connects atomic.Int32
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.Connect
			s.Connect = func(ctx context.Context, pr *peer.Peer, o peer.ConnectOptions) (*exchange.Session, error) {
				// The first two sessions carry UpdateNOC and
				// CommissioningComplete; every later one fails.
				if connects.Add(1) > 2 {
					return nil, errUnresponsive
				}
				return next(ctx, pr, o)
			}
			return s
		})
		f := p.failure()
		var agg interface{ Unwrap() []error }
		if !errors.As(f.Cause, &agg) || len(agg.Unwrap()) != 2 {
			t.Fatalf("cause %v, want an aggregate of two", f.Cause)
		}
		if !strings.Contains(f.Error(), "ValueOutsideRange") {
			t.Fatalf("failure %v, want the refusal in its cause", f)
		}
	})

	t.Run("keeps the new NOC when CommissioningComplete took effect but its answer is lost", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		p.intercept(gencomm.ClusterID, gencomm.CmdCommissioningComplete, fault{delivered: true})
		p.update()
		if handled.Load() != 1 || p.armed() {
			t.Fatalf("%d CommissioningComplete handled, armed %v", handled.Load(), p.armed())
		}
		p.expectNOC(before, false)
	})

	t.Run("refuses a fail-safe the ArmFailSafe command cannot carry or the update cannot fit into", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		for _, fs := range []time.Duration{29 * time.Second, 0x10000 * time.Second} {
			err := p.updateWith(client.CertificateUpdateOptions{Failsafe: fs}, nil)
			var ue *commissioner.CertificateUpdateError
			if err == nil || errors.As(err, &ue) || !strings.Contains(err.Error(), "is outside") {
				t.Fatalf("fail-safe %s: %v", fs, err)
			}
		}
	})

	t.Run("refuses while a commissioning window holds the fail-safe", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		if err := p.pair.Device.OpenWindow(ocuCtx(t), 3*time.Minute); err != nil {
			t.Fatal(err)
		}
		f := p.failure()
		if !errors.Is(f, commissioner.ErrCertificateUpdateBusy) || !strings.Contains(f.Error(), "another administrator holds the fail-safe") {
			t.Fatalf("failure %v", f)
		}
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, "")
		p.expectIndexReported(f)
		p.expectNOC(before, true)
	})

	refuseArm := func(p *ocuPair, answer func() (interaction.Result, error)) {
		p.answer(gencomm.ClusterID, gencomm.CmdArmFailSafe, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			if req, ok := f.(gencomm.ArmFailSafeRequest); ok && req.ExpiryLengthSeconds != 0 {
				return answer()
			}
			return next(f)
		})
	}

	t.Run("reports a refused ArmFailSafe as refused, not busy", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		refuseArm(p, func() (interaction.Result, error) {
			return response(gencomm.ArmFailSafeResponse{ErrorCode: gencomm.CommissioningErrorValueOutsideRange, DebugText: "test refusal"}), nil
		})
		f := p.failure()
		if !errors.Is(f, commissioner.ErrCertificateUpdateRefused) || errors.Is(f, commissioner.ErrCertificateUpdateBusy) {
			t.Fatalf("failure %v", f)
		}
		p.expectIndexReported(f)
		if p.armed() {
			t.Fatal("armed")
		}
		p.expectNOC(before, true)
	})

	t.Run("reports the previous certificate kept when the device answers ArmFailSafe with an error status", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		refuseArm(p, func() (interaction.Result, error) {
			return interaction.Result{Status: im.StatusConstraintError}, &interaction.StatusError{Status: im.StatusConstraintError}
		})
		expectOutcome(t, p.rejected(keepsPrevious), commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectNOC(before, true)
	})

	t.Run("arms nothing while Breadcrumb is not 0", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.setBreadcrumb(7)
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{})
		f := p.failure()
		if !errors.Is(f, commissioner.ErrCertificateUpdateBusy) || !strings.Contains(f.Error(), "Breadcrumb is 7, so another operation") {
			t.Fatalf("failure %v", f)
		}
		p.expectIndexReported(f)
		if arms.Load() != 0 || p.armed() || p.breadcrumb() != 7 {
			t.Fatalf("%d arms, armed %v, Breadcrumb %d", arms.Load(), p.armed(), p.breadcrumb())
		}
		p.expectNOC(before, true)
	})

	t.Run("arms nothing when Breadcrumb cannot be read", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{})
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.Read
			s.Read = func(ctx context.Context, ia *interaction.Interaction, paths []im.ConcreteAttributePath) ([]spec.Node, []bool, error) {
				for _, path := range paths {
					if path.Cluster == gencomm.ClusterID {
						return nil, nil, errUnresponsive
					}
				}
				return next(ctx, ia, paths)
			}
			return s
		})
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectIndexUnread(f)
		if arms.Load() != 0 || p.armed() {
			t.Fatalf("%d arms, armed %v", arms.Load(), p.armed())
		}
	})

	t.Run("arms nothing when the node reports no Breadcrumb value", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.dropAttribute(readBreadcrumb, nil)
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectIndexUnread(f)
		if p.armed() {
			t.Fatal("armed")
		}
	})

	t.Run("arms nothing when the node reports no CurrentFabricIndex value", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.dropAttribute(readFabricIndex, nil)
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		if !strings.Contains(f.Cause.Error(), "reported CurrentFabricIndex none") {
			t.Fatalf("cause %v", f.Cause)
		}
		p.expectIndexUnread(f)
		if p.armed() {
			t.Fatal("armed")
		}
	})

	// § 11.18.6.9: UpdateNOC answers OK with the index of the updated
	// fabric, which the update never changes.
	answerUpdateNOCFor := func(p *ocuPair, index uint8, sent func(noc []byte)) {
		p.answer(opcreds.ClusterID, opcreds.CmdUpdateNoc, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			if req, ok := f.(opcreds.UpdateNocRequest); ok && sent != nil {
				sent(req.NocValue)
			}
			res, err := next(f)
			if err != nil {
				return res, err
			}
			var nr opcreds.NocResponse
			if err := nr.DecodeTLV(res.Fields); err != nil {
				t.Error(err)
			}
			nr.FabricIndex = &index
			return response(nr), nil
		})
	}

	t.Run("rolls back without CommissioningComplete when UpdateNOC answers OK for another fabric index", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before, known := p.noc(), p.node.Record().FabricIndexOnPeer
		handled := p.countCommissioningComplete()
		answerUpdateNOCFor(p, 200, nil)
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		if handled.Load() != 0 {
			t.Fatalf("%d CommissioningComplete", handled.Load())
		}
		p.expectNOC(before, true)
		if p.node.Record().FabricIndexOnPeer != known {
			t.Fatal("stored index changed")
		}
	})

	t.Run("stores the CurrentFabricIndex read before an update the device refuses", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.answer(opcreds.ClusterID, opcreds.CmdUpdateNoc, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			return response(opcreds.NocResponse{StatusCode: opcreds.NodeOperationalCertStatusInvalidNoc}), nil
		})
		p.expectCauseIndexReported(p.rejected(keepsPrevious))
	})

	t.Run("stores the CurrentFabricIndex read before a refused CommissioningComplete", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		refuseCommissioningComplete(p)
		p.expectCauseIndexReported(p.rejected(reverted))
	})

	t.Run("stores the CurrentFabricIndex read before an update with an unknown outcome", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		idx := p.fabricIndex()
		p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{delivered: true, afterDelivery: func(ctx context.Context) {
			if err := p.pair.Device.Stop(ctx); err != nil {
				t.Error(err)
			}
		}})
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdateUnknown, unknown)
		if f.FabricIndexOnPeer != idx || p.node.Record().FabricIndexOnPeer != idx {
			t.Fatalf("index %d, stored %d, want %d", f.FabricIndexOnPeer, p.node.Record().FabricIndexOnPeer, idx)
		}
		// Both the lost UpdateNOC answer and the failed recovery.
		var agg interface{ Unwrap() []error }
		if !errors.As(f.Cause, &agg) || len(agg.Unwrap()) != 2 {
			t.Fatalf("cause %v, want an aggregate of two", f.Cause)
		}
	})

	t.Run("reports the CurrentFabricIndex when another operation completed the fail-safe of a refused NOCResponse", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var newNOC atomic.Pointer[[]byte]
		answerUpdateNOCFor(p, 200, func(noc []byte) { newNOC.Store(&noc) })
		// The disarm reverts the device; reporting the new NOC afterwards
		// looks like a commit by another operation.
		p.reportNOCs(func(entries []opcreds.NOCStruct) []opcreds.NOCStruct {
			return forged(*newNOC.Load())(entries)
		}, func() bool { return newNOC.Load() != nil })
		f := p.rejected("the new certificate is in place")
		expectOutcome(t, f, commissioner.CertificateUpdateNewInPlace, "")
		p.expectCauseIndexReported(f)
	})

	t.Run("refuses a peer the controller does not know", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		addr := p.pair.Controller.Fabric().AddressOf(0x4242)
		_, err := p.com.UpdateOperationalCertificate(ocuCtx(t), commissioner.CertificateUpdateOptions{Address: addr, Fabric: p.pair.Controller.Fabric()})
		if err == nil || !strings.Contains(err.Error(), "unknown peer") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("refuses a fabric whose certificate authority the controller does not hold", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		other, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
		if err != nil {
			t.Fatal(err)
		}
		f := p.pair.Controller.Fabric()
		foreign := &commissioner.Fabric{Fabric: f.Fabric, FabricID: f.FabricID, FabricIndex: f.FabricIndex, CA: other}
		rec := p.node.Record()
		_, err = p.com.UpdateOperationalCertificate(ocuCtx(t), commissioner.CertificateUpdateOptions{Address: *rec.PeerAddress, Fabric: foreign})
		if err == nil || !strings.Contains(err.Error(), "not the certificate authority") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("sends the ICAC when the fabric uses one", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{controller: fabrictest.ControllerOptions{IntermediateCA: true}})
		before, icac := p.identity()
		if len(icac) == 0 {
			t.Fatal("no ICAC")
		}
		p.update()
		after, icacAfter := p.identity()
		if bytes.Equal(after, before) || !bytes.Equal(icacAfter, icac) {
			t.Fatal("the NOC did not change or the ICAC did")
		}
		p.nodeLabelReaches("after rotation with ICAC")
	})

	loseUpdateNOCAnswer := func(p *ocuPair, after func(ctx context.Context)) *atomic.Int32 {
		return p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{delivered: true, afterDelivery: after})
	}

	t.Run("completes the update in recovery when the UpdateNOC answer is lost", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		loseUpdateNOCAnswer(p, nil)
		p.update()
		if p.armed() {
			t.Fatal("armed")
		}
		updated := p.noc()
		if bytes.Equal(updated, before) {
			t.Fatal("not updated")
		}
		p.advance(120 * time.Second)
		p.expectNOC(updated, true)
		p.nodeLabelReaches("after lost UpdateNOC answer")
	})

	t.Run("rolls back without CommissioningComplete when UpdateNOC never reached the node", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		sent := p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{})
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		if sent.Load() != 1 || handled.Load() != 0 {
			t.Fatalf("%d UpdateNOC, %d CommissioningComplete", sent.Load(), handled.Load())
		}
		p.expectNOC(before, true)
	})

	t.Run("arms nothing when the current NOC cannot be read", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{})
		p.dropAttribute(readNOCs, nil)
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdatePreviousKept, keepsPrevious)
		p.expectIndexUnread(f)
		if arms.Load() != 0 || p.armed() {
			t.Fatalf("%d arms, armed %v", arms.Load(), p.armed())
		}
	})

	t.Run("disarms its own fail-safe and reports an unknown outcome when NOCs is missing after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		var dropping atomic.Bool
		handled := p.countCommissioningComplete()
		loseUpdateNOCAnswer(p, func(context.Context) { dropping.Store(true) })
		p.dropAttribute(readNOCs, dropping.Load)
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if handled.Load() != 0 || p.armed() {
			t.Fatalf("%d CommissioningComplete, armed %v", handled.Load(), p.armed())
		}
		p.expectNOC(before, true)
	})

	t.Run("refuses a reported NOC that another CA signed", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		other, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
		if err != nil {
			t.Fatal(err)
		}
		f := p.pair.Controller.Fabric()
		noc, err := other.GenerateNOC(p.devicePublicKey(), f.FabricID, p.node.Record().PeerAddress.NodeID, []uint32{0xfffd_0001})
		if err != nil {
			t.Fatal(err)
		}
		p.expectForgedRefused(forged(noc))
	})

	ownNOC := func(p *ocuPair, fabricID, nodeID uint64, cats []uint32) []byte {
		noc, err := p.pair.Controller.CA().GenerateNOC(p.devicePublicKey(), fabricID, nodeID, cats)
		if err != nil {
			t.Fatal(err)
		}
		return noc
	}

	t.Run("refuses a reported NOC for another node", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.expectForgedRefused(forged(ownNOC(p, p.pair.Controller.Fabric().FabricID, 0x4242, nil)))
	})

	t.Run("refuses a reported NOC whose CASE Authenticated Tags differ from the NOC the node authenticated with", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		// An older NOC our CA issued to the same node verifies against the
		// fabric root, node ID and fabric ID.
		p.expectForgedRefused(forged(ownNOC(p, p.pair.Controller.Fabric().FabricID, p.node.Record().PeerAddress.NodeID, []uint32{0xfffd_0001})))
	})

	withCATs := ocuOptions{commission: client.CommissionOptions{Options: commissioner.Options{CATs: []uint32{0x1234_0001}}}}

	t.Run("refuses a reported NOC with other CASE Authenticated Tags of the same count", func(t *testing.T) {
		p := newOCUPair(t, withCATs)
		p.expectForgedRefused(forged(ownNOC(p, p.pair.Controller.Fabric().FabricID, p.node.Record().PeerAddress.NodeID, []uint32{0xfffd_0001})))
	})

	t.Run("refuses a reported NOC for another fabric", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.expectForgedRefused(forged(ownNOC(p, p.pair.Controller.Fabric().FabricID+1, p.node.Record().PeerAddress.NodeID, nil)))
	})

	t.Run("refuses when the node reports more than one NOC for its fabric", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.expectForgedRefused(func(entries []opcreds.NOCStruct) []opcreds.NOCStruct { return append(entries, entries...) })
	})

	t.Run("reports an unknown outcome when NOCs is empty after a disarm", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true})
		p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{delivered: true, answered: true, afterDelivery: func(context.Context) { p.setBreadcrumb(7) }})
		p.reportNOCs(func([]opcreds.NOCStruct) []opcreds.NOCStruct { return []opcreds.NOCStruct{} }, func() bool { return arms.Load() >= 2 })
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if arms.Load() != 2 || p.armed() {
			t.Fatalf("%d arms, armed %v", arms.Load(), p.armed())
		}
	})

	t.Run("reports an unknown outcome when NOCs holds a NOC of another rotation", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		other := ownNOC(p, p.pair.Controller.Fabric().FabricID, p.node.Record().PeerAddress.NodeID, nil)
		var delivered atomic.Bool
		loseUpdateNOCAnswer(p, func(ctx context.Context) {
			p.expireFailsafe(ctx)
			delivered.Store(true)
		})
		p.reportNOCs(forged(other), delivered.Load)
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
	})

	abortedBy := func(cause error) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			ctx, cancel := context.WithCancelCause(ctx)
			cancel(cause)
			return ctx
		}
	}

	t.Run("sends nothing when aborted before the call on a connected peer", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		if p.peer().NewestSession() == nil {
			t.Fatal("no session")
		}
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true})
		cause := errors.New("aborted by test")
		if err := p.updateWith(client.CertificateUpdateOptions{}, abortedBy(cause)); !errors.Is(err, cause) {
			t.Fatalf("got %v", err)
		}
		if arms.Load() != 0 || p.armed() || p.breadcrumb() != 0 {
			t.Fatalf("%d arms, armed %v, Breadcrumb %d", arms.Load(), p.armed(), p.breadcrumb())
		}
	})

	t.Run("sends nothing when aborted while the session is obtained", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true})
		cause := errors.New("aborted by test")
		var cancel context.CancelCauseFunc
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.Connect
			s.Connect = func(ctx context.Context, pr *peer.Peer, o peer.ConnectOptions) (*exchange.Session, error) {
				sess, err := next(ctx, pr, o)
				cancel(cause)
				return sess, err
			}
			return s
		})
		err := p.updateWith(client.CertificateUpdateOptions{}, func(ctx context.Context) context.Context {
			ctx, cancel = context.WithCancelCause(ctx)
			return ctx
		})
		if !errors.Is(err, cause) {
			t.Fatalf("got %v", err)
		}
		if arms.Load() != 0 || p.armed() {
			t.Fatalf("%d arms, armed %v", arms.Load(), p.armed())
		}
	})

	t.Run("completes the update and restarts subscriptions although dropping the resumption record fails", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		var restarts atomic.Int32
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.CloseForPeer
			s.DeleteResumptionRecord = func(exchange.PeerAddress) error { return errors.New("test storage failure") }
			s.CloseForPeer = func(a exchange.PeerAddress) {
				restarts.Add(1)
				next(a)
			}
			return s
		})
		p.update()
		if restarts.Load() != 1 {
			t.Fatalf("%d restarts", restarts.Load())
		}
		p.expectNOC(before, false)
	})

	t.Run("keeps the CASE Authenticated Tags of the current NOC", func(t *testing.T) {
		p := newOCUPair(t, withCATs)
		tags := func() []uint32 {
			cats, err := p.pair.Controller.Fabric().Verifier.PeerCATsFromNOC(p.noc())
			if err != nil {
				t.Fatal(err)
			}
			return cats
		}
		if got := tags(); len(got) != 1 || got[0] != 0x1234_0001 {
			t.Fatalf("CATs %#x", got)
		}
		before := p.noc()
		p.update()
		p.expectNOC(before, false)
		if got := tags(); len(got) != 1 || got[0] != 0x1234_0001 {
			t.Fatalf("CATs after %#x", got)
		}
	})

	t.Run("keeps the new NOC when CommissioningComplete committed but another operation's commit answered NoFailSafe", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(gencomm.ClusterID, gencomm.CmdCommissioningComplete, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			_, _ = next(f)
			return response(gencomm.CommissioningCompleteResponse{ErrorCode: gencomm.CommissioningErrorNoFailSafe}), nil
		})
		p.update()
		if p.armed() {
			t.Fatal("armed")
		}
		p.expectNOC(before, false)
	})

	t.Run("rolls back when the node answers CommissioningComplete with an error status", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		var attempts atomic.Int32
		p.answer(gencomm.ClusterID, gencomm.CmdCommissioningComplete, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			attempts.Add(1)
			return interaction.Result{Status: im.StatusConstraintError}, &interaction.StatusError{Status: im.StatusConstraintError}
		})
		f := p.failure()
		expectOutcome(t, f, commissioner.CertificateUpdateReverted, reverted)
		var se *interaction.StatusError
		if !errors.As(f, &se) {
			t.Fatalf("failure %v, want the status in its cause", f)
		}
		if p.armed() || attempts.Load() != 1 {
			t.Fatalf("armed %v, %d attempts", p.armed(), attempts.Load())
		}
		p.expectNOC(before, true)
	})

	// countDeletes counts the drops of the resumption record.
	countDeletes := func(p *ocuPair) *atomic.Int32 {
		n := new(atomic.Int32)
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.DeleteResumptionRecord
			s.DeleteResumptionRecord = func(a exchange.PeerAddress) error {
				n.Add(1)
				return next(a)
			}
			return s
		})
		return n
	}

	cancellable := func(cancel *context.CancelCauseFunc) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			ctx, *cancel = context.WithCancelCause(ctx)
			return ctx
		}
	}

	t.Run("skips the recovery when aborted after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var cancel context.CancelCauseFunc
		p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{delivered: true, answered: true, afterDelivery: func(context.Context) { cancel(errors.New("aborted by test")) }})
		cleanups := countDeletes(p)
		expectOutcome(t, asUpdateError(t, p.updateWith(client.CertificateUpdateOptions{}, cancellable(&cancel))), commissioner.CertificateUpdateUnknown, unknown)
		// Before the new session and once for the unknown outcome; a
		// recovery would add one more.
		if cleanups.Load() != 2 {
			t.Fatalf("%d cleanups", cleanups.Load())
		}
	})

	t.Run("forgets the sessions when aborted after a lost UpdateNOC answer", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var cancel context.CancelCauseFunc
		loseUpdateNOCAnswer(p, func(context.Context) { cancel(errors.New("aborted by test")) })
		cleanups := countDeletes(p)
		expectOutcome(t, asUpdateError(t, p.updateWith(client.CertificateUpdateOptions{}, cancellable(&cancel))), commissioner.CertificateUpdateUnknown, unknown)
		if cleanups.Load() != 1 {
			t.Fatalf("%d cleanups", cleanups.Load())
		}
	})

	t.Run("completes the update in recovery when the answer to UpdateNOC lacks its response", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.missingResponse(opcreds.ClusterID, opcreds.CmdUpdateNoc, true)
		p.update()
		if p.armed() {
			t.Fatal("armed")
		}
		p.expectNOC(before, false)
	})

	t.Run("tries the recovery when a peer answers CommissioningComplete without its response and without executing it", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		p.missingResponse(gencomm.ClusterID, gencomm.CmdCommissioningComplete, false)
		p.update()
		if handled.Load() != 1 || p.armed() {
			t.Fatalf("%d CommissioningComplete, armed %v", handled.Load(), p.armed())
		}
		p.expectNOC(before, false)
	})

	// Characterization: the read after a disarm also finds the committed NOC.
	t.Run("keeps the new NOC when CommissioningComplete took effect but the answer lacks its response", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		p.missingResponse(gencomm.ClusterID, gencomm.CmdCommissioningComplete, true)
		p.update()
		if handled.Load() != 1 || p.armed() {
			t.Fatalf("%d CommissioningComplete, armed %v", handled.Load(), p.armed())
		}
		p.expectNOC(before, false)
	})

	t.Run("closes the session of an aborted recovery", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var cancel context.CancelCauseFunc
		p.intercept(gencomm.ClusterID, gencomm.CmdCommissioningComplete, fault{})
		var mu sync.Mutex
		var sessions []*exchange.Session
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.Connect
			s.Connect = func(ctx context.Context, pr *peer.Peer, o peer.ConnectOptions) (*exchange.Session, error) {
				sess, err := next(ctx, pr, o)
				mu.Lock()
				sessions = append(sessions, sess)
				// The first session carries UpdateNOC, the second
				// CommissioningComplete, the third the recovery.
				if len(sessions) == 3 {
					cancel(errors.New("aborted by test"))
				}
				mu.Unlock()
				return sess, err
			}
			return s
		})
		expectOutcome(t, asUpdateError(t, p.updateWith(client.CertificateUpdateOptions{}, cancellable(&cancel))), commissioner.CertificateUpdateUnknown, unknown)
		mu.Lock()
		defer mu.Unlock()
		if len(sessions) != 3 || sessions[2] == nil {
			t.Fatalf("%d sessions", len(sessions))
		}
		if s := sessions[2]; !s.IsClosing() && !s.IsClosed() && !s.IsPeerLost() {
			t.Fatal("the recovery's session is open")
		}
	})

	t.Run("reports the previous certificate when the fail-safe ended before the update completed", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		loseUpdateNOCAnswer(p, p.expireFailsafe)
		arms := p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true})
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		// Only the arm at the start: a fail-safe that already ended is not
		// disarmed.
		if arms.Load() != 1 || handled.Load() != 0 {
			t.Fatalf("%d arms, %d CommissioningComplete", arms.Load(), handled.Load())
		}
		p.expectNOC(before, true)
		p.nodeLabelReaches("after expired fail-safe")
	})

	t.Run("reports an unknown outcome when the disarm after a refused CommissioningComplete fails", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.refuseDisarm()
		refuseCommissioningComplete(p)
		started := p.site.Clock().Now()
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if d := p.site.Clock().Now().Sub(started); d >= time.Minute {
			t.Fatalf("took %s", d)
		}
		if !p.armed() {
			t.Fatal("not armed")
		}
		p.advance(70 * time.Second)
		if p.armed() {
			t.Fatal("still armed after the fail-safe's expiry")
		}
		p.expectNOC(before, true)
	})

	t.Run("reports an unknown outcome when the previous NOC is back but Breadcrumb is not 0", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		loseUpdateNOCAnswer(p, func(ctx context.Context) {
			p.expireFailsafe(ctx)
			p.setBreadcrumb(7)
		})
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
	})

	t.Run("reports an unknown outcome when the node reports no NOCs value", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var dropping atomic.Bool
		loseUpdateNOCAnswer(p, func(ctx context.Context) {
			p.expireFailsafe(ctx)
			dropping.Store(true)
		})
		p.dropAttribute(readNOCs, dropping.Load)
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
	})

	afterUpdateNOC := func(p *ocuPair, fn func()) {
		p.intercept(opcreds.ClusterID, opcreds.CmdUpdateNoc, fault{delivered: true, answered: true, afterDelivery: func(context.Context) { fn() }})
	}

	t.Run("disarms a fail-safe another operation of the fabric re-armed after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		afterUpdateNOC(p, func() { p.setBreadcrumb(7) })
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		if handled.Load() != 0 || p.breadcrumb() != 0 {
			t.Fatalf("%d CommissioningComplete, Breadcrumb %d", handled.Load(), p.breadcrumb())
		}
		p.expectNOC(before, true)
	})

	t.Run("reports an unknown outcome when the node refuses to disarm a foreign fail-safe", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		handled := p.countCommissioningComplete()
		p.refuseDisarm()
		afterUpdateNOC(p, func() { p.setBreadcrumb(7) })
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if handled.Load() != 0 || !p.armed() || p.breadcrumb() != 7 {
			t.Fatalf("%d CommissioningComplete, armed %v, Breadcrumb %d", handled.Load(), p.armed(), p.breadcrumb())
		}
	})

	t.Run("disarms a fail-safe re-armed with Breadcrumb 0 while the new NOC is pending", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		handled := p.countCommissioningComplete()
		afterUpdateNOC(p, func() { p.setBreadcrumb(0) })
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		if handled.Load() != 0 {
			t.Fatalf("%d CommissioningComplete", handled.Load())
		}
		p.expectNOC(before, true)
	})

	t.Run("tries CommissioningComplete once more and then reports an unknown outcome", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		sent := p.intercept(gencomm.ClusterID, gencomm.CmdCommissioningComplete, fault{})
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if sent.Load() != 2 || !p.armed() {
			t.Fatalf("%d CommissioningComplete, armed %v", sent.Load(), p.armed())
		}
	})

	t.Run("reports an unknown outcome when Breadcrumb is not 0 again after the disarm", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var completed atomic.Bool
		p.intercept(gencomm.ClusterID, gencomm.CmdCommissioningComplete, fault{delivered: true, afterDelivery: func(context.Context) { completed.Store(true) }})
		p.intercept(gencomm.ClusterID, gencomm.CmdArmFailSafe, fault{delivered: true, answered: true, afterDelivery: func(context.Context) {
			if completed.Load() {
				p.setBreadcrumb(7)
			}
		}})
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
	})

	t.Run("reports an unknown outcome when the disarm of a Breadcrumb 0 fail-safe fails", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		handled := p.countCommissioningComplete()
		p.refuseDisarm()
		afterUpdateNOC(p, func() { p.setBreadcrumb(0) })
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
		if handled.Load() != 0 || !p.armed() {
			t.Fatalf("%d CommissioningComplete, armed %v", handled.Load(), p.armed())
		}
	})

	t.Run("disarms a foreign fail-safe in recovery after a lost UpdateNOC answer", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		handled := p.countCommissioningComplete()
		loseUpdateNOCAnswer(p, func(context.Context) { p.setBreadcrumb(7) })
		expectOutcome(t, p.rejected(reverted), commissioner.CertificateUpdateReverted, reverted)
		if handled.Load() != 0 || p.breadcrumb() != 0 {
			t.Fatalf("%d CommissioningComplete, Breadcrumb %d", handled.Load(), p.breadcrumb())
		}
	})

	t.Run("completes the update when dropping the resumption record fails once after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		var failed atomic.Bool
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			next := s.DeleteResumptionRecord
			s.DeleteResumptionRecord = func(a exchange.PeerAddress) error {
				if !failed.Swap(true) {
					return errors.New("test storage failure")
				}
				return next(a)
			}
			return s
		})
		p.update()
		if !failed.Load() || p.armed() {
			t.Fatalf("failed %v, armed %v", failed.Load(), p.armed())
		}
		p.expectNOC(before, false)
	})

	t.Run("reports an unknown outcome when the node cannot be reached after UpdateNOC", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		loseUpdateNOCAnswer(p, func(ctx context.Context) {
			if err := p.pair.Device.Stop(ctx); err != nil {
				t.Error(err)
			}
		})
		expectOutcome(t, p.failure(), commissioner.CertificateUpdateUnknown, unknown)
	})

	t.Run("rolls back when the CSR response is empty", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(opcreds.ClusterID, opcreds.CmdCsrRequest, func(spec.Encodable, func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			return response(opcreds.CsrResponse{NocsrElements: []byte{}, AttestationSignature: make([]byte, 64)}), nil
		})
		f := p.failure()
		if c := nocsrFailureOf(f); c != attestation.NOCSRResponseEmpty {
			t.Fatalf("NOCSR check %q (%v)", c, f)
		}
		if p.armed() {
			t.Fatal("armed")
		}
		p.expectNOC(before, true)
	})

	t.Run("rolls back when the CSR response carries another CSRNonce", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		before := p.noc()
		p.answer(opcreds.ClusterID, opcreds.CmdCsrRequest, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			req := f.(opcreds.CsrRequestRequest)
			nonce := make([]byte, len(req.CsrNonce))
			for i, b := range req.CsrNonce {
				nonce[i] = b ^ 0xff
			}
			req.CsrNonce = nonce
			return next(req)
		})
		f := p.failure()
		if c := nocsrFailureOf(f); c != attestation.NOCSRCSRNonceMismatch {
			t.Fatalf("NOCSR check %q (%v)", c, f)
		}
		if p.armed() {
			t.Fatal("armed")
		}
		p.expectNOC(before, true)
	})

	t.Run("accepts the fail-safe bounds 30 s and 65535 s", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		for _, fs := range []time.Duration{30 * time.Second, 0xffff * time.Second} {
			before := p.noc()
			if err := p.updateWith(client.CertificateUpdateOptions{Failsafe: fs}, nil); err != nil {
				t.Fatalf("fail-safe %s: %v", fs, err)
			}
			p.expectNOC(before, false)
		}
	})

	t.Run("fails fast without arming a fail-safe when aborted before connecting", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		p.peer().Disconnect(ocuCtx(t), errors.New("test"))
		if err := p.updateWith(client.CertificateUpdateOptions{}, abortedBy(errors.New("aborted by test"))); err == nil {
			t.Fatal("no error")
		}
		if p.armed() {
			t.Fatal("armed")
		}
	})

	t.Run("leaves no interaction open", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		var mu sync.Mutex
		var opened []*interaction.Interaction
		p.patch(func(s commissioner.NOCUpdateSeam) commissioner.NOCUpdateSeam {
			s.Opened = func(ia *interaction.Interaction) {
				mu.Lock()
				opened = append(opened, ia)
				mu.Unlock()
			}
			return s
		})
		p.update()
		mu.Lock()
		defer mu.Unlock()
		if len(opened) != 2 {
			t.Fatalf("%d interactions", len(opened))
		}
		for i, ia := range opened {
			if _, err := ia.Read(ocuCtx(t), im.ConcreteAttributePath{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true}); !errors.Is(err, interaction.ErrClosed) {
				t.Fatalf("interaction %d: %v, want closed", i, err)
			}
		}
	})

	t.Run("refuses a second update or a commission while the certificate update runs", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		r := p.pair.Controller.Registry()
		var second, commission error
		var once sync.Once
		p.answer(gencomm.ClusterID, gencomm.CmdArmFailSafe, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			once.Do(func() {
				second = r.UpdateOperationalCertificate(context.Background(), p.node, client.CertificateUpdateOptions{})
				commission = r.Commission(context.Background(), p.node, client.CommissionOptions{Options: commissioner.Options{Passcode: 20202021}})
			})
			return next(f)
		})
		p.update()
		for _, c := range []struct {
			err  error
			verb string
		}{{second, "update the operational certificate of"}, {commission, "commission"}} {
			if !errors.Is(c.err, client.ErrFabricOperationInProgress) || !strings.Contains(c.err.Error(), "cannot "+c.verb+" ") ||
				!strings.HasSuffix(c.err.Error(), "because a certificate update attempt is already in progress") {
				t.Fatalf("got %v", c.err)
			}
		}
	})

	t.Run("refuses to decommission while the certificate update runs", func(t *testing.T) {
		p := newOCUPair(t, ocuOptions{})
		r := p.pair.Controller.Registry()
		var decommission error
		var once sync.Once
		p.answer(gencomm.ClusterID, gencomm.CmdArmFailSafe, func(f spec.Encodable, next func(spec.Encodable) (interaction.Result, error)) (interaction.Result, error) {
			once.Do(func() { decommission = r.Decommission(context.Background(), p.node) })
			return next(f)
		})
		p.update()
		if !errors.Is(decommission, client.ErrFabricOperationInProgress) || !strings.Contains(decommission.Error(), "a certificate update attempt is already in progress") {
			t.Fatalf("got %v", decommission)
		}
	})
}
