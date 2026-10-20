// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/access-control/AccessControlAuxiliaryTest.ts
// at 862ad75f: the auxiliary entries of the AccessControl Auxiliary feature
// kept next to the real entries across restarts and ACL writes, the
// AuxiliaryAccessUpdated event per changed fabric, the refused AuxiliaryType
// on a local write and the per-entry split.
//
// matter.js registers a free observable as provider and records every list
// FabricAccessControl.aclList installs (recordAcl). The Go AccessControl has
// no installed list: its provider is the node's group state (Groupcast,
// AuxiliaryACLSource), and the access check composes the stored entries and
// the provider's at every request (endpoint.TopologyDispatcher.CheckACL,
// AccessControlServer #applyFabricAcl's `[...realAcl, ...auxiliary]`). So
// the cases that read the installed list run on a fabrictest device and
// assert what the installed list decides — the device's access check for
// the group subject and for the CASE subject the case writes — and the
// AuxiliaryAcl attribute, the auxiliaryAcl state; the provider entries come
// from a Groupcast JoinGroup with UseAuxiliaryACL (the node's one
// provider). The cases that emit entries Groupcast cannot produce (five
// subjects in one entry) or need two fabrics' events run on the server with
// a free source (export_test.go RegisterAuxiliaryProvider).
//
// Ported elsewhere: "installs provider entries together with the real
// entries" (groups:TestAuxiliaryACLEntries,
// cluster/core:TestGroupcastAuxiliaryACLParityMatterJS,
// endpoint:TestCheckACLWithTheAuxiliaryFeature), "does not install provider
// entries without the Auxiliary feature"
// (endpoint:TestCheckACLWithTheAuxiliaryFeature, the lister-less leg) and
// "rejects a written ACL entry that includes AuxiliaryType with FAILURE"
// (bridge:TestACLWriteWithAuxiliaryTypeOverTheWire).
//
// Missing (register): "ignores provider entries without AuxiliaryType" —
// AuxiliaryACLSource yields store.ACLEntry values, which carry no
// AuxiliaryType, and every entry it supplies is served as Groupcast, so
// there is no untyped provider entry to ignore. "keeps the applied real
// entries when auxiliary entries change during a controller ACL write" —
// the Go ACL write lands in the store at once, where matter.js applies the
// effective list when the interaction ends (AccessControlServer.ts:545-605,
// #handleInteractionEnd, #delayAclUpdateFor). "applies an overridden
// extension access check to a fabric commissioned after startup" — the Go
// server has no overridable extensionEntryAccessCheck
// (AccessControlServer.ts:441-450).
package core_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	// auxGroup is auxEntry's default group, 0x0101 (AccessControlAuxiliaryTest.ts:37).
	auxGroup uint16 = 0x0101
	// auxViewSubject is the CASE subject of the cases' View entry (:150).
	auxViewSubject uint64 = 0x1234
	// auxOnOff is the cluster an access check of a group asks for.
	auxOnOff uint32 = 0x0006
)

// auxSource is a free provider of auxiliary entries — the test's
// ObservableValue (AccessControlAuxiliaryTest.ts:32-34).
type auxSource struct {
	mu      sync.Mutex
	entries []mstore.ACLEntry
	fabrics []uint8
	notify  []func(context.Context, uint8)
}

