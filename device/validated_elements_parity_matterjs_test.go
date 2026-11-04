// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// illegalIDServer is a host server that lists the attributes it serves.
type illegalIDServer struct {
	hostServer
	attrs []uint32
}

func (l *illegalIDServer) MatterAttributes() []uint32 { return l.attrs }

// TestValidatedElementsHostedParityMatterJS ports matter.js
// packages/node/test/behavior/cluster/ValidatedElementsTest.ts:655-668 at
// 862ad75f, "fails endpoint initialization for a hosted cluster with an
// illegal ID": a device whose host server serves the attribute 0xFFF15000
// — suffix 0x5000, above the 0x4FFF an attribute suffix may take — is not
// built, and the error names the id; the same server with a legal
// attribute is.
func TestValidatedElementsHostedParityMatterJS(t *testing.T) {
	t.Parallel()
	illegal := &illegalIDServer{hostServer: hostServer{id: 0xFFF1_FC01}, attrs: []uint32{0x0000, 0xFFF1_5000}}
	_, err := NewOnOffLight(testOptions("lamp"), HostServer(illegal))
	if !errors.Is(err, ErrConfiguration) || !errors.Is(err, spec.ErrIllegalID) {
		t.Fatalf("err = %v, want ErrConfiguration wrapping spec.ErrIllegalID", err)
	}
	if !strings.Contains(err.Error(), "attribute 0xFFF15000") || !strings.Contains(err.Error(), "invalid attribute ID 0xfff15000") {
		t.Errorf("%q does not name the attribute", err)
	}
	legal := &illegalIDServer{hostServer: hostServer{id: 0xFFF1_FC01}, attrs: []uint32{0x0000, 0xFFF1_4FFF}}
	if _, err := NewOnOffLight(testOptions("lamp"), HostServer(legal)); err != nil {
		t.Errorf("legal ids refused: %v", err)
	}
	if _, err := NewOnOffLight(testOptions("lamp"), HostServer(&hostServer{id: 0xFFF1_0001})); !errors.Is(err, spec.ErrIllegalID) {
		t.Errorf("illegal cluster id: %v, want spec.ErrIllegalID", err)
	}
}
