// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/SukramJ/go-fabric/store"
)

// MulticastAddrPolicyEnum values of the Groupcast cluster
// (groupcast.element.ts MulticastAddrPolicyEnum).
const (
	// PolicyIanaAddr puts a group on the shared [IANAGroupcastAddress].
	PolicyIanaAddr uint8 = 0
	// PolicyPerGroup puts a group on its own fabric- and group-derived
	// address ([MulticastAddress]); conformance PGA.
	PolicyPerGroup uint8 = 1
)

// DefaultMcastAddrPolicy is the policy of a group without Groupcast
// properties — a group created through the Groups cluster — on a node whose
// Groupcast server supports the PerGroup feature, the only kind this module
// builds. Mirrors matter.js GroupcastServer #defaultMcastAddrPolicy
// ("PerGroup on PerGroupAddr-capable devices, so legacy groups stay on
// their ff35 per-group address") and Groups.multicastAddress, which uses the
// per-group address for a group without a policy.
const DefaultMcastAddrPolicy = PolicyPerGroup

// UnmappedKeySetID is the Membership KeySetId of a group GroupKeyMap does
// not bind to a key set. Mirrors matter.js GroupcastServer
// UNMAPPED_KEYSET_ID.
const UnmappedKeySetID uint16 = 0xFFFF

// IANAGroupcastAddress is the IANA-assigned multicast address the Groupcast
// cluster's IanaAddr policy shares across groups and fabrics: FF05::FA.
// Mirrors matter.js packages/protocol/src/groups/Groups.ts
// IANA_GROUPCAST_MULTICAST_ADDRESS.
var IANAGroupcastAddress = net.ParseIP("ff05::fa")

// GroupProperties is the Groupcast cluster's state of one group of a fabric:
// matter.js GroupcastServer GroupPropertiesStruct without GroupId and
// FabricIndex. A group with properties is a Groupcast member even while the
// group table holds none of its endpoints.
type GroupProperties struct {
	McastAddrPolicy uint8
	HasAuxiliaryACL bool
}

// GroupcastMembership is one entry of the Groupcast Membership attribute
// (groupcast.element.ts MembershipStruct), derived from its sources: the
// Groupcast properties, the group table and the GroupKeyMap.
type GroupcastMembership struct {
	FabricIndex uint8
	GroupID     uint16
	// Endpoints are the group's local member endpoints, in the order they
	// joined; empty for a group without one.
	Endpoints []uint16
	// KeySetID is the key set GroupKeyMap binds the group to, or
	// [UnmappedKeySetID].
	KeySetID        uint16
	HasAuxiliaryACL bool
	McastAddrPolicy uint8
}

// multicastAddress returns the address a group of f is received on.
// Mirrors matter.js Groups.multicastAddress: IanaAddr shares FF05::FA,
// everything else uses the per-group address. Called with m.mu held.
func (f *fabricGroups) multicastAddress(groupID uint16) net.IP {
	if p, ok := f.props[groupID]; ok && p.McastAddrPolicy == PolicyIanaAddr {
		return slices.Clone(IANAGroupcastAddress)
	}
	return MulticastAddress(f.fabricID, groupID)
}

// MulticastAddressFor returns the multicast address groupID of the fabric is
// received on, following its policy. Mirrors matter.js
// FabricGroups.multicastAddressFor.
func (m *Manager) MulticastAddressFor(ctx context.Context, fabricIndex uint8, groupID uint16) (net.IP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		return nil, err
	}
	return f.multicastAddress(groupID), nil
}

