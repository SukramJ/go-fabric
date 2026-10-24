// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/operational-credentials/OperationalCredentialsServerTest.ts
// at 862ad75f: the AddNOC admin vendor ID and certificate checks,
// SetVIDVerificationStatement's vendor ID check, UpdateNOC keeping the
// fabric's group state, and a NOC for another fabric refused.
//
// matter.js runs a MockSite pair and the controller's
// OperationalCredentialsClient, or a MockServerNode driven locally with a
// TestFabric authority's certificates. The port runs fabrictest pairs and
// sends the commands over the wire — over the pair's CASE session, or over
// a PASE session (Controller.PASE) for the certificate checks, which run
// against an uncommissioned device as createOnline() does — with every
// certificate issued by the site controller's certificate authority
// (Controller.CA, matter.js TestFabric.Authority / FabricAuthority.ca). The
// TypeScript StatusResponseError codes are the IM status of the answer
// (*interaction.StatusError); NOCResponse codes are its StatusCode field.
//
// Missing (register): "keeps group keys, endpoints, multicast policy and
// group ACL of the fabric" asserts besides the group state that the
// socket's multicast memberships do not change across the update
// (recordMembershipChanges, multicast-helpers.ts); fabrictest's network
// has no multicast, so that leg waits for its multicast model (wave 18c) —
// the group state legs run here. "treats UpdateFabricLabel as a final
// fabric change" and "treats SetVIDVerificationStatement as a final fabric
// change" assert the node's fabricsChanged event "1 updated"
// (CommissioningServer.ts:118-141 via OperationalCredentialsServer.ts:579-582),
// which matter.js raises on every persist of a fabric (FabricManager.ts:236-246,
// persistCallback); the Go node's is Observers.OnFabricUpdated
// (TestOperationalCredentialsFinalFabricChange).
package node_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// OperationalCredentials and GeneralCommissioning command ids.
const (
	ocCluster        uint32 = 0x003E
	ocCSRRequest     uint32 = 0x04
	ocAddNOC         uint32 = 0x06
	ocUpdateNOC      uint32 = 0x07
	ocAddTrustedRoot uint32 = 0x0B
	ocSetVIDStmt     uint32 = 0x0C
	gcCluster        uint32 = 0x0030
	gcArmFailSafe    uint32 = 0x00
	// failsafeLengthS is node-helpers.ts FAILSAFE_LENGTH_S (:104).
	failsafeLengthS uint16 = 60
)

// tlvFields is a command's fields as a structure the encoder fills.
type tlvFields func(enc *tlv.Encoder)

func (f tlvFields) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	f(enc)
	_ = enc.EndContainer()
}

// field is the element of a structure with the context tag.
func field(n spec.Node, tag uint32) (tlv.Element, bool) {
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == tag {
			return c.El, true
		}
	}
	return tlv.Element{}, false
}

// imStatus is the IM status of a command's answer: Success, or the status
// of the *interaction.StatusError.
func imStatus(t *testing.T, err error) im.StatusCode {
	t.Helper()
	if err == nil {
		return im.StatusSuccess
	}
	var se *interaction.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("answer: %v", err)
	}
	return se.Status
}

// invokeOC invokes an OperationalCredentials command on the root.
func invokeOC(t *testing.T, p *fabrictest.Pair, cmd uint32, fields spec.Encodable) (interaction.Result, error) {
	t.Helper()
	return fabrictest.Invoke(parityCtx(t), p, im.ConcreteCommandPath{Endpoint: 0, Cluster: ocCluster, Command: cmd}, fields)
}

// nocStatus is a NOCResponse's StatusCode.
func nocStatus(t *testing.T, res interaction.Result, err error) uint64 {
	t.Helper()
	if err != nil {
		t.Fatalf("NOC command: %v", err)
	}
	el, ok := field(res.Fields, 0)
	if !res.HasFields || !ok {
		t.Fatalf("no NOCResponse: %+v", res)
	}
	return el.Uint
}

// armFailSafe is GeneralCommissioningClient.armFailSafe({ expiryLengthSeconds:
// FAILSAFE_LENGTH_S, breadcrumb: 1 }).
func armFailSafe(t *testing.T, p *fabrictest.Pair) {
	t.Helper()
	res, err := fabrictest.Invoke(parityCtx(t), p, im.ConcreteCommandPath{Endpoint: 0, Cluster: gcCluster, Command: gcArmFailSafe},
		gencommdef.ArmFailSafeRequest{ExpiryLengthSeconds: failsafeLengthS, Breadcrumb: 1})
	if err != nil {
		t.Fatalf("ArmFailSafe: %v", err)
	}
	if el, ok := field(res.Fields, 0); !ok || el.Uint != 0 {
		t.Fatalf("ArmFailSafe answered %+v", res)
	}
}

