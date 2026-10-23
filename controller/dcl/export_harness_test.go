// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"net"
	"testing"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// SetHarnessNode installs the node the commission tests commission
// (harness_node_test.go, package dcl_test).
func SetHarnessNode(f func(t *testing.T, chain *secattestation.Chain) *net.UDPAddr) { harnessNode = f }
