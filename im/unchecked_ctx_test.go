// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "context"

// unchecked is the context of a request the access check grants without
// an ACL — a PASE session's implicit grant. The tests that exercise
// decoding, dispatch and response shape run under it, since a dispatcher
// without an [ACLChecker] no longer grants a request of any other session
// (review II, R2-P6-sec-0560).
func unchecked() context.Context { return WithAuthModePASE(context.Background()) }
