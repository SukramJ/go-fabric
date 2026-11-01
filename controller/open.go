// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"database/sql"
	"fmt"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
	"github.com/SukramJ/go-fabric/store"
)

// OpenDB opens the SQLite database file at path for [Config].DB with the
// module's opener ([store.OpenSQLite]: the pure-Go driver, so a controller
// program imports no driver of its own; an escaped `file:` URI; foreign
// keys on, as the schemas' cascades need them on every pooled connection;
// a busy timeout of 5 s) plus the write-ahead log, the reference
// controller's settings. A file it creates is owner-only (0600): it holds
// the CA's root key unencrypted. The schemas are [New]'s to apply.
//
// The file is opened on first use — [New]'s schemas, which report a path
// that cannot be opened. The caller closes the database after the
// controller.
//
// matter.js keeps a controller's fabric and nodes in the StorageService
// of the controller's Environment (packages/matter.js/src/CommissioningController.ts:338-349
// at 862ad75f); the Go controller keeps them in the SQL database the host
// owns, so the opener is a convenience over database/sql with no matter.js
// counterpart of its own.
func OpenDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: OpenDB needs a path", ErrConfiguration)
	}
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("controller: open %q: %w", path, err)
	}
	return db, nil
}

// ProductionTrust is the trust store of a production controller for
// [Config].Attestation: the Product Attestation Authorities and
// Certification Declaration signers of the DCL snapshot embedded in the
// module (controller/dcl/snapshot, the production set), and nothing else —
// attestation.Trust(attestation.Production, snapshot, nil). It reaches no
// network: a controller that also asks the online ledger layers a
// dcl.CertificateStore over it with attestation.Trust. A snapshot entry
// that does not load is an error; the store returned beside it holds the
// entries loaded until then.
//
// It mirrors the production trust of matter.js's DclCertificateService
// (packages/protocol/src/dcl/DclCertificateService.ts:296, :322 at
// 862ad75f: the production ledger's set, the test ledger only with
// fetchTestCertificates), as attestation.Trust documents.
func ProductionTrust() (attestation.PAAStore, error) {
	s, err := snapshot.Embedded()
	if s == nil {
		return nil, fmt.Errorf("controller: DCL snapshot: %w", err)
	}
	trust := attestation.Trust(attestation.Production, s, nil)
	if err != nil {
		return trust, fmt.Errorf("controller: DCL snapshot: %w", err)
	}
	return trust, nil
}