// csrPublicKey sends a CSRRequest and returns the public key of the CSR the
// device answers with — Certificate.getPublicKeyFromCsr of
// TlvCertSigningRequest.decode(nocsrElements).certSigningRequest.
func csrPublicKey(t *testing.T, p *fabrictest.Pair, forUpdate bool) []byte {
	t.Helper()
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	res, err := invokeOC(t, p, ocCSRRequest, tlvFields(func(enc *tlv.Encoder) {
		enc.PutOctets(tlv.ContextTag(0), nonce)
		if forUpdate {
			enc.PutBool(tlv.ContextTag(1), true)
		}
	}))
	if err != nil {
		t.Fatalf("CSRRequest: %v", err)
	}
	elements, ok := field(res.Fields, 0)
	if !ok {
		t.Fatalf("CSRResponse without NOCSRElements: %+v", res)
	}
	dec := tlv.NewDecoder(elements.Octets)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	nocsr, err := spec.ReadContainer(dec, open)
	if err != nil {
		t.Fatal(err)
	}
	csr, ok := field(nocsr, 1)
	if !ok {
		t.Fatal("NOCSRElements without a CSR")
	}
	req, err := x509.ParseCertificateRequest(csr.Octets)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := req.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("CSR key %T", req.PublicKey)
	}
	pub, err := key.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// addNOC is AddNOC with the given fields.
func addNOC(noc, icac, ipk []byte, caseAdminSubject uint64, adminVendorID uint16) tlvFields {
	return func(enc *tlv.Encoder) {
		enc.PutOctets(tlv.ContextTag(0), noc)
		if icac != nil {
			enc.PutOctets(tlv.ContextTag(1), icac)
		}
		enc.PutOctets(tlv.ContextTag(2), ipk)
		enc.PutUint(tlv.ContextTag(3), caseAdminSubject)
		enc.PutUint(tlv.ContextTag(4), uint64(adminVendorID))
	}
}

// TestOperationalCredentialsAddNOCAdminVendor ports the describe "addNoc"
// (OperationalCredentialsServerTest.ts:72-84): an AddNOC from the
// commissioned controller with a ten-byte NOC and no fail-safe answers
// InvalidCommand for the Matter Standard and a reserved admin vendor ID —
// checked first (OperationalCredentialsServer.ts:217-222) — and
// FailsafeRequired for a valid one, the next check (:224).
func TestOperationalCredentialsAddNOCAdminVendor(t *testing.T) {
	holdSite(t)
	for _, tc := range []struct {
		name   string
		vendor uint16
		want   im.StatusCode
	}{
		{"refuses the Matter Standard vendor ID as admin vendor ID", 0x0000, im.StatusInvalidCommand},
		{"refuses a reserved vendor ID as admin vendor ID", 0xFFF5, im.StatusInvalidCommand},
		{"checks the fail-safe after a valid admin vendor ID", 0xFFF1, im.StatusFailsafeRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
			_, err := invokeOC(t, p, ocAddNOC, addNOC(make([]byte, 10), nil, make([]byte, 16), 1, tc.vendor))
			if got := imStatus(t, err); got != tc.want {
				t.Errorf("AddNOC with admin vendor 0x%04X: %v, want %v", tc.vendor, got, tc.want)
			}
		})
	}
}