func (s *auxSource) AuxiliaryACL(_ context.Context, fabricIndex uint8) ([]mstore.ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []mstore.ACLEntry
	for _, e := range s.entries {
		if fabricIndex == 0 || e.FabricIndex == fabricIndex {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *auxSource) Fabrics(context.Context) ([]uint8, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.fabrics), nil
}

func (s *auxSource) OnGroupcastChanged(fn func(context.Context, uint8)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = append(s.notify, fn)
}

// emit is provider.emit(entries).
func (s *auxSource) emit(entries ...mstore.ACLEntry) {
	s.mu.Lock()
	s.entries = entries
	fns := slices.Clone(s.notify)
	s.mu.Unlock()
	for _, fn := range fns {
		fn(context.Background(), 0)
	}
}

// auxEntry is the test's auxEntry (:36-45): Operate for the groups on the
// endpoints.
func auxEntry(fabric uint8, endpoints []uint16, groups ...uint64) mstore.ACLEntry {
	if len(groups) == 0 {
		groups = []uint64{uint64(auxGroup)}
	}
	e := mstore.ACLEntry{FabricIndex: fabric, Privilege: mstore.PrivilegeOperate, AuthMode: mstore.AuthModeGroup, Subjects: groups}
	for _, ep := range endpoints {
		e.Targets = append(e.Targets, mstore.ACLTarget{Endpoint: &ep})
	}
	return e
}

// TestAccessControlAuxiliaryEventsPerChangedFabric ports "reports
// AuxiliaryAccessUpdated only for fabrics whose entries changed"
// (AccessControlAuxiliaryTest.ts:125-142): entries for two fabrics report
// both, a change of the second fabric's entries reports it alone
// (AccessControlServer.ts #syncAuxAcl :505-543).
func TestAccessControlAuxiliaryEventsPerChangedFabric(t *testing.T) {
	t.Parallel()
	acl, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	emitter := &gcRecordingEmitter{}
	acl.SetMatterEventEmitter(emitter)
	src := &auxSource{fabrics: []uint8{1, 2}}
	acl.RegisterAuxiliaryProvider(context.Background(), src)

	src.emit(auxEntry(1, []uint16{1}), auxEntry(2, []uint16{1}))
	src.emit(auxEntry(1, []uint16{1}), auxEntry(2, []uint16{2}))

	var updated []uint8
	for _, e := range emitter.take() {
		if ev, ok := e.data.(core.AuxiliaryAccessUpdatedEvent); ok && e.cluster == 0x001F && e.event == 0x0003 {
			updated = append(updated, ev.FabricIndex)
		}
	}
	if !slices.Equal(updated, []uint8{1, 2, 2}) {
		t.Errorf("AuxiliaryAccessUpdated fabrics = %v, want [1 2 2]", updated)
	}
}

// TestAccessControlAuxiliarySplitsProviderEntries ports "splits provider
// entries to the per-entry subject and target limits"
// (AccessControlAuxiliaryTest.ts:196-214): six endpoints and five groups
// become four entries of at most four subjects and four targets, subjects
// outer, targets inner (AccessControlServer.ts #auxiliaryAclFor :666-690).
// The Go access check evaluates the provider's entry unsplit, which grants
// what the split entries grant; the split shows in the attribute, matter.js's
// auxiliaryAcl state.
func TestAccessControlAuxiliarySplitsProviderEntries(t *testing.T) {
	t.Parallel()
	acl, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	src := &auxSource{fabrics: []uint8{1}}
	acl.RegisterAuxiliaryProvider(context.Background(), src)

	src.emit(auxEntry(1, []uint16{1, 2, 3, 4, 5, 6}, 1, 2, 3, 4, 5))

	want := []mstore.ACLEntry{
		auxEntry(1, []uint16{1, 2, 3, 4}, 1, 2, 3, 4),
		auxEntry(1, []uint16{5, 6}, 1, 2, 3, 4),
		auxEntry(1, []uint16{1, 2, 3, 4}, 5),
		auxEntry(1, []uint16{5, 6}, 5),
	}
	v, ok := acl.MatterRead(0x0007)
	if !ok {
		t.Fatal("AuxiliaryAcl unreadable")
	}
	got := v.([]core.AccessControlAuxiliaryEntryStruct)
	if len(got) != len(want) {
		t.Fatalf("AuxiliaryAcl = %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		var eps []uint16
		for _, tg := range g.Entry.Targets {
			eps = append(eps, *tg.Endpoint)
		}
		var wantEps []uint16
		for _, tg := range w.Targets {
			wantEps = append(wantEps, *tg.Endpoint)
		}
		if !slices.Equal(g.Entry.Subjects, w.Subjects) || !slices.Equal(eps, wantEps) ||
			g.Entry.Privilege != uint8(w.Privilege) || g.Entry.AuthMode != uint8(w.AuthMode) ||
			g.Entry.FabricIndex != 1 || g.AuxiliaryType != core.AccessControlAuxiliaryTypeGroupcast {
			t.Errorf("AuxiliaryAcl[%d] = subjects %v targets %v %+v, want subjects %v targets %v", i, g.Entry.Subjects, eps, g, w.Subjects, wantEps)
		}
	}
}

// auxDevice is a commissioned fabrictest pair — matter.js's
// createOnline(AuxiliaryRoot) plus addFabric, or addCommissionedPair for
// the controller cases. The Go node serves AccessControl with Extension and
// Auxiliary (the root mounts Groupcast).
func auxDevice(t *testing.T) *fabrictest.Pair {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	return site.CommissionedPair(fabrictest.PairOptions{})
}

// auxFabric is the device's one fabric index.
func auxFabric(t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	fabrics, err := p.Device.Node().FabricInfos(context.Background())
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	return fabrics[0].FabricIndex
}

// joinAuxiliary is the provider's emit on the node: a Groupcast JoinGroup
// of auxGroup on endpoint 1 with UseAuxiliaryACL, which yields
// auxEntry(fabric, [1]).
func joinAuxiliary(t *testing.T, p *fabrictest.Pair, fabric uint8) {
	t.Helper()
	err := p.Device.Act(func(n *node.Node) error {
		_, err := n.Root().Groupcast.MatterInvoke(as(fabric, 5), 0x00, core.JoinGroupRequest{
			GroupID: auxGroup, Endpoints: []uint16{1}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true),
		})
		return err
	})
	if err != nil {
		t.Fatalf("JoinGroup: %v", err)
	}
}

// auxAccess is the device's access check of the installed list: whether a
// group message of auxGroup may Operate endpoint ep.
func auxAccess(t *testing.T, p *fabrictest.Pair, fabric uint8, ep uint16) bool {
	t.Helper()
	checker, ok := p.Device.Bridge().Dispatcher().(im.ACLChecker)
	if !ok {
		t.Fatal("the dispatcher checks no ACL")
	}
	ctx := im.WithGroupSubject(context.Background(), im.GroupSubject{GroupID: auxGroup, HasValidMapping: true, Endpoints: []uint16{ep}})
	return checker.CheckACL(ctx, fabric, 0, nil, ep, auxOnOff, 3) == im.StatusSuccess
}

// viewAccess is the device's access check for the cases' CASE subject: View
// on the root.
func viewAccess(t *testing.T, p *fabrictest.Pair, fabric uint8) bool {
	t.Helper()
	checker := p.Device.Bridge().Dispatcher().(im.ACLChecker)
	return checker.CheckACL(context.Background(), fabric, auxViewSubject, nil, 0, 0x0028, 1) == im.StatusSuccess
}

// realACL is node.stateOf(AccessControlServer).acl of the fabric.
func realACL(t *testing.T, p *fabrictest.Pair, fabric uint8) []core.AccessControlEntryStruct {
	t.Helper()
	var out []core.AccessControlEntryStruct
	if err := p.Device.Act(func(n *node.Node) error {
		v, ok := n.Root().AccessControl.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, fabric), 0x0000)
		if !ok {
			return errors.New("ACL unreadable")
		}
		out = v.([]core.AccessControlEntryStruct)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// auxiliaryACL is node.stateOf(AccessControlServer).auxiliaryAcl.
func auxiliaryACL(t *testing.T, p *fabrictest.Pair) []core.AccessControlAuxiliaryEntryStruct {
	t.Helper()
	var out []core.AccessControlAuxiliaryEntryStruct
	if err := p.Device.Act(func(n *node.Node) error {
		v, ok := n.Root().AccessControl.MatterRead(0x0007)
		if !ok {
			return errors.New("AuxiliaryAcl unreadable")
		}
		out = v.([]core.AccessControlAuxiliaryEntryStruct)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// isAuxEntry reports whether e is auxEntry(fabric, [1]).
func isAuxEntry(e core.AccessControlAuxiliaryEntryStruct, fabric uint8) bool {
	return e.AuxiliaryType == core.AccessControlAuxiliaryTypeGroupcast && e.Entry.FabricIndex == fabric &&
		e.Entry.Privilege == 3 && e.Entry.AuthMode == 3 && slices.Equal(e.Entry.Subjects, []uint64{uint64(auxGroup)}) &&
		len(e.Entry.Targets) == 1 && e.Entry.Targets[0].Endpoint != nil && *e.Entry.Targets[0].Endpoint == 1 &&
		e.Entry.Targets[0].Cluster == nil && e.Entry.Targets[0].DeviceType == nil
}

// viewEntry is the cases' View entry for the CASE subject 0x1234 (:146-152).
func viewEntry(fabric uint8) core.AccessControlEntryStruct {
	return core.AccessControlEntryStruct{Privilege: 1, AuthMode: 2, Subjects: []uint64{auxViewSubject}, FabricIndex: fabric}
}

// TestAccessControlAuxiliaryRestart ports "installs unchanged provider
// entries after a restart" (AccessControlAuxiliaryTest.ts:99-123): the
// node restarted on its store with the same provider entries installs them
// — the group keeps its access and the attribute its entry
// (AccessControlServer.ts :518-521, "unchanged does not mean installed").
func TestAccessControlAuxiliaryRestart(t *testing.T) {
	t.Parallel()
	p := auxDevice(t)
	fabric := auxFabric(t, p)
	joinAuxiliary(t, p, fabric)
	if aux := auxiliaryACL(t, p); len(aux) != 1 || !isAuxEntry(aux[0], fabric) {
		t.Fatalf("auxiliaryAcl before the restart = %+v", aux)
	}

	if err := p.Device.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !auxAccess(t, p, fabric, 1) {
		t.Error("after the restart the group lost the access of its provider entry")
	}
	if auxAccess(t, p, fabric, 2) {
		t.Error("the group reaches an endpoint no entry names")
	}
	if aux := auxiliaryACL(t, p); len(aux) != 1 || !isAuxEntry(aux[0], fabric) {
		t.Errorf("auxiliaryAcl after the restart = %+v, want the one entry", aux)
	}
}

// TestAccessControlAuxiliaryLocalWrite ports "keeps auxiliary entries when
// the ACL is written locally" (AccessControlAuxiliaryTest.ts:144-166) and
// "rejects a local ACL write with an entry that includes AuxiliaryType
// with FAILURE" (:168-194). A local write is the server's MatterWrite
// without an IM request behind it — matter.js setStateOf; the refusal is
// the IM status Failure (StatusResponseError Status.Failure,
// AccessControlServer.ts #validateAccessControlListChanges).
func TestAccessControlAuxiliaryLocalWrite(t *testing.T) {
	t.Parallel()
	t.Run("keeps auxiliary entries when the ACL is written locally", func(t *testing.T) {
		t.Parallel()
		p := auxDevice(t)
		fabric := auxFabric(t, p)
		joinAuxiliary(t, p, fabric)

		acl := append(realACL(t, p, fabric), viewEntry(fabric))
		if err := p.Device.Act(func(n *node.Node) error {
			return n.Root().AccessControl.MatterWrite(context.Background(), 0x0000, acl)
		}); err != nil {
			t.Fatal(err)
		}

		if got := realACL(t, p, fabric); len(got) != len(acl) || !slices.Equal(got[len(got)-1].Subjects, []uint64{auxViewSubject}) {
			t.Errorf("acl = %+v, want the written list", got)
		}
		if !viewAccess(t, p, fabric) {
			t.Error("the written View entry is not in effect")
		}
		if !auxAccess(t, p, fabric, 1) {
			t.Error("the local write dropped the provider entry's access")
		}
		if aux := auxiliaryACL(t, p); len(aux) != 1 || !isAuxEntry(aux[0], fabric) {
			t.Errorf("auxiliaryAcl = %+v", aux)
		}
	})
	t.Run("rejects a local ACL write with an entry that includes AuxiliaryType with FAILURE", func(t *testing.T) {
		t.Parallel()
		p := auxDevice(t)
		fabric := auxFabric(t, p)
		before := realACL(t, p, fabric)

		entry := viewEntry(fabric)
		entry.AuxiliaryType = u8p(core.AccessControlAuxiliaryTypeGroupcast)
		err := p.Device.Act(func(n *node.Node) error {
			return n.Root().AccessControl.MatterWrite(context.Background(), 0x0000, append(slices.Clone(before), entry))
		})

		if statusOf(err) != im.StatusFailure {
			t.Errorf("local write with AuxiliaryType: %v, want Failure", err)
		}
		if got := realACL(t, p, fabric); len(got) != len(before) {
			t.Errorf("acl = %+v, want unchanged %+v", got, before)
		}
	})
}

// aclList encodes an ACL list as a controller writes it: the entries'
// fields 1-5, the fabric index left to the device (fabric-scoped).
type aclList []core.AccessControlEntryStruct

func (l aclList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, e := range l {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), uint64(e.Privilege))
		enc.PutUint(tlv.ContextTag(2), uint64(e.AuthMode))
		if e.Subjects == nil {
			enc.PutNull(tlv.ContextTag(3))
		} else {
			enc.StartArray(tlv.ContextTag(3))
			for _, s := range e.Subjects {
				enc.PutUint(tlv.AnonymousTag(), s)
			}
			_ = enc.EndContainer()
		}
		if e.Targets == nil {
			enc.PutNull(tlv.ContextTag(4))
		} else {
			enc.StartArray(tlv.ContextTag(4))
			for _, tg := range e.Targets {
				enc.StartStruct(tlv.AnonymousTag())
				putNullableUint(enc, 0, tg.Cluster)
				putNullableUint(enc, 1, tg.Endpoint)
				putNullableUint(enc, 2, tg.DeviceType)
				_ = enc.EndContainer()
			}
			_ = enc.EndContainer()
		}
		if e.AuxiliaryType != nil {
			enc.PutUint(tlv.ContextTag(5), uint64(*e.AuxiliaryType))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

func putNullableUint[T uint16 | uint32](enc *tlv.Encoder, tag uint8, v *T) {
	if v == nil {
		enc.PutNull(tlv.ContextTag(tag))
		return
	}
	enc.PutUint(tlv.ContextTag(tag), uint64(*v))
}

// TestAccessControlAuxiliaryControllerWrite ports "keeps auxiliary entries
// across a controller ACL write" (AccessControlAuxiliaryTest.ts:271-293):
// the controller appends the View entry over the wire; the installed list
// holds the two real entries and the provider entry.
func TestAccessControlAuxiliaryControllerWrite(t *testing.T) {
	t.Parallel()
	p := auxDevice(t)
	fabric := auxFabric(t, p)
	joinAuxiliary(t, p, fabric)

	acl := append(realACL(t, p, fabric), viewEntry(fabric))
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x001F, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	if err := fabrictest.Write(context.Background(), p, path, aclList(acl)); err != nil {
		var se *interaction.StatusError
		errors.As(err, &se)
		t.Fatalf("controller ACL write: %v (%+v)", err, se)
	}

	if got := realACL(t, p, fabric); len(got) != 2 {
		t.Errorf("real entries = %+v, want 2", got)
	}
	if !viewAccess(t, p, fabric) {
		t.Error("the written View entry is not in effect")
	}
	if !auxAccess(t, p, fabric, 1) {
		t.Error("the controller write dropped the provider entry's access")
	}
	if aux := auxiliaryACL(t, p); len(aux) != 1 || !isAuxEntry(aux[0], fabric) {
		t.Errorf("auxiliaryAcl = %+v", aux)
	}
}
