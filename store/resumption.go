// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// ResumptionRecord is a CASE resumption-id row (Matter §4.13.2.4).
// One per (fabric, peer_node) pair; reused on every successful
// resumption.
//
// CASEAuthTags carries the set of CASE Authenticated Tags extracted
// from the peer's operational certificate (Matter §6.6.5.1 / Core
// §4.13.2.2). Tags allow the responder to grant fabric-scoped ACL
// privilege to resumed sessions without re-validating the full NOC.
// Persisted as JSON in the `case_authenticated_tags` column (migration
// 010). An empty or nil slice means no CATs were present.
// SessionParameters are the peer's session parameters of the session the
// record resumes — what a CASE client keeps to resume it
// (client/handshake.ResumptionRecord.SessionParameters). One record per
// peer serves both roles, the responder's and the client's. Every field
// is optional: zero is absent, and a record a responder wrote carries
// none.
//
// Initiator records which role wrote the record last: true when the CASE
// client of client/handshake saved it, false for a CASE responder and for
// every writer that does not set it. Each role has a per-fabric cap of its
// own and evicts only its own records ([Resumption].UpsertResumption): 16
// records with Initiator false, 1024 with Initiator true. A rewrite by the
// other role takes that role. A record stored before schema v12 did not
// record its role and counts as Initiator false.
//
// matter.js ref: packages/protocol/src/session/case/
type ResumptionRecord struct {
	FabricIndex       uint8
	PeerNodeID        uint64
	ResumptionID      []byte   // 16 bytes
	SharedSecret      []byte   // 32 bytes (HKDF input for resumed sessions)
	CASEAuthTags      []uint32 // optional; nil == no CATs (Matter §6.6.5.1)
	SessionParameters sessionparams.Parameters
	Initiator         bool // the node's CASE client wrote it (schema v12)
}

// resumptionColumns are the matter_resumption columns a record reads back,
// in scanResumption's order, the role and the seal mark last.
const resumptionColumns = `fabric_index, peer_node_id, resumption_id, shared_secret, case_authenticated_tags,
    session_idle_interval, session_active_interval, session_active_threshold,
    data_model_revision, interaction_model_revision, specification_version,
    max_paths_per_invoke, supported_transports, max_tcp_message_size, initiator, sealed`

// scanResumption reads one row of resumptionColumns, its shared secret
// opened.
func (s *Store) scanResumption(r scanRow) (ResumptionRecord, error) {
	var (
		rec      ResumptionRecord
		nodeBlob []byte
		catsJSON []byte
		sealed   bool
	)
	p := &rec.SessionParameters
	if err := r.Scan(&rec.FabricIndex, &nodeBlob, &rec.ResumptionID, &rec.SharedSecret, &catsJSON,
		&p.SessionIdleInterval, &p.SessionActiveInterval, &p.SessionActiveThreshold,
		&p.DataModelRevision, &p.InteractionModelRevision, &p.SpecificationVersion,
		&p.MaxPathsPerInvoke, &p.SupportedTransports, &p.MaxTCPMessageSize, &rec.Initiator, &sealed); err != nil {
		return ResumptionRecord{}, err
	}
	if len(nodeBlob) != 8 {
		return ResumptionRecord{}, fmt.Errorf("store: peer_node_id length=%d (want 8)", len(nodeBlob))
	}
	rec.PeerNodeID = binary.BigEndian.Uint64(nodeBlob)
	secret, err := s.openColumn(rec.SharedSecret, sealed, "matter_resumption", "shared_secret", rec.FabricIndex, nodeBlob)
	if err != nil {
		return ResumptionRecord{}, err
	}
	if len(secret) != resumptionSecretSize {
		return ResumptionRecord{}, fmt.Errorf("store: shared_secret length=%d (want %d)", len(secret), resumptionSecretSize)
	}
	rec.SharedSecret = secret
	if err := json.Unmarshal(catsJSON, &rec.CASEAuthTags); err != nil {
		rec.CASEAuthTags = nil // tolerate corrupt rows
	}
	return rec, nil
}

// resumptionSecretSize is the shared secret's size, checked on every write
// and every read.
const resumptionSecretSize = 32

// resumptionRecordsPerFabric is how many resumption records with
// Initiator false the store keeps for one fabric ([Resumption]):
// UpsertResumption of one more peer's record evicts the fabric's such
// record written least recently. The cap is per fabric, so a fabric's
// administrator, who mints the node ids its peers authenticate with,
// cannot grow the table for every fabric or evict another fabric's
// records. It bounds the records with Initiator false only; the records
// the CASE client of client/handshake writes ([ResumptionRecord].Initiator)
// are neither counted nor evicted by it, so the sentence above holds for
// the responder's records, and resumptionClientRecordsPerFabric bounds the
// client's. store/memstore keeps the same numbers, and storetest's
// resumption/cap-per-fabric, resumption/cap-counts-responders-only and
// resumption/cap-per-role hold both to them.
const resumptionRecordsPerFabric = 16

// resumptionClientRecordsPerFabric is how many resumption records with
// Initiator true the store keeps for one fabric (owner decision 273): the
// write of one more evicts the fabric's client record written least
// recently, never a responder's. The client's records are of a
// controller's peers and of the targets the node's client stack connects
// to, among them targets a peer names (node/peer_path.go: the provider of
// an AnnounceOTAProvider, a camera's WebRTC requestor); this bound keeps
// the administrator who mints the node ids from growing them without end.
const resumptionClientRecordsPerFabric = 1024

// ErrResumptionNotFound is returned when a lookup misses.
var ErrResumptionNotFound = errors.New("store: resumption record not found")

