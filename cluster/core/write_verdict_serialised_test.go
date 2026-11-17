// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// gateKey marks the context of the write whose first store read waits.
type gateKey struct{}

// storeGate holds the first store read of the write whose context carries
// gateKey until release is closed; listed is closed when that read has
// taken its snapshot.
type storeGate struct {
	once    sync.Once
	listed  chan struct{}
	release chan struct{}
}

func newStoreGate() *storeGate {
	return &storeGate{listed: make(chan struct{}), release: make(chan struct{})}
}

// hold blocks the gated write's read, once, after its snapshot.
func (g *storeGate) hold(ctx context.Context) {
	if ctx.Value(gateKey{}) == nil {
		return
	}
	g.once.Do(func() {
		close(g.listed)
		<-g.release
	})
}

// gatedACLStore is the fakeStore's ACL whose gated ListACL waits.
type gatedACLStore struct {
	*fakeStore
	gate *storeGate
}

func (s gatedACLStore) ListACL(ctx context.Context, fabric uint8) ([]mstore.ACLEntry, error) {
	out, err := s.fakeStore.ListACL(ctx, fabric)
	s.gate.hold(ctx)
	return out, err
}

// gatedGroupStore is the fakeStore's GroupKeyMap whose gated
// ListGroupKeyMappings waits.
type gatedGroupStore struct {
	*fakeStore
	gate *storeGate
}

func (s gatedGroupStore) ListGroupKeyMappings(ctx context.Context, fabric uint8) ([]mstore.GroupKeyMapping, error) {
	out, err := s.fakeStore.ListGroupKeyMappings(ctx, fabric)
	s.gate.hold(ctx)
	return out, err
}

// verdict is one write's answer.
type verdict struct {
	changed bool
	err     error
}

// interleave runs held — whose first store read waits at the gate — and
// then other, each in its own goroutine, and lets held go on once other
// has finished or the bound below has passed (a write serialised behind
// held cannot finish before held does).
func interleave(t *testing.T, gate *storeGate, held, other func() (bool, error)) (heldV, otherV verdict) {
	t.Helper()
	heldDone := make(chan verdict, 1)
	otherDone := make(chan verdict, 1)
	go func() {
		c, err := held()
		heldDone <- verdict{c, err}
	}()
	<-gate.listed
	go func() {
		c, err := other()
		otherDone <- verdict{c, err}
	}()
	// How long other is given to reach the store while held sits between
	// its read and its store; a test-only bound, no protocol number.
	wait, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	select {
	case otherV = <-otherDone:
		close(gate.release)
		return <-heldDone, otherV
	case <-wait.Done():
	}
	close(gate.release)
	return <-heldDone, <-otherDone
}

