// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// A request's read of a fabric-scoped attribute that fails inside the
// server answers FAILURE; the server's request-free MatterRead — every
// fabric's entries, whole — never stands in for it. matter.js has no
// second read to fall back to: a read that throws ends the interaction in
// InteractionServerMessenger.handleRequest, whose catch answers
// Status.Failure for any error that is not a StatusResponseError
// (packages/protocol/src/interaction/InteractionMessenger.ts:425-459 at
// db021da0).

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

var errReadFault = errors.New("store read fault")

// faultingReadStore fails its failAt-th read call (counted over every
// method below since the last reset), once: one transient store error.
type faultingReadStore struct {
	*store.Store
	failAt int
	calls  int
}

func (s *faultingReadStore) fault() error {
	s.calls++
	if s.calls == s.failAt {
		return errReadFault
	}
	return nil
}

func (s *faultingReadStore) ListFabrics(ctx context.Context) ([]store.FabricRecord, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListFabrics(ctx)
}

func (s *faultingReadStore) GetFabric(ctx context.Context, fabric uint8) (store.FabricRecord, error) {
	if err := s.fault(); err != nil {
		return store.FabricRecord{}, err
	}
	return s.Store.GetFabric(ctx, fabric)
}

func (s *faultingReadStore) GetIdentity(ctx context.Context, fabric uint8) (store.IdentityRecord, error) {
	if err := s.fault(); err != nil {
		return store.IdentityRecord{}, err
	}
	return s.Store.GetIdentity(ctx, fabric)
}

func (s *faultingReadStore) ListACL(ctx context.Context, fabric uint8) ([]store.ACLEntry, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListACL(ctx, fabric)
}

func (s *faultingReadStore) ListGroupKeySets(ctx context.Context, fabric uint8) ([]store.GroupKeySet, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListGroupKeySets(ctx, fabric)
}

func (s *faultingReadStore) ListGroupKeyMappings(ctx context.Context, fabric uint8) ([]store.GroupKeyMapping, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListGroupKeyMappings(ctx, fabric)
}

func (s *faultingReadStore) ListGroupTable(ctx context.Context, fabric uint8) ([]store.GroupTableEntry, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListGroupTable(ctx, fabric)
}

func (s *faultingReadStore) ListGroupcastGroups(ctx context.Context, fabric uint8) ([]store.GroupcastGroup, error) {
	if err := s.fault(); err != nil {
		return nil, err
	}
	return s.Store.ListGroupcastGroups(ctx, fabric)
}

