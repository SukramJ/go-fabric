// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	matterparity "github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/store"
)

// storingServer is a cluster server that refuses nothing: it lists the
// attributes it is given, reads every one of them, carries a DataVersion and
// stores whatever is written. A write to a read-only attribute that reaches
// it is a write the gate let through.
type storingServer struct {
	id      uint32
	attrs   []uint32
	version uint32
	written []uint32
}

func (s *storingServer) MatterClusterID() uint32 { return s.id }
func (s *storingServer) MatterRead(attr uint32) (any, bool) {
	if !slices.Contains(s.attrs, attr) {
		return nil, false
	}
	return uint8(0), true
}
func (s *storingServer) MatterReportable() []uint32 { return s.attrs }
func (s *storingServer) MatterAttributes() []uint32 { return s.attrs }
func (s *storingServer) MatterDataVersion() uint32  { return s.version }
func (s *storingServer) MatterWrite(_ context.Context, attr uint32, _ any) error {
	s.written = append(s.written, attr)
	return nil
}

func (s *storingServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

var (
	_ contract.ClusterServer          = (*storingServer)(nil)
	_ contract.ClusterAttributeLister = (*storingServer)(nil)
	_ contract.ClusterDataVersion     = (*storingServer)(nil)
)

// readOnlyRig mounts one storingServer on the root endpoint and a second
// one for the same cluster on a bare endpoint, behind an access-control
// list with one CASE entry of the given privilege (0 for none).
func readOnlyRig(clusterID uint32, attrs []uint32, priv store.Privilege) (d *TopologyDispatcher, root, other *storingServer) {
	root = &storingServer{id: clusterID, attrs: attrs, version: 7}
	other = &storingServer{id: clusterID, attrs: attrs, version: 7}
	bare := &Endpoint{ID: 5}
	bare.PublishClusterServers([]contract.ClusterServer{other})
	d = NewTopologyDispatcher(&Topology{
		Endpoints: []*Endpoint{rootEndpointWith(root), {ID: 1, Aggregator: true, DeviceType: 0x000E}, bare},
		VendorID:  0xFFF1, ProductID: 0x8000,
	})
	var entries []store.ACLEntry
	if priv != 0 {
		entries = append(entries, caseEntry(1, priv, nil))
	}
	d.SetACLLister(fakeACLLister{entries: entries})
	return d, root, other
}

// TestWriteToReadOnlyAttributeParity holds the write path's answer for an
// attribute that is not writable, in the references' order: the View pass,
// the existence statuses, UNSUPPORTED_WRITE, and only then the write
// privilege and the DataVersion. matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts at db021da0:
// :215 the View pass, :221-229 existence, :231-234 `!limits.writable` →
// UnsupportedWrite, :236-241 the write privilege, :244-251 timed and the
// accessing fabric, :254-257 the DataVersion; :331-333 the wildcard path
// skips the attribute without a status. chip src/app/WriteHandler.cpp at
// 6170af84: :777 the View pass, :782-788 existence, :791 no write privilege
// → UnsupportedWrite, :794 the write privilege, :801-802 timed, :808-812 the
// fabric-scoped list, :818-826 the DataVersion. So a subject that may view
// the cluster learns UNSUPPORTED_WRITE whatever else is wrong with its
// write, in every cluster — review II, W3-clugen-05.
func TestWriteToReadOnlyAttributeParity(t *testing.T) {
	t.Parallel()
	attributes := []struct {
		name          string
		cluster, attr uint32
	}{
		// In the hand-written table this gate had before.
		{"OnOff.OnOff", 0x0006, 0x0000},
		// Outside it.
		{"AirQuality.AirQuality", 0x005B, 0x0000},
		{"HepaFilterMonitoring.Condition", 0x0071, 0x0000},
		{"Chime.InstalledChimeSounds", 0x0556, 0x0000},
		// Inherited: the derived cluster states no access of its own, the
		// base's (BasicInformation, ModeBase) is the effective one.
		{"BridgedDeviceBasicInformation.VendorName", 0x0039, 0x0001},
		{"RvcRunMode.SupportedModes", 0x0054, 0x0000},
		// Global, on a cluster that was outside the table.
		{"AirQuality.ClusterRevision", 0x005B, 0xFFFD},
		{"AirQuality.AttributeList", 0x005B, 0xFFFB},
	}
	subjects := []struct {
		name string
		priv store.Privilege
		// concrete is the status of a concrete path.
		concrete im.StatusCode
	}{
		{"Administer", store.PrivilegeAdminister, im.StatusUnsupportedWrite},
		{"Operate", store.PrivilegeOperate, im.StatusUnsupportedWrite},
		{"View", store.PrivilegeView, im.StatusUnsupportedWrite},
		// Without View the first pass refuses, and nothing is disclosed.
		{"no privilege", 0, im.StatusUnsupportedAccess},
	}
	for _, a := range attributes {
		if writable, known := schema.AttributeWritable(a.cluster, a.attr); !known || writable {
			t.Errorf("%s: schema.AttributeWritable = (writable %v, known %v), want a read-only attribute", a.name, writable, known)
		}
		for _, s := range subjects {
			for _, stale := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stale=%v", a.name, s.name, stale), func(t *testing.T) {
					t.Parallel()
					d, root, other := readOnlyRig(a.cluster, []uint32{a.attr, 0x7777}, s.priv)
					w := im.AttributeWrite{Path: concreteAttrPath(0, a.cluster, a.attr), Value: im.AttributeValue{Value: uint8(1)}}
					if stale {
						w.DataVersion, w.HasDataVersion = 99, true
					}
					resp := im.HandleWriteRequest(privilegeEnforcementCtx(), d, im.WriteRequest{Writes: []im.AttributeWrite{w}})
					if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != s.concrete {
						t.Errorf("concrete write answered %+v, want one %v", resp.Responses, s.concrete)
					}

					// The wildcard-endpoint path skips the attribute on
					// every endpoint, without a status.
					wild := w
					wild.Path.HasEndpoint = false
					resp = im.HandleWriteRequest(privilegeEnforcementCtx(), d, im.WriteRequest{Writes: []im.AttributeWrite{wild}})
					if len(resp.Responses) != 0 {
						t.Errorf("wildcard-endpoint write answered %+v, want no status", resp.Responses)
					}
					if len(root.written)+len(other.written) != 0 {
						t.Errorf("the write was stored: root %v, endpoint 5 %v", root.written, other.written)
					}
				})
			}
		}
	}
}

