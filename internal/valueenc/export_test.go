// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc

// ForgetUnexpectedPanics takes n back from the tripwire's count, for the
// test that provokes undeclared panics on purpose.
func ForgetUnexpectedPanics(n int64) { unexpectedPanics.Add(-n) }
