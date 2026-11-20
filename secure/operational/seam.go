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

// errSeamStake is the error of an install hook handed something other than
// a Stake.
var errSeamStake = errors.New("operational: sessionseam: not an operational.Stake")

func init() {
	sessionseam.OpenAt = func(mgr any, id uint16, initiator bool, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (any, error) {
		m, ok := mgr.(*Manager)
		if !ok || m == nil {
			return nil, errSeamManager
		}
		e, err := m.openFromSigma(id, nil, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, initiator, false)
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
	sessionseam.OpenEstablishing = func(mgr, stake any, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (any, error) {
		m, ok := mgr.(*Manager)
		if !ok || m == nil {
			return nil, errSeamManager
		}
		s, ok := stake.(Stake)
		if !ok {
			return nil, errSeamStake
		}
		e, err := m.openFromSigma(s.ID, &s, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, false, true)
		if err != nil {
			return nil, err
		}
		return e, nil
	}
	sessionseam.CompleteEstablishment = func(mgr, entry any) bool {
		m, ok := mgr.(*Manager)
		if !ok || m == nil {
			return false
		}
		e, ok := entry.(*Entry)
		return ok && m.completeEstablishment(e)
	}
}
