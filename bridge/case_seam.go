// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// This file installs the CASE responder's hook of internal/bridgeseam that
// node reads: the completion of a session node installs establishing
// ([CaseAdapter.completeEstablishment]). It declares no exported identifier
// on purpose — see the bridgeseam package doc.

import "github.com/SukramJ/go-fabric/internal/bridgeseam"

func init() {
	bridgeseam.SetCaseEstablishmentCompletion = func(adapter any, fn func(sessionID uint16, completed bool) bool) bool {
		a, ok := adapter.(*CaseAdapter)
		if !ok || a == nil {
			return false
		}
		a.setOnCompletion(fn)
		return true
	}
}
