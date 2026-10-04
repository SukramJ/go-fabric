// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// A bridge of lamps with real group state: the secure harness plus a
// groups.Manager shared by GroupKeyManagement on the root and the Groups
// server the assembler mounts on every lamp (OnOffLight mandates Groups).
// Each lamp's OnOff server records the commands that reach it.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// recordingOnOff is an OnOff (0x0006) server that records every command.
type recordingOnOff struct {
	mu       sync.Mutex
	commands []uint32
	on       bool
}

func (s *recordingOnOff) MatterClusterID() uint32 { return 0x0006 }

func (s *recordingOnOff) MatterRead(attrID uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attrID == 0x0000 {
		return s.on, true
	}
	return nil, false
}

func (s *recordingOnOff) MatterWrite(context.Context, uint32, any) error {
	return errors.New("read-only")
}

func (s *recordingOnOff) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch cmdID {
	case 0x00:
		s.on = false
	case 0x01:
		s.on = true
	case 0x02:
		s.on = !s.on
	default:
		return nil, fmt.Errorf("unknown command 0x%02X", cmdID)
	}
	s.commands = append(s.commands, cmdID)
	return nil, nil
}

func (s *recordingOnOff) MatterReportable() []uint32       { return []uint32{0x0000} }
func (s *recordingOnOff) MatterAcceptedCommands() []uint32 { return []uint32{0x00, 0x01, 0x02} }
func (s *recordingOnOff) MatterGeneratedCommands() []uint32 {
	return nil
}

func (s *recordingOnOff) received() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.commands)
}

// lampSource is an OnOffLight source carrying one recordingOnOff.
type lampSource struct{ onoff *recordingOnOff }

func (lampSource) MatterDeviceType() uint16 { return 0x0100 }
func (l lampSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{l.onoff}
}

// groupsHarness is a secure harness over a bridge of lamps.
type groupsHarness struct {
	*secureHarness
	groups    *groups.Manager
	lamps     map[uint16]*recordingOnOff // by endpoint id
	groupcast *mattercore.Groupcast      // set by newGroupcastHarness
}

// newGroupsHarness builds a bridge with n lamps (endpoints 2..n+1), a root
// GroupKeyManagement, and the CASE controller holding Administer.
func newGroupsHarness(t *testing.T, n int) *groupsHarness {
	t.Helper()
	return newGroupsHarnessWith(t, n, false)
}

// newGroupcastHarness is newGroupsHarness with the root Matter 1.6.1 asks
// for: GroupKeyManagement, AccessControl and Groupcast over the one group
// state, the bridge enforcing the auxiliary entries, and group messaging
// attached.
func newGroupcastHarness(t *testing.T, n int) *groupsHarness {
	t.Helper()
	gh := newGroupsHarnessWith(t, n, true)
	gh.bridge.AttachAuxiliaryACL(gh.groups)
	gh.bridge.AttachGroupMessaging(gh.groups)
	if err := gh.bridge.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	return gh
}

func newGroupsHarnessWith(t *testing.T, n int, withGroupcast bool) *groupsHarness {
	t.Helper()
	gh := &groupsHarness{lamps: make(map[uint16]*recordingOnOff)}
	recorders := make([]*recordingOnOff, n)
	for i := range recorders {
		recorders[i] = &recordingOnOff{}
	}
	h := newSecureHarnessWith(t, func(st *store.Store, _ uint8) (Snapshotter, []contract.ClusterServer) {
		mgr, err := groups.NewManager(st, nil)
		if err != nil {
			t.Fatalf("groups.NewManager: %v", err)
		}
		gh.groups = mgr
		cfg := endpointtest.AssemblerConfig()
		cfg.Groups = mgr
		asm, err := endpoint.New(endpointtest.NewFakeStore(), cfg, nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		specs := make([]endpoint.Spec, n)
		for i := range specs {
			specs[i] = endpoint.Spec{
				StableKey: endpoint.StringKey(fmt.Sprintf("lamp-%d", i)), DeviceType: 0x0100,
				FriendlyName: fmt.Sprintf("Lamp %d", i), Source: lampSource{onoff: recorders[i]},
			}
		}
		snap := func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "lamps", Endpoints: specs, ModelComplete: true}})
		}
		gkm, err := mattercore.NewGroupKeyManagement(st, mattercore.GroupKeyMgmtConfig{Groups: mgr})
		if err != nil {
			t.Fatalf("NewGroupKeyManagement: %v", err)
		}
		if !withGroupcast {
			return snap, []contract.ClusterServer{gkm}
		}
		acl, err := mattercore.NewAccessControl(st)
		if err != nil {
			t.Fatalf("NewAccessControl: %v", err)
		}
		gc, err := mattercore.NewGroupcast(mattercore.GroupcastConfig{Groups: mgr, GroupKeyManagement: gkm, AccessControl: acl})
		if err != nil {
			t.Fatalf("NewGroupcast: %v", err)
		}
		t.Cleanup(gc.Close)
		gh.groupcast = gc
		return snap, []contract.ClusterServer{gkm, acl, gc}
	})
	h.allowAll()
	gh.secureHarness = h
	for _, ep := range h.bridge.Topology().Bridged() {
		src, ok := ep.Source.(lampSource)
		if !ok {
			t.Fatalf("endpoint %d is not a lamp", ep.ID)
		}
		gh.lamps[ep.ID] = src.onoff
	}
	if len(gh.lamps) != n {
		t.Fatalf("assembled %d lamps, want %d", len(gh.lamps), n)
	}
	return gh
}