// TestACLWriteVerdictIsTheWritesOwnUnderAConcurrentWrite pins that a
// write of the ACL the fabric holds stores nothing and answers unchanged,
// and that a concurrent write of another list is the one change the
// DataVersion records: whatever the store holds afterwards is what the
// last advance of the version was taken for. The read, the comparison and
// the store of one write are one step, as matter.js holds the datasource
// across the change and the commit's comparison
// (packages/node/src/behavior/state/managed/Datasource.ts:1345-1350,
// :1446-1457, :1476-1492 at db021da0, #noteIfChanged).
func TestACLWriteVerdictIsTheWritesOwnUnderAConcurrentWrite(t *testing.T) {
	t.Parallel()
	a := []core.AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{112233}, FabricIndex: 1}}
	b := []core.AccessControlEntryStruct{
		{Privilege: 5, AuthMode: 2, Subjects: []uint64{112233}, FabricIndex: 1},
		{Privilege: 3, AuthMode: 2, Subjects: []uint64{445566}, FabricIndex: 1},
	}
	fs := newFakeStore()
	if _, err := fs.AddFabric(context.Background(), mstore.FabricRecord{FabricID: 1}); err != nil {
		t.Fatal(err)
	}
	gate := newStoreGate()
	ac, err := core.NewAccessControl(gatedACLStore{fakeStore: fs, gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	if _, err := ac.MatterWriteChanged(ctx, 0, a); err != nil {
		t.Fatal(err)
	}
	v0 := ac.MatterDataVersion()

	heldV, otherV := interleave(t, gate,
		func() (bool, error) { return ac.MatterWriteChanged(context.WithValue(ctx, gateKey{}, true), 0, a) },
		func() (bool, error) { return ac.MatterWriteChanged(ctx, 0, b) })
	if heldV.err != nil || otherV.err != nil {
		t.Fatalf("writes: held %v, other %v", heldV.err, otherV.err)
	}
	final, _ := fs.ListACL(context.Background(), 1)
	v := ac.MatterDataVersion()
	if heldV.changed {
		t.Errorf("the write of the held ACL answered changed")
	}
	if !otherV.changed {
		t.Errorf("the write of another ACL answered unchanged")
	}
	if len(final) != len(b) || v != v0+1 {
		t.Errorf("after the two writes the store holds %d entries under DataVersion %d (start %d); want the %d entries of the one changing write under one advance",
			len(final), v, v0, len(b))
	}
}

// TestGroupKeyMapWritesAreSerialised pins that two concurrent writes of a
// fabric's GroupKeyMap leave one of the written lists, never a mixture of
// both, and that a write of the held list stores nothing and answers
// unchanged while a concurrent write of another list is the change the
// DataVersion records — the read, the comparison and the store of one
// write are one step (Datasource.ts:1446-1457, :1476-1492 at db021da0).
func TestGroupKeyMapWritesAreSerialised(t *testing.T) {
	t.Parallel()
	held := []core.GroupKeyMapStruct{{GroupID: 0x0100, GroupKeySetID: 1}}
	for _, tc := range []struct {
		name        string
		first       []core.GroupKeyMapStruct
		heldChanged bool
	}{
		{"the held list", held, false},
		{"another list", []core.GroupKeyMapStruct{{GroupID: 0x0200, GroupKeySetID: 2}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := newFakeStore()
			gate := newStoreGate()
			gkm, err := core.NewGroupKeyManagement(gatedGroupStore{fakeStore: fs, gate: gate}, core.GroupKeyMgmtConfig{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := im.WithFabricFilter(context.Background(), true, 1)
			if _, err := gkm.MatterWriteChanged(ctx, 0, held); err != nil {
				t.Fatal(err)
			}
			v0 := gkm.MatterDataVersion()
			last := []core.GroupKeyMapStruct{{GroupID: 0x0300, GroupKeySetID: 3}}

			heldV, otherV := interleave(t, gate,
				func() (bool, error) {
					return gkm.MatterWriteChanged(context.WithValue(ctx, gateKey{}, true), 0, tc.first)
				},
				func() (bool, error) { return gkm.MatterWriteChanged(ctx, 0, last) })
			if heldV.err != nil || otherV.err != nil {
				t.Fatalf("writes: held %v, other %v", heldV.err, otherV.err)
			}
			if heldV.changed != tc.heldChanged || !otherV.changed {
				t.Errorf("verdicts: gated write %v (want %v), other write %v (want true)", heldV.changed, tc.heldChanged, otherV.changed)
			}
			got, _ := fs.ListGroupKeyMappings(context.Background(), 1)
			groups := make([]uint16, 0, len(got))
			for _, m := range got {
				groups = append(groups, m.GroupID)
			}
			slices.Sort(groups)
			if !slices.Equal(groups, []uint16{0x0300}) {
				t.Errorf("the store holds groups %#04x after the two writes; want the last write's [0x0300] alone", groups)
			}
			advances := uint32(1)
			if tc.heldChanged {
				advances = 2
			}
			if v := gkm.MatterDataVersion(); v != v0+advances {
				t.Errorf("DataVersion %d → %d; want %d advances", v0, v, advances)
			}
		})
	}
}

// gatedOtaStore is a DefaultOTAProviders store whose gated
// ReplaceDefaultOTAProviders waits; replaces counts the calls.
type gatedOtaStore struct {
	mu       sync.Mutex
	rows     map[uint8][]mstore.OTAProviderLocation
	replaces int
	gate     *storeGate
}

func (s *gatedOtaStore) ListDefaultOTAProviders(context.Context) ([]mstore.OTAProviderLocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []mstore.OTAProviderLocation
	for f := uint8(1); f < 255; f++ {
		out = append(out, s.rows[f]...)
	}
	return out, nil
}

func (s *gatedOtaStore) ReplaceDefaultOTAProviders(ctx context.Context, f uint8, e []mstore.OTAProviderLocation) error {
	s.mu.Lock()
	s.rows[f] = slices.Clone(e)
	s.replaces++
	s.mu.Unlock()
	s.gate.hold(ctx)
	return nil
}

func (s *gatedOtaStore) LoadOTARequestorState(context.Context) (state []byte, ok bool, err error) {
	return nil, false, nil
}

func (s *gatedOtaStore) SaveOTARequestorState(context.Context, []byte) error { return nil }

func (s *gatedOtaStore) replaced() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaces
}

func newGatedOtaRequestor(t *testing.T) (*core.OTASoftwareUpdateRequestor, *gatedOtaStore) {
	t.Helper()
	st := &gatedOtaStore{rows: map[uint8][]mstore.OTAProviderLocation{}, gate: newStoreGate()}
	r, err := core.NewOTASoftwareUpdateRequestor(core.OTARequestorConfig{
		NodeInformation: func() core.OtaNodeInformation { return core.OtaNodeInformation{VendorID: 0xFFF1, ProductID: 0x8001} },
		Store:           st,
		DownloadDir:     t.TempDir(),
		Logger:          slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	return r, st
}

// providers is a DefaultOTAProviders list of one provider.
func providers(node uint64) spec.List[otadef.ProviderLocation] {
	return spec.List[otadef.ProviderLocation]{{ProviderNodeId: node}}
}

// TestOTADefaultProvidersWriteOfTheHeldEntriesStoresNothing pins that a
// fabric's write of the DefaultOTAProviders entries it holds stores
// nothing and answers unchanged, wherever its entries sit in the list:
// matter.js reacts to defaultOtaProviders$Changed
// (OtaSoftwareUpdateRequestorServer.ts:188), which a commit emits only for
// a changed value (Datasource.ts:1476-1492 at db021da0, #noteIfChanged).
func TestOTADefaultProvidersWriteOfTheHeldEntriesStoresNothing(t *testing.T) {
	t.Parallel()
	r, st := newGatedOtaRequestor(t)
	attr := otadef.AttrDefaultOtaProviders
	for f, node := range map[uint8]uint64{1: 0xAA, 2: 0xBB} {
		if changed, err := r.MatterWriteChanged(im.WithFabricFilter(context.Background(), true, f), attr, providers(node)); err != nil || !changed {
			t.Fatalf("fabric %d's first write: changed %v, %v", f, changed, err)
		}
	}
	v0, calls := r.MatterDataVersion(), st.replaced()
	for f, node := range map[uint8]uint64{1: 0xAA, 2: 0xBB} {
		changed, err := r.MatterWriteChanged(im.WithFabricFilter(context.Background(), true, f), attr, providers(node))
		if err != nil || changed {
			t.Errorf("fabric %d's held entries again: changed %v, %v; want unchanged", f, changed, err)
		}
	}
	if r.MatterDataVersion() != v0 || st.replaced() != calls {
		t.Errorf("the held entries again: DataVersion %d → %d, store calls %d → %d; want neither moved",
			v0, r.MatterDataVersion(), calls, st.replaced())
	}
}

// TestOTADefaultProvidersWritesAreSerialised pins that two fabrics'
// concurrent DefaultOTAProviders writes both stand: each write's entries
// replace its own fabric's in the list as it is when the write commits,
// never in a copy taken before another write committed.
func TestOTADefaultProvidersWritesAreSerialised(t *testing.T) {
	t.Parallel()
	r, st := newGatedOtaRequestor(t)
	attr := otadef.AttrDefaultOtaProviders
	ctx1 := im.WithFabricFilter(context.Background(), true, 1)
	ctx2 := im.WithFabricFilter(context.Background(), true, 2)
	heldV, otherV := interleave(t, st.gate,
		func() (bool, error) {
			return r.MatterWriteChanged(context.WithValue(ctx1, gateKey{}, true), attr, providers(0xAA))
		},
		func() (bool, error) { return r.MatterWriteChanged(ctx2, attr, providers(0xBB)) })
	if heldV.err != nil || otherV.err != nil || !heldV.changed || !otherV.changed {
		t.Fatalf("writes: fabric 1 %+v, fabric 2 %+v; want both changed", heldV, otherV)
	}
	v, _ := r.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 1), attr)
	got, _ := v.(spec.List[otadef.ProviderLocation])
	nodes := make([]uint64, 0, len(got))
	for _, p := range got {
		nodes = append(nodes, p.ProviderNodeId)
	}
	slices.Sort(nodes)
	if !slices.Equal(nodes, []uint64{0xAA, 0xBB}) {
		t.Errorf("DefaultOTAProviders after both writes holds providers %#x; want both fabrics' [0xaa 0xbb]", nodes)
	}
}

// TestBridgedNodeLabelHookHearsTheStoredChangesInOrder pins that
// OnNodeLabelWrite hears a controller's NodeLabel write only when the label
// differs from the one held (matter.js observes no equal field,
// Datasource.ts:1406-1407 and :1479 at db021da0), and hears the labels in
// the order the server stores them: a write that stores after another
// reaches the hook after it, so the host keeps the label the server holds.
func TestBridgedNodeLabelHookHearsTheStoredChangesInOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	var heard []string
	gate := newStoreGate()
	b, err := core.NewBridgedDeviceBasicInformation(core.BridgedConfig{UniqueID: "u-1", NodeLabel: "lamp", OnNodeLabelWrite: func(label string) {
		if label == "desk" {
			gate.hold(context.WithValue(ctx, gateKey{}, true))
		}
		mu.Lock()
		heard = append(heard, label)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	const nodeLabel = 0x0005
	if changed, err := b.MatterWriteChanged(ctx, nodeLabel, "lamp"); changed || err != nil || len(heard) != 0 {
		t.Errorf("the held label: changed %v, %v, hook heard %q; want unchanged and nothing heard", changed, err, heard)
	}
	interleave(t, gate,
		func() (bool, error) { return b.MatterWriteChanged(ctx, nodeLabel, "desk") },
		func() (bool, error) { return b.MatterWriteChanged(ctx, nodeLabel, "hall") },
	)
	held, _ := b.MatterRead(nodeLabel)
	if want := []string{"desk", "hall"}; !slices.Equal(heard, want) || held != "hall" {
		t.Errorf("hook heard %q, the server holds %v; want %q and the last one held", heard, held, want)
	}
}
