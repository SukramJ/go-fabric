// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package embedded is a fixture: a server that embeds a type whose
// MatterQuiesce stops the timer it arms; the method is promoted.
package embedded

import "fixture/gaps/twotypes"

// Server embeds First.
type Server struct{ *twotypes.First }
