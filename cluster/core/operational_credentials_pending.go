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

// pendingNOCSettingKey is the settings key of the durable twin of
// pendingInstallFabricIndex / pendingUpdateRevert: what an AddNOC or
// UpdateNOC wrote to the store before CommissioningComplete committed it.
// The fail-safe that would roll it back lives in memory; this marker is
// what lets a restart inside the window roll it back too (review II,
// R2-P1-core-01). Empty means no marker.
const pendingNOCSettingKey = "operational_credentials.pending_noc"

// pendingNOCUpdateSettingKey holds the "update" marker, apart from the
// "add" one at [pendingNOCSettingKey]: an UpdateNOC that fails after its
// identity write leaves its revert pending without counting as the
// fail-safe's NOC command, so an AddNOC in the same fail-safe is admitted
// and both are pending at once — in memory (pendingUpdateRevert and
// pendingInstallFabricIndex) and so on disk (review II, R2-P1-dr-sec-04).
const pendingNOCUpdateSettingKey = "operational_credentials.pending_noc_update"

// pendingNOCKeys are the marker's keys, in the order a start rolls them
// back. Each holds one marker, which says by its op what it names.
var pendingNOCKeys = [...]string{pendingNOCSettingKey, pendingNOCUpdateSettingKey}

// setPendingNOCSetting writes value at key, one of [pendingNOCKeys]. Each
// key is stated at its own SetSetting call, so what the marker writes is
// readable from the calls (script/docscheck's settings-keys guard).
func setPendingNOCSetting(ctx context.Context, st interface {
	SetSetting(ctx context.Context, key, value string) error
}, key, value string,
) error {
	switch key {
	case pendingNOCSettingKey:
		return st.SetSetting(ctx, pendingNOCSettingKey, value)
	case pendingNOCUpdateSettingKey:
		return st.SetSetting(ctx, pendingNOCUpdateSettingKey, value)
	}
	return fmt.Errorf("core: pending NOC marker key %q", key)
}

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