// TestOperationalCredentialsAddNOCCertificateChecks ports the describe
// "addNoc certificate checks" (OperationalCredentialsServerTest.ts:86-154):
// over a PASE session to an uncommissioned device, ArmFailSafe, CSRRequest,
// AddTrustedRootCertificate with the authority's root and AddNOC with the
// authority's NOC for the CSR key. A NOC node ID outside the operational
// range answers InvalidNodeOpId, a CAT admin subject of version 0
// InvalidAdminSubject, a valid pair Ok (OperationalCredentialsServer.ts
// #mapNocErrors :168-206).
func TestOperationalCredentialsAddNOCCertificateChecks(t *testing.T) {
	holdSite(t)
	addNOCStatus := func(t *testing.T, nocNodeID, caseAdminSubject uint64) uint64 {
		t.Helper()
		site := quietSite(t)
		c := site.AddController(fabrictest.ControllerOptions{})
		d := site.AddDevice(fabrictest.Topology{})
		p, err := c.PASE(parityCtx(t), d)
		if err != nil {
			t.Fatal(err)
		}
		armFailSafe(t, p)
		pub := csrPublicKey(t, p, false)
		if _, err := invokeOC(t, p, ocAddTrustedRoot, tlvFields(func(enc *tlv.Encoder) {
			enc.PutOctets(tlv.ContextTag(0), c.CA().RootCert())
		})); err != nil {
			t.Fatalf("AddTrustedRootCertificate: %v", err)
		}
		noc, err := c.CA().GenerateNOC(pub, c.Fabric().FabricID, nocNodeID, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := invokeOC(t, p, ocAddNOC, addNOC(noc, c.CA().ICACCert(), c.Fabric().IPK, caseAdminSubject, 0xFFF1))
		return nocStatus(t, res, err)
	}

	t.Run("answers InvalidNodeOpId for a NOC whose node ID is outside the operational range", func(t *testing.T) {
		if got := addNOCStatus(t, 0xFFFF_FFFF_0000_0001, 100); got != uint64(core.NOCStatusInvalidNodeOpID) {
			t.Errorf("StatusCode %d, want InvalidNodeOpId (%d)", got, core.NOCStatusInvalidNodeOpID)
		}
	})
	t.Run("answers InvalidAdminSubject for a CAT admin subject with version 0", func(t *testing.T) {
		if got := addNOCStatus(t, 1, 0xFFFF_FFFD_0001_0000); got != uint64(core.NOCStatusInvalidAdminSubject) {
			t.Errorf("StatusCode %d, want InvalidAdminSubject (%d)", got, core.NOCStatusInvalidAdminSubject)
		}
	})
	t.Run("adds the fabric for a valid NOC and admin subject", func(t *testing.T) {
		if got := addNOCStatus(t, 1, 100); got != uint64(core.NOCStatusOK) {
			t.Errorf("StatusCode %d, want Ok", got)
		}
	})
}

// TestOperationalCredentialsSetVIDVerificationStatementVendor ports
// "refuses the Matter Standard vendor ID" of setVidVerificationStatement
// (OperationalCredentialsServerTest.ts:156-164): VendorID 0 answers ConstraintError
// (OperationalCredentialsServer.ts:516-518, VendorId.isOperational).
func TestOperationalCredentialsSetVIDVerificationStatementVendor(t *testing.T) {
	holdSite(t)
	p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
	_, err := invokeOC(t, p, ocSetVIDStmt, tlvFields(func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0)
	}))
	if got := imStatus(t, err); got != im.StatusConstraintError {
		t.Errorf("SetVIDVerificationStatement with VendorID 0: %v, want ConstraintError", got)
	}
}

// updateNOCGroup is the describe's GROUP, 0x0001.
const updateNOCGroup uint16 = 0x0001

// groupState is groupStateOf (OperationalCredentialsServerTest.ts:212-229):
// what a group message for the group meets on the fabric — the key set
// GroupKeyMap names, the endpoints, the multicast address and the access a
// group subject has to OnOff on the group's endpoint (Operate and View:
// Operate granted, Manage not).
type groupState struct {
	keySetID  uint16
	endpoints []uint16
	address   string
	operate   bool
	manage    bool
}

func groupStateOf(t *testing.T, n *node.Node, fabric uint8, ep uint16) groupState {
	t.Helper()
	ctx := context.Background()
	var gs groupState
	maps, err := n.Store().ListGroupKeyMappings(ctx, fabric)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range maps {
		if m.GroupID == updateNOCGroup {
			gs.keySetID = m.GroupKeySetID
		}
	}
	table, err := n.Groups().GroupTable(ctx, fabric)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range table {
		if e.GroupID == updateNOCGroup {
			gs.endpoints = slices.Sorted(slices.Values(e.Endpoints))
		}
	}
	if addr, err := n.Groups().MulticastAddressFor(ctx, fabric, updateNOCGroup); err == nil {
		gs.address = addr.String()
	}
	checker := n.Bridge().Dispatcher().(im.ACLChecker)
	group := im.WithGroupSubject(ctx, im.GroupSubject{GroupID: updateNOCGroup, HasValidMapping: true, Endpoints: []uint16{ep}})
	gs.operate = checker.CheckACL(group, fabric, 0, nil, ep, 0x0006, 3) == im.StatusSuccess
	gs.manage = checker.CheckACL(group, fabric, 0, nil, ep, 0x0006, 4) == im.StatusSuccess
	return gs
}

