// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"github.com/SukramJ/go-fabric/cluster/closure"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
)

// ready puts a fresh ControlServer, which starts in SetupRequired, into
// Stopped as the host's first report does, so MoveTo is accepted
// (connectedhomeip ClosureControlCluster.cpp:599-605 at 6170af84 refuses
// it in SetupRequired).
func ready(s *closure.ControlServer) *closure.ControlServer {
	s.SetMainState(clusterwire.ClosureMainStateStopped)
	return s
}
