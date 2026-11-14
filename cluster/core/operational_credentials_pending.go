// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/store"
)

// The two settings keys below are where releases before the fabric row
// carried its own commit state (store schema v8, owner decision 36) kept
// what an AddNOC or UpdateNOC wrote and CommissioningComplete had not
// committed. A database such a release wrote in the middle of a
// commissioning may still hold one of them, next to a fabric row the
// upgrade marked committed; a start rolls back exactly what that release's
// start-up rolled back, then clears the key. Nothing else reads or writes
// them.

// pendingNOCSettingKey held an AddNOC's marker (review II, R2-P1-core-01).
const pendingNOCSettingKey = "operational_credentials.pending_noc"

// pendingNOCUpdateSettingKey held an UpdateNOC's marker (review II,
// R2-P1-dr-sec-04).
const pendingNOCUpdateSettingKey = "operational_credentials.pending_noc_update"

// pendingNOCKeys are the marker's keys, in the order a start rolls them
// back. Each holds one marker, which says by its op what it names.
var pendingNOCKeys = [...]string{pendingNOCSettingKey, pendingNOCUpdateSettingKey}

// pendingNOC* name the marker's op.
const (
	pendingNOCAdd    = "add"
	pendingNOCUpdate = "update"
)

// pendingNOCMarker is the persisted marker. An "add" is written twice
// (handleAddNOC): first as the intent, with the new fabric's identity pair
// — FabricID and root public key — and no index, before AddFabric; then
// with the index AddFabric assigned, before the identity is written. An
// "update" carries what the UpdateNOC replaced ([nocUpdateRevert]) and
// the identity pair. Either way the pair is checked against the fabric
// at the index, so a fabric index reused after a removal is never
// mistaken for the one the marker names.
type pendingNOCMarker struct {
	Op            string `json:"op"`
	FabricIndex   uint8  `json:"fabric_index,omitempty"`
	FabricID      uint64 `json:"fabric_id"`
	RootPublicKey []byte `json:"root_public_key"`
	NOC           []byte `json:"noc,omitempty"`
	ICAC          []byte `json:"icac,omitempty"`
	PrivateKey    []byte `json:"private_key,omitempty"`
	IPK           []byte `json:"ipk,omitempty"`
	NodeID        uint64 `json:"node_id,omitempty"`
	VendorID      uint16 `json:"vendor_id,omitempty"`
	VIDStatement  []byte `json:"vid_statement,omitempty"`
	VVSC          []byte `json:"vvsc,omitempty"`
}

// updateRevert is the marker's update as the in-memory revert record.
func (m pendingNOCMarker) updateRevert() nocUpdateRevert {
	return nocUpdateRevert{
		fabricIndex: m.FabricIndex,
		identity: store.IdentityRecord{
			FabricIndex: m.FabricIndex, NOC: m.NOC, ICAC: m.ICAC, PrivateKey: m.PrivateKey, IPK: m.IPK,
		},
		nodeID:   m.NodeID,
		vid:      vidData{statement: m.VIDStatement, vvsc: m.VVSC},
		vendorID: m.VendorID,
	}
}

// nocUpdateRevert is what an UpdateNOC of a release before v8 replaced,
// as its marker carries it.
type nocUpdateRevert struct {
	fabricIndex uint8
	identity    store.IdentityRecord
	nodeID      uint64
	vid         vidData
	vendorID    uint16
}

// vidData is a fabric's VID verification statement and VVSC.
type vidData struct {
	statement []byte
	vvsc      []byte
}