// lampIDs returns the lamp endpoint ids ascending.
func (gh *groupsHarness) lampIDs() []uint16 {
	ids := make([]uint16, 0, len(gh.lamps))
	for id := range gh.lamps {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// writeAttribute sends a WriteRequest for one concrete attribute and
// returns the status the WriteResponse carries for it.
func (h *secureHarness) writeAttribute(endpointID uint16, cluster, attribute uint32, value func(enc *tlv.Encoder, tag tlv.Tag)) uint64 {
	h.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag()) // AttributeDataIB
	enc.StartList(tlv.ContextTag(1))    // AttributePathIB
	enc.PutUint(tlv.ContextTag(2), uint64(endpointID))
	enc.PutUint(tlv.ContextTag(3), uint64(cluster))
	enc.PutUint(tlv.ContextTag(4), uint64(attribute))
	_ = enc.EndContainer()
	value(enc, tlv.ContextTag(2))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), false) // MoreChunkedMessages
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		h.t.Fatalf("encode WriteRequest: %v", err)
	}
	op, payload, ok := h.exchange(0x06, body)
	if !ok || op != 0x07 {
		h.t.Fatalf("WriteRequest answered opcode 0x%02X (ok=%v), want WriteResponse", op, ok)
	}
	resp := decodeTLVTree(h.t, payload)
	statuses := resp.mustChild(h.t, 0).Children
	if len(statuses) != 1 {
		h.t.Fatalf("WriteResponse has %d statuses, want 1", len(statuses))
	}
	return statuses[0].mustChild(h.t, 1).mustChild(h.t, 0).El.Uint
}

// provisionGroup does what a controller does to put lamps in a group:
// KeySetWrite, a GroupKeyMap write binding the group to the key set, and
// AddGroup on each lamp — all over the wire.
func (gh *groupsHarness) provisionGroup(t *testing.T, keySetID uint16, epochKey []byte, groupID uint16, name string, lamps ...uint16) {
	t.Helper()
	if _, _, status, _ := invokeResult(t, gh.invoke(0, gkmCluster, gkmCmdKeySetWrite, putKeySet(keySetID, epochKey, 1, 0))); status != 0 {
		t.Fatalf("KeySetWrite: %v", status)
	}
	if status := gh.writeAttribute(0, gkmCluster, 0x0000, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), uint64(groupID))
		enc.PutUint(tlv.ContextTag(2), uint64(keySetID))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}); status != 0 {
		t.Fatalf("GroupKeyMap write: status 0x%02X", status)
	}
	for _, ep := range lamps {
		cmd, fields, status, isStatus := invokeResult(t, gh.invoke(ep, mattercore.GroupsClusterID, 0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), uint64(groupID))
			enc.PutUTF8(tlv.ContextTag(1), name)
		}))
		if isStatus || cmd != 0x00 {
			t.Fatalf("AddGroup on %d: status %v (isStatus=%v)", ep, status, isStatus)
		}
		if got := fields.mustChild(t, 0).El.Uint; got != 0 {
			t.Fatalf("AddGroupResponse.Status on %d = 0x%02X, want Success", ep, got)
		}
	}
}