// twoFabricStore is a store with two fabrics, each holding its operational
// identity, one ACL entry, one Extension entry, one GroupKeyMap entry and
// one group table entry.
func twoFabricStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "scoped.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	for _, id := range []uint64{0xA, 0xB} {
		root := make([]byte, 65)
		root[0] = 0x04
		root[1] = byte(id)
		f, err := st.AddFabric(ctx, store.FabricRecord{
			FabricID: id, NodeID: 0x0B0B, RootPublicKey: root, VendorID: 0xFFF1,
			CompressedID: [8]byte{byte(id)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertIdentity(ctx, store.IdentityRecord{
			FabricIndex: f, NOC: []byte{0x15, f, 0x18}, PrivateKey: make([]byte, 32), IPK: make([]byte, 16),
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceACL(ctx, f, []store.ACLEntry{{
			FabricIndex: f, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0xD0 + uint64(f)},
		}}); err != nil {
			t.Fatal(err)
		}
		// The setting AccessControl restores a fabric's Extension from
		// (aclExtensionSettingKey, cluster/core/access_control.go).
		if err := st.SetSetting(ctx, fmt.Sprintf("access_control.extension.%d", f), hex.EncodeToString([]byte{0x15, 0x18})); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: f, GroupKeySetID: 7, EpochKey0: make([]byte, 16), EpochStart0: 1}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: uint16(f), GroupKeySetID: 7}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: uint16(f), Endpoints: []uint16{1}}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// scopedReadDispatcher mounts AccessControl, GroupKeyManagement (with a
// group state not yet loaded) and OperationalCredentials over st on the
// root endpoint.
func scopedReadDispatcher(t *testing.T, st *faultingReadStore) *TopologyDispatcher {
	t.Helper()
	acl, err := mattercore.NewAccessControlWith(context.Background(), mattercore.AccessControlConfig{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := groups.NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := mattercore.NewGroupKeyManagement(st, mattercore.GroupKeyMgmtConfig{Groups: mgr})
	if err != nil {
		t.Fatal(err)
	}
	opc, err := mattercore.NewOperationalCredentials(st, mattercore.OpcredsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return NewTopologyDispatcher(&Topology{
		Endpoints: []*Endpoint{rootEndpointWith(acl, gkm, opc), {ID: 1, Aggregator: true, DeviceType: 0x000E}},
		VendorID:  0xFFF1, ProductID: 0x8000,
	})
}

// fabricsOfList lists the FabricIndex of every entry of a fabric-scoped
// list value.
func fabricsOfList(t *testing.T, v any) []uint8 {
	t.Helper()
	var out []uint8
	switch x := v.(type) {
	case []mattercore.AccessControlEntryStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []mattercore.AccessControlExtensionEntry:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []mattercore.GroupKeyMapStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []mattercore.GroupInfoMapStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []mattercore.FabricDescriptorStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []mattercore.NOCStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	default:
		t.Fatalf("value %T is not a fabric-scoped list this test knows", v)
	}
	return out
}

// TestFabricScopedReadFailureAnswersFailure fails, one at a time, every
// store call a request's read of Acl, Extension, GroupKeyMap, GroupTable,
// Fabrics and NOCs makes, fabric-filtered and not, from fabric 1: each
// answers FAILURE, and none answers the entries of the request-free read
// (review II, R2-P4-fr-sec-04: the NOCs read's GetIdentity of each fabric
// included).
func TestFabricScopedReadFailureAnswersFailure(t *testing.T) {
	t.Parallel()
	st := twoFabricStore(t)
	for _, tc := range []struct {
		name      string
		cluster   uint32
		attribute uint32
	}{
		{"Acl", 0x001F, 0x0000},
		{"Extension", 0x001F, 0x0001},
		{"GroupKeyMap", 0x003F, 0x0000},
		{"GroupTable", 0x003F, 0x0001},
		{"Fabrics", 0x003E, 0x0001},
		{"NOCs", 0x003E, 0x0000},
	} {
		for _, filtered := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s fabricFiltered=%v", tc.name, filtered), func(t *testing.T) {
				t.Parallel()
				path := im.ConcreteAttributePath{Endpoint: 0, Cluster: tc.cluster, Attribute: tc.attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
				ctx := im.WithFabricFilter(context.Background(), filtered, 1)
				read := func(failAt int) (im.ReadResult, int) {
					fs := &faultingReadStore{Store: st}
					d := scopedReadDispatcher(t, fs)
					fs.calls, fs.failAt = 0, failAt
					res := d.Read(ctx, path)
					if len(res) != 1 {
						t.Fatalf("read answered %d results, want 1", len(res))
					}
					return res[0], fs.calls
				}
				clean, calls := read(0)
				if clean.Status != im.StatusSuccess {
					t.Fatalf("read without a store fault: status %v, want SUCCESS", clean.Status)
				}
				want := fabricsOfList(t, clean.Value.Value)
				if filtered && (len(want) != 1 || want[0] != 1) {
					t.Fatalf("fabric-filtered read without a store fault: fabrics %v, want [1]", want)
				}
				for n := 1; n <= calls; n++ {
					res, _ := read(n)
					if res.Status != im.StatusFailure {
						var got any = res.Status
						if res.Status == im.StatusSuccess {
							got = fmt.Sprintf("SUCCESS with fabrics %v", fabricsOfList(t, res.Value.Value))
						}
						t.Errorf("store call %d of %d failed: answered %v, want FAILURE", n, calls, got)
					}
				}
			})
		}
	}
}

// hostScopedReader is a host's own FabricScopedReader on the
// OperationalCredentials cluster id: its filtered read answers (nil,
// false) for every attribute, and its MatterRead answers every one.
type hostScopedReader struct{}

func (hostScopedReader) MatterClusterID() uint32 { return 0x003E }

func (hostScopedReader) MatterRead(attrID uint32) (any, bool) {
	return []uint8{1, 2}, true
}

func (hostScopedReader) MatterReadFiltered(context.Context, uint32) (any, bool) {
	return nil, false
}

func (hostScopedReader) MatterWrite(context.Context, uint32, any) error { return nil }

func (hostScopedReader) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

func (hostScopedReader) MatterReportable() []uint32 { return nil }

// MatterAttributes lists Fabrics (0x0001, fabric-scoped) and
// SupportedFabrics (0x0002), not NOCs (0x0000, fabric-scoped).
func (hostScopedReader) MatterAttributes() []uint32 { return []uint32{0x0001, 0x0002} }

var (
	_ contract.FabricScopedReader     = hostScopedReader{}
	_ contract.ClusterAttributeLister = hostScopedReader{}
)

// TestFabricScopedReaderNotOkOnARequest holds the rule for every
// FabricScopedReader, a host's included: on a request, (nil, false) for a
// fabric-scoped attribute it serves answers FAILURE, for one it does not
// serve UNSUPPORTED_ATTRIBUTE, and for any other attribute — or with no
// request behind the read — MatterRead answers.
func TestFabricScopedReaderNotOkOnARequest(t *testing.T) {
	t.Parallel()
	d := NewTopologyDispatcher(&Topology{
		Endpoints: []*Endpoint{rootEndpointWith(hostScopedReader{}), {ID: 1, Aggregator: true, DeviceType: 0x000E}},
		VendorID:  0xFFF1, ProductID: 0x8000,
	})
	request := im.WithFabricFilter(context.Background(), false, 1)
	for _, tc := range []struct {
		name      string
		ctx       context.Context
		attribute uint32
		want      im.StatusCode
	}{
		{"served fabric-scoped attribute", request, 0x0001, im.StatusFailure},
		{"unserved fabric-scoped attribute", request, 0x0000, im.StatusUnsupportedAttribute},
		{"attribute that is not fabric-scoped", request, 0x0002, im.StatusSuccess},
		{"fabric-scoped attribute without a request", context.Background(), 0x0001, im.StatusSuccess},
	} {
		path := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x003E, Attribute: tc.attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		res := d.Read(tc.ctx, path)
		if len(res) != 1 || res[0].Status != tc.want {
			t.Errorf("%s: read answered %+v, want status %v", tc.name, res, tc.want)
		}
	}
}
