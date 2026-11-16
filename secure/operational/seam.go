// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// This file installs the module-internal seams of internal/sessionseam. It
// declares no exported identifier on purpose: an install under a chosen id
// is what the stake installs exist to refuse in production, and the close
// of one entry is the node facade's, not a consumer's. See the sessionseam
// package doc.

import (
	"errors"

	"github.com/SukramJ/go-fabric/internal/sessionseam"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// errSeamManager is the error of a seam hook handed something other than a
// *Manager.
var errSeamManager = errors.New("operational: sessionseam: not a *operational.Manager")

func init() {
	sessionseam.OpenAt = func(mgr any, id uint16, initiator bool, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (any, error) {
		m, ok := mgr.(*Manager)
		if !ok || m == nil {
			return nil, errSeamManager
		}
		e, err := m.openFromSigma(id, nil, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, initiator)
		if err != nil {
			return nil, err
		}
		return e, nil
	}
	sessionseam.CloseEntry = func(mgr, entry any) bool {
		m, ok := mgr.(*Manager)
		if !ok || m == nil {
			return false
		}
		e, ok := entry.(*Entry)
		return ok && m.closeEntry(e)
	}
}
