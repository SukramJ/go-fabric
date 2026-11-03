// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The group-messaging proof of phase 30 over the suite's interactive
// chip-tool; connectedhomeip at the harness pin (6170af84) is the
// authority on how chip-tool configures and sends a group command.

const (
	clusterOnOff = 0x0006

	// proofGroupID and proofKeySetID are the group and the key set the
	// group leg configures on both sides; KeySetID 0 is the IPK's
	// (groupcast-cluster.xml JoinGroup KeySetID min 1). chip-tool installs
	// its own test groups 0x0101-0x0103 with key sets 0x01A1-0x01A3 into
	// every commissioner (CHIPCommand.cpp:539, src/lib/support/
	// TestGroupData.h:45-50 at the harness pin), so the leg stays clear of
	// them.
	proofGroupID  = 0x0E01
	proofKeySetID = 0x0E11
	// proofEpochKey is the key set's one epoch key, 16 bytes
	// (GroupDataProvider::EpochKey::kLengthBytes).
	proofEpochKey = "hex:d0d1d2d3d4d5d6d7d8d9dadbdcdddedf"
)

// testGroupMessaging proves group messaging against chip-tool: the
// controller's own group state is configured with chip-tool's
// GroupSettings commands (examples/chip-tool/commands/group/Commands.h:
// AddGroup :85-125, BindKeySet :190-220, AddKeySet :265-308 at the
// harness pin), the daemon joins the group through Groupcast JoinGroup
// with the assumptions of ADR 0010 — the shared IanaAddr FF05::FA, the
// auxiliary ACL — and a multicast OnOff Toggle to the group node id
// 0xFFFFFFFFFFFF<group> (ModelCommand.cpp:29-37 SendGroupCommand) toggles
// the daemon's light, which a unicast read then shows.
func testGroupMessaging(t *testing.T, f *fixture) {
	t.Helper()
	if has, candidates := lanIPv6(); !has {
		t.Skipf("skipped, %s: group messages are IPv6 multicast (FF05::FA) and no up multicast interface of this host has IPv6 (%v)", classHarness, candidates)
	}
	light, ok := f.endpointWith(dtOnOffLight, clusterOnOff)
	if !ok {
		t.Fatal("no endpoint of the On/Off Light device type serves OnOff")
	}
	// The controller side: the group on the IanaAddr policy (addressPolicy
	// 0, MulticastAddrPolicyEnum kIanaAddr), its key set, the binding.
	f.mustRun(t, fmt.Sprintf("groupsettings add-group gofabric-proof %d --addressPolicy 0", proofGroupID))
	f.mustRun(t, fmt.Sprintf("groupsettings add-keysets %d 0 0 %s", proofKeySetID, proofEpochKey))
	f.mustRun(t, fmt.Sprintf("groupsettings bind-keyset %d %d", proofGroupID, proofKeySetID))
	// The device side: Groupcast JoinGroup on the root with the same key,
	// the auxiliary ACL granting the group Operate on the light, IanaAddr.
	f.mustRun(t, fmt.Sprintf("groupcast join-group %d [%d] %d %s 0 --Key %s --UseAuxiliaryACL 1 --McastAddrPolicy 0",
		proofGroupID, light, proofKeySetID, f.node(), proofEpochKey))

	before, isBool := f.read(t, "onoff", "on-off", light).(bool)
	if !isBool {
		t.Fatalf("OnOff on endpoint %d is not a bool", light)
	}
	a := f.run(t, fmt.Sprintf("onoff toggle 0xFFFFFFFFFFFF%04X %d", proofGroupID, light))
	if a.failed() || answerError(a) != "" {
		t.Fatalf("group toggle failed: %v\n%s", a.Results, a.logText())
	}
	if !strings.Contains(a.logText(), "Sending command to group") {
		t.Errorf("chip-tool did not send the toggle as a group command:\n%s", a.logText())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if now, _ := f.read(t, "onoff", "on-off", light).(bool); now != before {
			t.Logf("the group toggle moved OnOff on endpoint %d from %v to %v", light, before, now)
			return
		}
		if time.Now().After(deadline) {
			f.br.dump(t)
			t.Fatalf("OnOff on endpoint %d still %v 10 s after the group toggle to group 0x%04X", light, before, proofGroupID)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
