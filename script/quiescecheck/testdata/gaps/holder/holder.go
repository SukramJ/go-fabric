// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package holder is a fixture: a server that holds another package's
// timer helper and declares no MatterQuiesce.
package holder

import "fixture/gaps/helper"

// Server holds the throttle.
type Server struct{ quiet *helper.Throttle }

// Set arms the throttle.
func (s *Server) Set() { s.quiet.Arm() }