func (g groupState) equal(o groupState) bool {
	return g.keySetID == o.keySetID && slices.Equal(g.endpoints, o.endpoints) && g.address == o.address &&
		g.operate == o.operate && g.manage == o.manage
}

// expectedGroupState is the describe's expectedGroupState (:231-236) for
// the group on ep.
func expectedGroupState(endpoints ...uint16) groupState {
	return groupState{keySetID: 1, endpoints: endpoints, address: groups.IANAGroupcastAddress.String(), operate: true}
}

// groupDevice is deviceWithGroup (OperationalCredentialsServerTest.ts:192-209):
// a commissioned device with an OnOffLight and a Groupcast group on the
// light's endpoint, key set 1, an auxiliary ACL entry and the IANA address.
// observers are the device's node observers.
type groupDevice struct {
	site    *fabrictest.Site
	pair    *fabrictest.Pair
	fabric  uint8
	nodeID  uint64
	ep      uint16
	mu      sync.Mutex
	updated []groupState // group state at each OnFabricUpdated
	removed []uint8
}

func newGroupDevice(t *testing.T) *groupDevice {
	t.Helper()
	g := &groupDevice{site: quietSite(t)}
	light := newLight(t, g.site, "light:1")
	var dev *fabrictest.Device
	g.pair = g.site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{light},
		Configure: func(cfg *node.Config) {
			cfg.Observers.OnFabricUpdated = func(f node.FabricInfo) error {
				gs := groupStateOf(t, dev.Node(), f.FabricIndex, g.ep)
				g.mu.Lock()
				g.updated = append(g.updated, gs)
				g.mu.Unlock()
				return nil
			}
			cfg.Observers.OnFabricRemoved = func(f node.FabricInfo) error {
				g.mu.Lock()
				g.removed = append(g.removed, f.FabricIndex)
				g.mu.Unlock()
				return nil
			}
		},
	}})
	dev = g.pair.Device
	ep, ok := dev.EndpointOf("light:1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	g.ep = ep
	fabrics, err := dev.Node().FabricInfos(context.Background())
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	g.fabric, g.nodeID = fabrics[0].FabricIndex, fabrics[0].NodeID
	policy := groups.PolicyIanaAddr
	if err := dev.Act(func(n *node.Node) error {
		_, err := n.Root().Groupcast.MatterInvoke(adminContext(g.fabric), 0x00, core.JoinGroupRequest{
			GroupID: updateNOCGroup, Endpoints: []uint16{ep}, KeySetID: 1, Key: make([]byte, 16),
			UseAuxiliaryACL: &[]bool{true}[0], McastAddrPolicy: &policy,
		})
		return err
	}); err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
	return g
}

// adminContext is a CASE request of the fabric's administrator.
func adminContext(fabric uint8) context.Context {
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, fabric), 112233, nil)
	return im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
}