// TestWriteToWritableAttributeKeepsItsGates is the other side: a writable
// attribute of a cluster the hand-written table did not know still answers
// the write privilege and the DataVersion, and an administrator's write is
// stored. HepaFilterMonitoring.LastChangedTime (0x0071:0x0004) is "RW VO".
func TestWriteToWritableAttributeKeepsItsGates(t *testing.T) {
	t.Parallel()
	const cluster, attr uint32 = 0x0071, 0x0004
	if writable, _ := schema.AttributeWritable(cluster, attr); !writable {
		t.Fatalf("schema.AttributeWritable(0x%04X, 0x%04X) is read-only, want writable", cluster, attr)
	}
	cases := []struct {
		name   string
		priv   store.Privilege
		stale  bool
		want   im.StatusCode
		stored bool
	}{
		{"an administrator's write is stored", store.PrivilegeAdminister, false, im.StatusSuccess, true},
		{"an Operate subject's write is stored", store.PrivilegeOperate, false, im.StatusSuccess, true},
		{"a stale DataVersion is a mismatch", store.PrivilegeAdminister, true, im.StatusDataVersionMismatch, false},
		{"a View subject lacks the write privilege", store.PrivilegeView, false, im.StatusUnsupportedAccess, false},
		{"the write privilege is judged before the DataVersion", store.PrivilegeView, true, im.StatusUnsupportedAccess, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, root, _ := readOnlyRig(cluster, []uint32{attr}, tc.priv)
			w := im.AttributeWrite{Path: concreteAttrPath(0, cluster, attr), Value: im.AttributeValue{Value: uint32(1)}}
			if tc.stale {
				w.DataVersion, w.HasDataVersion = 99, true
			}
			resp := im.HandleWriteRequest(privilegeEnforcementCtx(), d, im.WriteRequest{Writes: []im.AttributeWrite{w}})
			if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != tc.want {
				t.Errorf("answered %+v, want one %v", resp.Responses, tc.want)
			}
			if stored := len(root.written) == 1; stored != tc.stored {
				t.Errorf("stored = %v (writes %v), want %v", stored, root.written, tc.stored)
			}
		})
	}
}