// recoverLegacyPendingNOC rolls back what a marker a release before v8
// left names: the node went down inside a fail-safe window, after an
// AddNOC or UpdateNOC and before CommissioningComplete, and was upgraded
// before it came back. It runs at construction, before the node reads
// the stored fabrics. A rolled-back AddNOC's fabric is kept for
// [OperationalCredentials.RolledBackFabrics].
func (o *OperationalCredentials) recoverLegacyPendingNOC(ctx context.Context) error {
	for _, key := range pendingNOCKeys {
		if err := o.recoverPendingNOCKey(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// recoverPendingNOCKey rolls back what the marker at key names and clears it.
func (o *OperationalCredentials) recoverPendingNOCKey(ctx context.Context, key string) error {
	raw, found, err := o.store.GetSetting(ctx, key)
	if err != nil {
		return fmt.Errorf("core: OperationalCredentials: read the pending NOC marker: %w", err)
	}
	if !found || raw == "" {
		return nil
	}
	var m pendingNOCMarker
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("core: OperationalCredentials: decode the pending NOC marker: %w", err)
	}
	switch m.Op {
	case pendingNOCAdd:
		idx, err := o.uncommittedAddFabric(ctx, m)
		if err != nil {
			return err
		}
		if idx == 0 {
			break
		}
		fab, err := o.store.GetFabric(ctx, idx)
		if err != nil {
			return fmt.Errorf("core: OperationalCredentials: read fabric %d: %w", idx, err)
		}
		// The release's revertAddNOC: the ACL entries, the group keys and
		// the fabric; the fabric's VID data is part of its row now.
		_ = o.store.ReplaceACL(ctx, idx, nil)
		_ = o.store.RemoveGroupKeysByFabric(ctx, idx)
		if err := o.store.RemoveFabric(ctx, idx); err != nil && !errors.Is(err, store.ErrFabricNotFound) {
			return fmt.Errorf("core: OperationalCredentials: remove the uncommitted fabric %d: %w", idx, err)
		}
		o.rolledBack = append(o.rolledBack, fab)
		o.logger().Warn("matter.opcreds.pending_noc.fabric_reverted", slog.Int("fabric_index", int(idx)))
	case pendingNOCUpdate:
		fab, err := o.store.GetFabric(ctx, m.FabricIndex)
		switch {
		case errors.Is(err, store.ErrFabricNotFound):
		case err != nil:
			return fmt.Errorf("core: OperationalCredentials: read fabric %d: %w", m.FabricIndex, err)
		case fab.FabricID == m.FabricID && bytes.Equal(fab.RootPublicKey, m.RootPublicKey):
			if err := o.restoreUpdateNOC(ctx, m.updateRevert()); err != nil {
				return fmt.Errorf("core: OperationalCredentials: restore the credentials of fabric %d: %w", m.FabricIndex, err)
			}
			o.logger().Warn("matter.opcreds.pending_noc.update_reverted", slog.Int("fabric_index", int(m.FabricIndex)))
		}
	default:
		return fmt.Errorf("core: OperationalCredentials: pending NOC marker with op %q", m.Op)
	}
	if err := o.store.SetSettings(ctx, map[string]string{key: ""}); err != nil {
		return fmt.Errorf("core: OperationalCredentials: clear the pending NOC marker: %w", err)
	}
	return nil
}

// restoreUpdateNOC writes back what an UpdateNOC of a release before v8
// replaced: the identity, the VID verification data, the vendor id and
// the node id. Each step writes the value it restores, so a second
// attempt after a failed one — the marker stays until all landed —
// completes it.
func (o *OperationalCredentials) restoreUpdateNOC(ctx context.Context, r nocUpdateRevert) error {
	if err := o.store.UpsertIdentity(ctx, r.identity); err != nil {
		return err
	}
	if err := o.store.UpdateFabricVIDVerification(ctx, r.fabricIndex, r.vid.statement, r.vid.vvsc); err != nil {
		return err
	}
	fab, err := o.store.GetFabric(ctx, r.fabricIndex)
	if err != nil {
		return err
	}
	if fab.VendorID != r.vendorID {
		if err := o.store.UpdateFabricVendorID(ctx, r.fabricIndex, r.vendorID); err != nil {
			return err
		}
	}
	if fab.NodeID != r.nodeID {
		return o.store.UpdateFabricNodeID(ctx, r.fabricIndex, r.nodeID)
	}
	return nil
}

// uncommittedAddFabric is the fabric an "add" marker names, 0 for none.
// A marker with an index names the fabric at that index when it still
// carries the marker's pair. An intent-only marker (no index) was left
// before AddFabric, by an AddFabric that refused the pair, or by a crash
// before the index was written: the fabric with that pair is the
// uncommitted one only when it has no identity, because handleAddNOC
// writes the identity after the index marker — a fabric with an identity
// was there before this AddNOC, and the store refused the duplicate
// (review II, R2-P1-dr-sec-01).
func (o *OperationalCredentials) uncommittedAddFabric(ctx context.Context, m pendingNOCMarker) (uint8, error) {
	if m.FabricIndex != 0 {
		fab, err := o.store.GetFabric(ctx, m.FabricIndex)
		switch {
		case errors.Is(err, store.ErrFabricNotFound):
			return 0, nil
		case err != nil:
			return 0, fmt.Errorf("core: OperationalCredentials: read fabric %d: %w", m.FabricIndex, err)
		case fab.FabricID != m.FabricID || !bytes.Equal(fab.RootPublicKey, m.RootPublicKey):
			return 0, nil
		}
		return m.FabricIndex, nil
	}
	fabrics, err := o.store.ListFabrics(ctx)
	if err != nil {
		return 0, fmt.Errorf("core: OperationalCredentials: list fabrics: %w", err)
	}
	for i := range fabrics {
		f := fabrics[i]
		if f.FabricID != m.FabricID || !bytes.Equal(f.RootPublicKey, m.RootPublicKey) {
			continue
		}
		_, err := o.store.GetIdentity(ctx, f.FabricIndex)
		switch {
		case errors.Is(err, store.ErrIdentityNotFound):
			return f.FabricIndex, nil
		case err != nil:
			return 0, fmt.Errorf("core: OperationalCredentials: read the identity of fabric %d: %w", f.FabricIndex, err)
		}
		return 0, nil
	}
	return 0, nil
}