// key is the settings key the marker is written to.
func (m pendingNOCMarker) key() string {
	if m.Op == pendingNOCUpdate {
		return pendingNOCUpdateSettingKey
	}
	return pendingNOCSettingKey
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

// updateMarker is the marker of an UpdateNOC that replaces r on fab.
func updateMarker(fab store.FabricRecord, r *nocUpdateRevert) pendingNOCMarker {
	return pendingNOCMarker{
		Op: pendingNOCUpdate, FabricIndex: r.fabricIndex,
		FabricID: fab.FabricID, RootPublicKey: fab.RootPublicKey,
		NOC: r.identity.NOC, ICAC: r.identity.ICAC, PrivateKey: r.identity.PrivateKey, IPK: r.identity.IPK,
		NodeID: r.nodeID, VendorID: r.vendorID,
		VIDStatement: r.vid.statement, VVSC: r.vid.vvsc,
	}
}

// writePendingNOC persists m, at its key, before the command's first
// store write. A store without settings keeps the pending state in memory
// only, as before.
func (o *OperationalCredentials) writePendingNOC(ctx context.Context, m pendingNOCMarker) error {
	st, ok := o.store.(vidVerificationSettings)
	if !ok {
		return nil
	}
	b, err := json.Marshal(m) //nolint:gosec // an update's marker carries the replaced operational key, persisted as the identity table holds it (store.IdentityRecord.PrivateKey)
	if err != nil {
		return fmt.Errorf("core: OperationalCredentials: encode the pending NOC marker: %w", err)
	}
	if err := setPendingNOCSetting(ctx, st, m.key(), string(b)); err != nil {
		return fmt.Errorf("core: OperationalCredentials: persist the pending NOC marker: %w", err)
	}
	o.mu.Lock()
	if o.pendingNOCMarkersOut == nil {
		o.pendingNOCMarkersOut = map[string]pendingNOCMarker{}
	}
	o.pendingNOCMarkersOut[m.key()] = m
	o.mu.Unlock()
	return nil
}

// clearPendingNOCKey removes the marker at key once what it names is
// committed or rolled back. It touches the store only when this server wrote the
// marker at key, and writes only when it still stands: CommissioningComplete
// clears it before it answers (GeneralCommissioning.commitPendingNOCLocked).
// A failed clear is logged: the next start would roll back what it names.
func (o *OperationalCredentials) clearPendingNOCKey(ctx context.Context, key string) {
	o.mu.Lock()
	_, out := o.pendingNOCMarkersOut[key]
	o.mu.Unlock()
	if !out {
		return
	}
	st, ok := o.store.(vidVerificationSettings)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if v, found, err := st.GetSetting(ctx, key); err == nil && (!found || v == "") {
		o.mu.Lock()
		delete(o.pendingNOCMarkersOut, key)
		o.mu.Unlock()
		return
	}
	if err := setPendingNOCSetting(ctx, st, key, ""); err != nil {
		o.logger().Error("matter.opcreds.pending_noc.clear_failed", slog.String("key", key), slog.Any("err", err))
		return
	}
	o.mu.Lock()
	delete(o.pendingNOCMarkersOut, key)
	o.mu.Unlock()
}

// clearSettledPendingNOC clears the markers whose rollback is not owed:
// a marker an expiry's failed rollback left stays until it lands.
func (o *OperationalCredentials) clearSettledPendingNOC(ctx context.Context) {
	o.mu.Lock()
	updateOwed, addOwed := o.owedUpdateRevert != nil, o.owedAddRevert != 0
	o.mu.Unlock()
	if !updateOwed {
		o.clearPendingNOCKey(ctx, pendingNOCUpdateSettingKey)
	}
	if !addOwed {
		o.clearPendingNOCKey(ctx, pendingNOCSettingKey)
	}
}

// retryOwedReverts runs again the rollbacks an earlier expiry could not
// land. One that lands — or whose fabric is gone — clears its marker; one that does not keeps it,
// written again when CommissioningComplete cleared it in between
// (GeneralCommissioning.commitPendingNOCLocked clears every marker key).
// A landed AddNOC rollback fires no hook: the expiry that owed it already
// reported the fabric removed. Caller holds nocMu.
func (o *OperationalCredentials) retryOwedReverts(ctx context.Context) {
	o.mu.Lock()
	update, add := o.owedUpdateRevert, o.owedAddRevert
	o.mu.Unlock()
	if update != nil {
		// A fabric removed since the update has nothing left to restore:
		// its owed revert is settled, not retried — a retry would write an
		// identity for the removed index and fail on the fabric's row
		// forever (review II, R2-P1-rw-02). A failed read keeps it owed.
		_, gerr := o.store.GetFabric(ctx, update.fabricIndex)
		if errors.Is(gerr, store.ErrFabricNotFound) || (gerr == nil && o.revertUpdateNOC(ctx, *update)) {
			o.mu.Lock()
			o.owedUpdateRevert = nil
			o.mu.Unlock()
			o.clearPendingNOCKey(ctx, pendingNOCUpdateSettingKey)
		} else {
			o.restorePendingNOC(ctx, pendingNOCUpdateSettingKey)
		}
	}
	if add != 0 {
		if err := o.revertAddNOC(ctx, add); err == nil || errors.Is(err, store.ErrFabricNotFound) {
			o.mu.Lock()
			o.owedAddRevert = 0
			o.mu.Unlock()
			o.clearPendingNOCKey(ctx, pendingNOCSettingKey)
		} else {
			o.restorePendingNOC(ctx, pendingNOCSettingKey)
		}
	}
}

// owedRevertError fails an AddNOC (op "add") or UpdateNOC (op "update")
// that would write its own marker over the key an owed rollback holds,
// while that rollback is still owed. The command retried the owed
// rollbacks at its head (retryOwedReverts), before it read anything its
// checks and its own revert record are made of (review II, R2-P1-rw-01);
// this is the refusal of one that did not land, where the marker is
// written. Caller holds nocMu.
func (o *OperationalCredentials) owedRevertError(op string) error {
	o.mu.Lock()
	owed := (op == pendingNOCUpdate && o.owedUpdateRevert != nil) || (op == pendingNOCAdd && o.owedAddRevert != 0)
	o.mu.Unlock()
	if owed {
		return fmt.Errorf("core: OperationalCredentials: an expired fail-safe's %s rollback has not landed in the store yet", op)
	}
	return nil
}

// restorePendingNOC writes the marker at key again when the store no
// longer holds it, from this server's copy. A failed write is logged.
func (o *OperationalCredentials) restorePendingNOC(ctx context.Context, key string) {
	o.mu.Lock()
	m, out := o.pendingNOCMarkersOut[key]
	o.mu.Unlock()
	st, ok := o.store.(vidVerificationSettings)
	if !out || !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if v, found, err := st.GetSetting(ctx, key); err == nil && found && v != "" {
		return
	}
	if err := o.writePendingNOC(ctx, m); err != nil {
		o.logger().Error("matter.opcreds.pending_noc.restore_failed", slog.String("key", key), slog.Any("err", err))
	}
}

// recoverPendingNOC rolls back what a marker left by a previous run names:
// the node went down inside a fail-safe window, after an AddNOC or
// UpdateNOC and before CommissioningComplete. Both references come back
// from such a restart without the uncommitted fabric: chip keeps it
// pending until CommissioningComplete (OperationalCredentialsCluster.cpp:507
// AddNewPendingFabricWithOperationalKeystore at 6170af84) and matter.js
// persists it only in completeCommission (FailsafeContext.ts:141-143 at
// db021da0). It runs at construction, before the node reads the stored
// fabrics; no hook fires, since nothing has been announced or opened yet.
func (o *OperationalCredentials) recoverPendingNOC(ctx context.Context) error {
	st, ok := o.store.(vidVerificationSettings)
	if !ok {
		return nil
	}
	for _, key := range pendingNOCKeys {
		if err := o.recoverPendingNOCKey(ctx, st, key); err != nil {
			return err
		}
	}
	return nil
}

// recoverPendingNOCKey rolls back what the marker at key names and clears it.
func (o *OperationalCredentials) recoverPendingNOCKey(ctx context.Context, st vidVerificationSettings, key string) error {
	raw, found, err := st.GetSetting(ctx, key)
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
		if err := o.revertAddNOC(ctx, idx); err != nil && !errors.Is(err, store.ErrFabricNotFound) {
			return fmt.Errorf("core: OperationalCredentials: remove the uncommitted fabric %d: %w", idx, err)
		}
		o.forgetVidVerification(ctx, idx)
		o.logger().Warn("matter.opcreds.pending_noc.fabric_reverted", slog.Int("fabric_index", int(idx)))
	case pendingNOCUpdate:
		fab, err := o.store.GetFabric(ctx, m.FabricIndex)
		switch {
		case errors.Is(err, store.ErrFabricNotFound):
		case err != nil:
			return fmt.Errorf("core: OperationalCredentials: read fabric %d: %w", m.FabricIndex, err)
		case fab.FabricID == m.FabricID && bytes.Equal(fab.RootPublicKey, m.RootPublicKey):
			if _, _, err := o.restoreUpdateNOC(ctx, m.updateRevert()); err != nil {
				return fmt.Errorf("core: OperationalCredentials: restore the credentials of fabric %d: %w", m.FabricIndex, err)
			}
			o.logger().Warn("matter.opcreds.pending_noc.update_reverted", slog.Int("fabric_index", int(m.FabricIndex)))
		}
	default:
		return fmt.Errorf("core: OperationalCredentials: pending NOC marker with op %q", m.Op)
	}
	if err := setPendingNOCSetting(ctx, st, key, ""); err != nil {
		return fmt.Errorf("core: OperationalCredentials: clear the pending NOC marker: %w", err)
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