// TestWriteToAnAbsentAttributeStaysUnsupportedAttribute holds the status
// ahead of UNSUPPORTED_WRITE: an attribute the schema marks read-only but
// the server does not list answers UNSUPPORTED_ATTRIBUTE to a View subject
// (AttributeWriteResponse.ts:227-229 at db021da0, before :231-234).
func TestWriteToAnAbsentAttributeStaysUnsupportedAttribute(t *testing.T) {
	t.Parallel()
	d, root, _ := readOnlyRig(0x005B, []uint32{0x7777}, store.PrivilegeView)
	w := im.AttributeWrite{Path: concreteAttrPath(0, 0x005B, 0x0000), Value: im.AttributeValue{Value: uint8(1)}, DataVersion: 99, HasDataVersion: true}
	resp := im.HandleWriteRequest(privilegeEnforcementCtx(), d, im.WriteRequest{Writes: []im.AttributeWrite{w}})
	if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusUnsupportedAttribute {
		t.Errorf("answered %+v, want one UNSUPPORTED_ATTRIBUTE", resp.Responses)
	}
	if len(root.written) != 0 {
		t.Errorf("the write was stored: %v", root.written)
	}
}

// TestEveryReadOnlyAttributeOfTheSnapshotIsRefused walks the snapshot the
// table is generated from: every attribute whose effective access is not
// writable answers UNSUPPORTED_WRITE to an Operate subject with a stale
// DataVersion and is not stored, on a server that would have stored it.
func TestEveryReadOnlyAttributeOfTheSnapshotIsRefused(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Name       string `json:"name"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Effective struct {
					Access struct {
						RW string `json:"rw"`
					} `json:"access"`
					Quality struct {
						Fixed bool `json:"fixed"`
					} `json:"quality"`
				} `json:"effective"`
			} `json:"attributes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(matterparity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	clusters, refused := 0, 0
	for _, c := range snap.Clusters {
		var readOnly []uint32
		for _, a := range c.Attributes {
			rw := a.Effective.Access.RW
			if rw == "" || rw == "R" || a.Effective.Quality.Fixed {
				readOnly = append(readOnly, a.ID)
			}
		}
		if len(readOnly) == 0 {
			t.Errorf("%s (0x%04X): no read-only attribute, not even ClusterRevision", c.Name, c.ID)
			continue
		}
		clusters++
		d, root, _ := readOnlyRig(c.ID, readOnly, store.PrivilegeOperate)
		for _, id := range readOnly {
			w := im.AttributeWrite{Path: concreteAttrPath(0, c.ID, id), Value: im.AttributeValue{Value: uint8(1)}, DataVersion: 99, HasDataVersion: true}
			resp := im.HandleWriteRequest(privilegeEnforcementCtx(), d, im.WriteRequest{Writes: []im.AttributeWrite{w}})
			if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusUnsupportedWrite {
				t.Errorf("%s (0x%04X) attribute 0x%04X: answered %+v, want one UNSUPPORTED_WRITE", c.Name, c.ID, id, resp.Responses)
				continue
			}
			refused++
		}
		if len(root.written) != 0 {
			t.Errorf("%s (0x%04X): stored %v", c.Name, c.ID, root.written)
		}
	}
	t.Logf("%d read-only attributes of %d clusters refused", refused, clusters)
}