// updateNOC is the describe's updateNoc (:183-189): ArmFailSafe, CSRRequest
// for UpdateNOC, UpdateNOC with the controller's NOC for the CSR key on
// fabricID and the device's node id.
func updateNOC(t *testing.T, p *fabrictest.Pair, fabricID, nodeID uint64) uint64 {
	t.Helper()
	armFailSafe(t, p)
	pub := csrPublicKey(t, p, true)
	noc, err := p.Controller.CA().GenerateNOC(pub, fabricID, nodeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	icac := p.Controller.CA().ICACCert()
	res, err := invokeOC(t, p, ocUpdateNOC, tlvFields(func(enc *tlv.Encoder) {
		enc.PutOctets(tlv.ContextTag(0), noc)
		if icac != nil {
			enc.PutOctets(tlv.ContextTag(1), icac)
		}
	}))
	return nocStatus(t, res, err)
}

// identity is the fabric's operational credentials as the device holds them.
func identity(t *testing.T, d *fabrictest.Device, fabric uint8) store.IdentityRecord {
	t.Helper()
	id, err := d.Store().GetIdentity(context.Background(), fabric)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestOperationalCredentialsUpdateNOC ports the describe "UpdateNOC"
// (OperationalCredentialsServerTest.ts:166-318). The Go UpdateNOC replaces
// the fabric's identity in place (fabric index, group keys, group table and
// ACL keyed by it), and a fail-safe expiry restores the identity
// (revertUpdateNOC); "fabrics[0] not equals previous" is the stored NOC
// changed, FabricManager's replaced event is Observers.OnFabricUpdated, and
// its deleting/deleted pair is Observers.OnFabricRemoved, the one removal
// event of the Go node.
func TestOperationalCredentialsUpdateNOC(t *testing.T) {
	holdSite(t)
	t.Run("keeps group keys, endpoints, multicast policy and group ACL of the fabric", func(t *testing.T) {
		g := newGroupDevice(t)
		dev := g.pair.Device
		if got := groupStateOf(t, dev.Node(), g.fabric, g.ep); !got.equal(expectedGroupState(g.ep)) {
			t.Fatalf("group state before = %+v, want %+v", got, expectedGroupState(g.ep))
		}
		previous := identity(t, dev, g.fabric)
		// recordMembershipChanges and network.isMemberOf
		// (OperationalCredentialsServerTest.ts:260-261, :272-273): the
		// socket's memberships on fabrictest's multicast model.
		changes := watchMemberships(t, g.site)
		if !holdsGroup(dev, groups.IANAGroupcastAddress) {
			t.Fatalf("before UpdateNOC the device holds %v, want the IANA address", dev.MulticastGroups())
		}

		if got := updateNOC(t, g.pair, g.pair.Controller.Fabric().FabricID, g.nodeID); got != uint64(core.NOCStatusOK) {
			t.Fatalf("UpdateNOC StatusCode %d, want Ok", got)
		}

		if bytes.Equal(identity(t, dev, g.fabric).NOC, previous.NOC) {
			t.Error("the fabric still holds the previous NOC")
		}
		g.mu.Lock()
		updated := slices.Clone(g.updated)
		g.mu.Unlock()
		if len(updated) != 1 || !updated[0].equal(expectedGroupState(g.ep)) {
			t.Errorf("group state at the replacement = %+v, want one %+v", updated, expectedGroupState(g.ep))
		}
		if got := groupStateOf(t, dev.Node(), g.fabric, g.ep); !got.equal(expectedGroupState(g.ep)) {
			t.Errorf("group state after = %+v", got)
		}
		if err := g.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := changes(); len(got) != 0 {
			t.Errorf("UpdateNOC changed the multicast memberships: %v, want none", got)
		}
		if !holdsGroup(dev, groups.IANAGroupcastAddress) {
			t.Errorf("after UpdateNOC the device holds %v, want the IANA address", dev.MulticastGroups())
		}
	})

	t.Run("keeps the group state when the fail-safe expires and restores the previous credentials", func(t *testing.T) {
		g := newGroupDevice(t)
		dev := g.pair.Device
		previous := identity(t, dev, g.fabric)

		if got := updateNOC(t, g.pair, g.pair.Controller.Fabric().FabricID, g.nodeID); got != uint64(core.NOCStatusOK) {
			t.Fatalf("UpdateNOC StatusCode %d, want Ok", got)
		}
		if bytes.Equal(identity(t, dev, g.fabric).NOC, previous.NOC) {
			t.Fatal("UpdateNOC kept the previous NOC")
		}
		ep2 := g.ep + 1
		if err := dev.Act(func(n *node.Node) error {
			return n.Groups().AddEndpointForGroup(context.Background(), g.fabric, updateNOCGroup, ep2, "")
		}); err != nil {
			t.Fatal(err)
		}

		// The fail-safe runs out on the site's clock.
		if err := g.site.Step(parityCtx(t), time.Duration(failsafeLengthS)*time.Second); err != nil {
			t.Fatal(err)
		}
		restored := func() bool { return bytes.Equal(identity(t, dev, g.fabric).NOC, previous.NOC) }
		if err := g.site.Eventually(parityCtx(t), "the previous credentials restored", restored); err != nil {
			t.Fatal(err)
		}

		want := expectedGroupState(slices.Sorted(slices.Values([]uint16{g.ep, ep2}))...)
		if got := groupStateOf(t, dev.Node(), g.fabric, g.ep); !got.equal(want) {
			t.Errorf("group state after the restore = %+v, want %+v", got, want)
		}

		if err := dev.Act(func(n *node.Node) error { return n.RemoveFabric(context.Background(), g.fabric) }); err != nil {
			t.Fatal(err)
		}
		g.mu.Lock()
		removed := slices.Clone(g.removed)
		g.mu.Unlock()
		if !slices.Equal(removed, []uint8{g.fabric}) {
			t.Errorf("fabric removals %v, want [%d]", removed, g.fabric)
		}
	})

	t.Run("rejects a NOC for another fabric ID with InvalidNOC", func(t *testing.T) {
		p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
		fabrics, err := p.Device.Node().FabricInfos(context.Background())
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("fabrics %v, %v", fabrics, err)
		}
		before := identity(t, p.Device, fabrics[0].FabricIndex)

		got := updateNOC(t, p, fabrics[0].FabricID+1, fabrics[0].NodeID)

		if got != uint64(core.NOCStatusInvalidNOC) {
			t.Errorf("UpdateNOC StatusCode %d, want InvalidNOC (%d)", got, core.NOCStatusInvalidNOC)
		}
		after, err := p.Device.Node().FabricInfos(context.Background())
		if err != nil || len(after) != 1 || !sameFabric(after[0], fabrics[0]) {
			t.Errorf("fabrics after = %+v, want %+v", after, fabrics)
		}
		if !bytes.Equal(identity(t, p.Device, fabrics[0].FabricIndex).NOC, before.NOC) {
			t.Error("the refused NOC replaced the fabric's")
		}
	})
}

func sameFabric(a, b node.FabricInfo) bool {
	return a.FabricIndex == b.FabricIndex && a.FabricID == b.FabricID && a.NodeID == b.NodeID &&
		bytes.Equal(a.RootPublicKey, b.RootPublicKey) && a.CompressedFabricID == b.CompressedFabricID
}

// ocUpdateFabricLabel is UpdateFabricLabel's command id.
const ocUpdateFabricLabel uint32 = 0x09

// TestOperationalCredentialsFinalFabricChange ports the describe "while a
// fail-safe without AddNOC or UpdateNOC is armed"
// (OperationalCredentialsServerTest.ts:320-359). matter.js's
// fabricsChanged(index, "updated") is the host's Observers.OnFabricUpdated
// (fix: a label or vendor ID change reports the fabric,
// FabricManager.ts:236-246); its captureLog of errors is the node's records
// at error level; CommissioningServer's commissioned is the node holding
// its one fabric.
func TestOperationalCredentialsFinalFabricChange(t *testing.T) {
	holdSite(t)
	changeFabric := func(t *testing.T, cmd uint32, fields spec.Encodable) node.FabricInfo {
		t.Helper()
		var mu sync.Mutex
		var changes []uint8
		errLog := &bqLog{}
		p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Logger = slog.New(&bqHandler{log: errLog})
			cfg.Observers.OnFabricUpdated = func(f node.FabricInfo) error {
				mu.Lock()
				changes = append(changes, f.FabricIndex)
				mu.Unlock()
				return nil
			}
		}}})
		armFailSafe(t, p)
		mark := errLog.mark()
		mu.Lock()
		before := len(changes)
		mu.Unlock()
		res, err := invokeOC(t, p, cmd, fields)
		if cmd == ocUpdateFabricLabel {
			if got := nocStatus(t, res, err); got != 0 {
				t.Fatalf("UpdateFabricLabel: NOCResponse status %d", got)
			}
		} else if err != nil {
			t.Fatalf("SetVIDVerificationStatement: %v", err)
		}
		for _, r := range errLog.records()[mark:] {
			if r.level >= slog.LevelError {
				t.Errorf("error logged: %s %v", r.msg, r.attrs)
			}
		}
		fabrics, err := p.Device.Node().FabricInfos(parityCtx(t))
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("fabrics %+v, %v: the node is not commissioned", fabrics, err)
		}
		mu.Lock()
		got := slices.Clone(changes[before:])
		mu.Unlock()
		if !slices.Equal(got, []uint8{fabrics[0].FabricIndex}) {
			t.Errorf("fabric changes %v, want [%d updated]", got, fabrics[0].FabricIndex)
		}
		return fabrics[0]
	}

	// :346-350.
	t.Run("treats UpdateFabricLabel as a final fabric change", func(t *testing.T) {
		f := changeFabric(t, ocUpdateFabricLabel, tlvFields(func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(0), "renamed") }))
		if f.Label != "renamed" {
			t.Errorf("label %q, want renamed", f.Label)
		}
	})

	// :352-358.
	t.Run("treats SetVIDVerificationStatement as a final fabric change", func(t *testing.T) {
		f := changeFabric(t, ocSetVIDStmt, tlvFields(func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 0xFFF2) }))
		if f.VendorID != 0xFFF2 {
			t.Errorf("vendor ID 0x%04X, want 0xFFF2", f.VendorID)
		}
	})
}