// GroupcastMemberships derives the Groupcast Membership list of every
// fabric: a group is a member while it has Groupcast properties or a group
// table entry (GroupKeyMap alone makes no member); Endpoints come from the
// group table, KeySetID from GroupKeyMap, the flags from the properties or
// their defaults. Ordered by fabric index, then group id. Mirrors matter.js
// GroupcastServer #deriveMembership, which rebuilds the attribute from the
// same three sources on every change of one of them.
func (m *Manager) GroupcastMemberships(ctx context.Context) ([]GroupcastMembership, error) {
	if err := m.ensureAllLoaded(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []GroupcastMembership
	for _, idx := range m.fabricIndexesLocked() {
		out = append(out, m.fabricMembershipsLocked(m.fabrics[idx])...)
	}
	return out, nil
}

// fabricMembershipsLocked derives one fabric's Membership entries. Called
// with m.mu held.
func (m *Manager) fabricMembershipsLocked(f *fabricGroups) []GroupcastMembership {
	ids := make([]uint16, 0, len(f.props)+len(f.table))
	for id := range f.props {
		ids = append(ids, id)
	}
	for id := range f.table {
		if _, dup := f.props[id]; !dup {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	out := make([]GroupcastMembership, 0, len(ids))
	for _, id := range ids {
		e := GroupcastMembership{
			FabricIndex: f.index, GroupID: id, Endpoints: []uint16{},
			KeySetID: UnmappedKeySetID, McastAddrPolicy: DefaultMcastAddrPolicy,
		}
		if t, ok := f.table[id]; ok {
			e.Endpoints = slices.Clone(t.Endpoints)
		}
		if ks, ok := f.idMap[id]; ok {
			e.KeySetID = ks
		}
		if p, ok := f.props[id]; ok {
			e.McastAddrPolicy, e.HasAuxiliaryACL = p.McastAddrPolicy, p.HasAuxiliaryACL
		}
		out = append(out, e)
	}
	return out
}

// SetGroupProperties creates or updates the Groupcast properties of a
// group, applying only the fields given: a new entry takes
// [DefaultMcastAddrPolicy] and no auxiliary ACL for a field left nil.
// Mirrors matter.js GroupcastServer #upsertGroupProperties. A policy change
// of a group with member endpoints moves its multicast membership, as
// ServerGroupNetworking rebinds on a policy change.
func (m *Manager) SetGroupProperties(ctx context.Context, fabricIndex uint8, groupID uint16, policy *uint8, hasAuxiliaryACL *bool) error {
	m.mu.Lock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	prev, existed := f.props[groupID]
	next := GroupProperties{McastAddrPolicy: DefaultMcastAddrPolicy}
	if existed {
		next = prev
	}
	if policy != nil {
		next.McastAddrPolicy = *policy
	}
	if hasAuxiliaryACL != nil {
		next.HasAuxiliaryACL = *hasAuxiliaryACL
	}
	if existed && next == prev {
		m.mu.Unlock()
		return nil
	}
	if err := m.st.UpsertGroupcastGroup(ctx, store.GroupcastGroup{
		FabricIndex: fabricIndex, GroupID: groupID, McastAddrPolicy: next.McastAddrPolicy, HasAuxiliaryACL: next.HasAuxiliaryACL,
	}); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("groups: persist groupcast group %d: %w", groupID, err)
	}
	f.props[groupID] = next
	rebind := (prev.McastAddrPolicy != next.McastAddrPolicy || !existed) && f.hasEndpoints(groupID)
	m.mu.Unlock()
	m.logger.Info("groups.groupcast_properties",
		slog.Int("fabric_index", int(fabricIndex)), slog.Int("group_id", int(groupID)),
		slog.Int("policy", int(next.McastAddrPolicy)), slog.Bool("auxiliary_acl", next.HasAuxiliaryACL))
	m.notifyGroupcast(ctx, fabricIndex)
	if rebind {
		m.notify()
	}
	return nil
}

// RemoveGroupProperties deletes the Groupcast properties of a group.
// Mirrors matter.js GroupcastServer #removeGroupProperties.
func (m *Manager) RemoveGroupProperties(ctx context.Context, fabricIndex uint8, groupID uint16) error {
	m.mu.Lock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	prev, existed := f.props[groupID]
	if !existed {
		m.mu.Unlock()
		return nil
	}
	if err := m.st.RemoveGroupcastGroup(ctx, fabricIndex, groupID); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("groups: remove groupcast group %d: %w", groupID, err)
	}
	delete(f.props, groupID)
	rebind := prev.McastAddrPolicy != DefaultMcastAddrPolicy && f.hasEndpoints(groupID)
	m.mu.Unlock()
	m.notifyGroupcast(ctx, fabricIndex)
	if rebind {
		m.notify()
	}
	return nil
}

// hasEndpoints reports whether the group table holds an endpoint of
// groupID. Called with m.mu held.
func (f *fabricGroups) hasEndpoints(groupID uint16) bool {
	e, ok := f.table[groupID]
	return ok && len(e.Endpoints) > 0
}

// AuxiliaryACL returns the auxiliary access control entries the Groupcast
// state grants — of one fabric, or of every fabric for fabricIndex 0: for
// each Membership entry with HasAuxiliaryACL and at least one listener
// endpoint, an Operate entry for the Group auth mode whose only subject is
// the group id and whose targets are the group's endpoints. Mirrors matter.js
// GroupcastServer #emitAuxAcl (core§11.27.10: sender-only memberships get
// none); the entries carry AuxiliaryType Groupcast on the wire.
func (m *Manager) AuxiliaryACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	if err := m.ensureAllLoaded(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []store.ACLEntry
	for _, idx := range m.fabricIndexesLocked() {
		if fabricIndex != 0 && idx != fabricIndex {
			continue
		}
		for _, ms := range m.fabricMembershipsLocked(m.fabrics[idx]) {
			if !ms.HasAuxiliaryACL || len(ms.Endpoints) == 0 {
				continue
			}
			targets := make([]store.ACLTarget, 0, len(ms.Endpoints))
			for _, ep := range ms.Endpoints {
				targets = append(targets, store.ACLTarget{Endpoint: &ep})
			}
			out = append(out, store.ACLEntry{
				FabricIndex: idx, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup,
				Subjects: []uint64{uint64(ms.GroupID)}, Targets: targets,
			})
		}
	}
	return out, nil
}

// OnGroupcastChanged registers fn to run after a change of any source of
// the Groupcast Membership attribute on a fabric — the group table, the
// GroupKeyMap, the Groupcast properties — or the fabric's removal. ctx is
// the context of the change: the request that caused it when there was
// one, so a listener can name the administering node. fn runs
// synchronously, outside the manager's locks. Mirrors the reactors matter.js
// GroupcastServer registers on groupTable$Changed, groupKeyMap$Changed and
// the FabricManager events, each of which re-derives Membership and
// re-emits the auxiliary ACL.
func (m *Manager) OnGroupcastChanged(fn func(ctx context.Context, fabricIndex uint8)) {
	if fn == nil {
		return
	}
	m.observersMu.Lock()
	m.groupcastObservers = append(m.groupcastObservers, fn)
	m.observersMu.Unlock()
}

func (m *Manager) notifyGroupcast(ctx context.Context, fabricIndex uint8) {
	m.observersMu.Lock()
	obs := slices.Clone(m.groupcastObservers)
	m.observersMu.Unlock()
	for _, fn := range obs {
		fn(ctx, fabricIndex)
	}
}
