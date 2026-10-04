// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import "context"

// AuthorityFunc answers whether the subject of a request holds privilege
// at (endpoint, clusterID): [StatusSuccess] when it does, the denial status
// otherwise.
type AuthorityFunc func(endpoint uint16, clusterID uint32, privilege uint8) StatusCode

type authorityCtxKey struct{}

// WithAuthority stamps the access check of the request ctx belongs to.
// [HandleInvokeRequest] stamps it before a command runs; a test of a
// cluster server stamps its own.
func WithAuthority(ctx context.Context, fn AuthorityFunc) context.Context {
	return context.WithValue(ctx, authorityCtxKey{}, fn)
}

// AuthorityAt reports whether the subject of the request ctx belongs to
// holds privilege at (endpoint, clusterID). A cluster server calls it when a
// command needs more than the command's own privilege for part of its
// request — Groupcast's JoinGroup with a key, say, which requires
// Administer although the command needs Manage. Without a stamped check it
// denies: a privilege nobody could verify is not held.
//
// Mirrors matter.js `session.authorityAt(AccessLevel, location)`
// (packages/protocol/src/action/server/AccessControl.ts), which
// GroupcastServer #requireAdmin evaluates.
func AuthorityAt(ctx context.Context, endpoint uint16, clusterID uint32, privilege uint8) StatusCode {
	fn, ok := ctx.Value(authorityCtxKey{}).(AuthorityFunc)
	if !ok || fn == nil {
		return StatusUnsupportedAccess
	}
	return fn(endpoint, clusterID, privilege)
}
