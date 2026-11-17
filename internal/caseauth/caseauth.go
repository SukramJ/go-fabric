// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package caseauth tells a request that arrived over a CASE session apart,
// for the cluster servers that answer a command over CASE only (the
// connectedhomeip servers' `authMode == Access::AuthMode::kCase`). One
// rule for every caller, so a copy cannot drift (review II,
// R2-P6-sec-1111: a copy that took "a fabric and not PASE" for CASE let a
// group message through).
package caseauth

import (
	"context"

	"github.com/SukramJ/go-fabric/im"
)

// IsCASE reports whether the request behind ctx arrived over a CASE
// session. The bridge stamps the session's fabric and subject on every
// request and marks a PASE session and a group message
// (im.IsPASEFromContext, im.GroupSubjectFromContext); a CASE request is
// one with a fabric and a node subject that is neither.
func IsCASE(ctx context.Context) bool {
	if im.IsPASEFromContext(ctx) {
		return false
	}
	if _, group := im.GroupSubjectFromContext(ctx); group {
		return false
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	node, _ := im.SubjectFromContext(ctx)
	return fabric != 0 && node != 0
}
