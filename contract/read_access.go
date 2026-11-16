// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// ReadAccessDeclarer is implemented by a cluster server that admits a
// request's read of an attribute only under a condition of its own,
// beyond the access control list — a datastore whose content is one
// fabric's, say. The dispatcher asks MatterReadAllowed as part of the
// access check of a read and of every subscription report, before the
// server's MatterRead or MatterReadFiltered runs, and only for a read a
// request is behind (ctx carries the accessing fabric, [AccessingFabric];
// a local read without one is not asked).
//
// A refusal is answered as the access control list's refusal is: a fully
// concrete path (endpoint, cluster and attribute named) is answered
// UNSUPPORTED_ACCESS, and the attribute is left out of a wildcard read or
// report without a status. The server's own read is never called for a
// refused attribute, so nothing of its value — not even that a read of
// it failed — reaches the requester. The global attributes (AttributeList,
// FeatureMap, …) are asked too; a server that serves them to everyone
// answers true for them.
//
// MatterReadAllowed answers from memory, as MatterRead does, and must be
// safe for concurrent use.
type ReadAccessDeclarer interface {
	MatterReadAllowed(ctx context.Context, attrID uint32) bool
}