// UpsertResumption replaces the resumption row for (fabric_index,
// peer_node_id) in one transaction: the old row deleted, the new one
// inserted with a write sequence one above the table's highest (write_seq,
// the store's own count — not the clock, which may step back), and then the
// fabric's rows of the written record's role beyond that role's cap
// deleted — resumptionRecordsPerFabric for initiator 0,
// resumptionClientRecordsPerFabric for initiator 1 — the lowest write
// sequence first; a write never deletes a row of the other role. The row
// just written holds the highest, so it is never the one evicted.
// CASEAuthTags is
// serialised as JSON; nil and empty slice both persist as '[]'.
func (s *Store) UpsertResumption(ctx context.Context, rec ResumptionRecord) error {
	if len(rec.ResumptionID) != 16 {
		return fmt.Errorf("store: resumption_id length=%d (want 16)", len(rec.ResumptionID))
	}
	if len(rec.SharedSecret) != resumptionSecretSize {
		return fmt.Errorf("store: shared_secret length=%d (want %d)", len(rec.SharedSecret), resumptionSecretSize)
	}
	cats := rec.CASEAuthTags
	if cats == nil {
		cats = []uint32{}
	}
	catsJSON, err := json.Marshal(cats)
	if err != nil {
		return fmt.Errorf("store: marshal case_authenticated_tags: %w", err)
	}
	p := rec.SessionParameters
	peer := uint64ToBE(rec.PeerNodeID)
	secret, err := s.sealColumn(rec.SharedSecret, "matter_resumption", "shared_secret", rec.FabricIndex, peer)
	if err != nil {
		return fmt.Errorf("store: upsert resumption: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: upsert resumption: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM matter_resumption WHERE fabric_index = ? AND peer_node_id = ?`, rec.FabricIndex, peer); err != nil {
		return fmt.Errorf("store: upsert resumption: %w", err)
	}
	if err := s.refuseClearSecretWrite(ctx, tx); err != nil {
		return fmt.Errorf("store: upsert resumption: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx, `
INSERT INTO matter_resumption
    (`+resumptionColumns+`, last_used_at, write_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'),
    (SELECT COALESCE(MAX(write_seq), 0) + 1 FROM matter_resumption))`,
		rec.FabricIndex, peer, rec.ResumptionID, secret, catsJSON,
		p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold,
		p.DataModelRevision, p.InteractionModelRevision, p.SpecificationVersion,
		p.MaxPathsPerInvoke, p.SupportedTransports, p.MaxTCPMessageSize, rec.Initiator, s.sealer != nil,
	); err != nil {
		return fmt.Errorf("store: upsert resumption: %w", err)
	}
	limit := resumptionRecordsPerFabric
	if rec.Initiator {
		limit = resumptionClientRecordsPerFabric
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM matter_resumption WHERE fabric_index = ? AND initiator = ? AND peer_node_id NOT IN (
    SELECT peer_node_id FROM matter_resumption WHERE fabric_index = ? AND initiator = ?
    ORDER BY write_seq DESC LIMIT ?)`,
		rec.FabricIndex, rec.Initiator, rec.FabricIndex, rec.Initiator, limit); err != nil {
		return fmt.Errorf("store: upsert resumption: evict: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: upsert resumption: commit: %w", err)
	}
	return nil
}

// GetResumptionByID looks up a row by ResumptionID alone (the Sigma1
// path, where the responder receives the ID before it knows which
// fabric/peer it points at).
func (s *Store) GetResumptionByID(ctx context.Context, resumptionID []byte) (ResumptionRecord, error) {
	if len(resumptionID) != 16 {
		return ResumptionRecord{}, fmt.Errorf("store: resumption_id length=%d (want 16)", len(resumptionID))
	}
	rec, err := s.scanResumption(s.db.QueryRowContext(ctx, `
SELECT `+resumptionColumns+`
FROM matter_resumption WHERE resumption_id = ?`, resumptionID))
	if errors.Is(err, sql.ErrNoRows) {
		return ResumptionRecord{}, ErrResumptionNotFound
	}
	if err != nil {
		return ResumptionRecord{}, fmt.Errorf("store: get resumption: %w", err)
	}
	return rec, nil
}

// GetResumptionByPeer looks up by (fabric_index, peer_node_id).
func (s *Store) GetResumptionByPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (ResumptionRecord, error) {
	rec, err := s.scanResumption(s.db.QueryRowContext(ctx, `
SELECT `+resumptionColumns+`
FROM matter_resumption WHERE fabric_index = ? AND peer_node_id = ?`,
		fabricIndex, uint64ToBE(peerNodeID)))
	if errors.Is(err, sql.ErrNoRows) {
		return ResumptionRecord{}, ErrResumptionNotFound
	}
	if err != nil {
		return ResumptionRecord{}, fmt.Errorf("store: get resumption: %w", err)
	}
	return rec, nil
}

// RemoveResumption deletes a row. Idempotent.
func (s *Store) RemoveResumption(ctx context.Context, fabricIndex uint8, peerNodeID uint64) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM matter_resumption WHERE fabric_index = ? AND peer_node_id = ?`,
		fabricIndex, uint64ToBE(peerNodeID)); err != nil {
		return fmt.Errorf("store: remove resumption: %w", err)
	}
	return nil
}

// RemoveResumptionsByFabric deletes every resumption record bound to
// fabricIndex. Called from the OnFabricRemoved hook to clean up
// resumption state for a fabric that was just removed via RemoveFabric;
// mirrors chip src/credentials/FabricTable.cpp Delete() resumption
// teardown. Independent of the matter_fabrics FK so callers don't have
// to rely on SQLite's CASCADE behaviour for defense-in-depth.
func (s *Store) RemoveResumptionsByFabric(ctx context.Context, fabricIndex uint8) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM matter_resumption WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("store: remove resumptions by fabric: %w", err)
	}
	return nil
}
