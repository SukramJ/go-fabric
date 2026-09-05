// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
)

// aggregatorEndpoint is the endpoint the reference daemon's bridged devices
// hang off. It is not a guess: examples/reference-bridge/main.go attaches a
// root PartsList provider returning exactly []uint16{1} and an aggregator
// PartsList provider that lists the assembled bridged endpoints, so 1 is
// the aggregator by construction. The bridged endpoint numbers themselves
// are assigned by the assembler and persisted, so those are discovered
// rather than assumed.
const aggregatorEndpoint uint16 = 1

// testVendorID is the CSA test vendor block the reference daemon advertises
// (examples/reference-bridge/main.go, const testVendorID = 0xFFF1). Reading
// it back over the operational session is the cheapest proof that CASE
// carries application data in both directions, and it is a real
// cross-check: the value has to travel from the daemon's BasicInformation
// cluster through TLV, the Interaction Model and the encrypted session into
// chip-tool's printout.
const testVendorID int64 = 0xFFF1

// TestChipToolCommissionsReferenceBridge is this module's real-commissioner
// guard.
//
// Every other test in the module is go-fabric talking to go-fabric. This one
// puts somebody else's implementation on the far side of the wire — the CSA
// reference commissioner — and walks the whole path a real ecosystem walks:
//
//  1. commission over PASE and complete CASE,
//  2. read an attribute back over the operational session,
//  3. drive the on/off device and observe the state change on both sides,
//  4. leave the device off and tear the fabric down.
//
// The subtests are ordered and share the session on purpose: each one is a
// stage of a single commissioning, not an independent case. A failure in an
// early stage makes the later ones meaningless, which is why they stop
// rather than skip.
func TestChipToolCommissionsReferenceBridge(t *testing.T) {
	chipBin := requireChipTool(t)
	bridgeBin := requireBridgeBinary(t)

	// The daemon's command line is read from the daemon, not asserted here.
	flags := requireBridgeFlags(t, bridgeBin, "db", "listen")
	br := startBridge(t, bridgeBin, flags)

	ctl := newController(t, chipBin, controllerNodeID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// --- 1. PASE + CASE ------------------------------------------------
	out, err := ctl.pair(ctx, t, pairTargetHost, br.info.port, br.info.passcode)
	if err != nil {
		br.dump(t)
		t.Fatalf("commissioning the reference daemon stopped at: %s\n%v", handshakeStage(out), err)
	}
	if !commissioningComplete(out) {
		br.dump(t)
		t.Fatalf("chip-tool exited 0 but reported no completed commissioning; furthest stage: %s\n%s",
			handshakeStage(out), out)
	}
	t.Log("commissioning complete: PASE, AddNOC and CASE all reported success")

	// The device is left off and the fabric removed even when a later stage
	// fails, so a red run does not leave a half-owned daemon behind. The
	// endpoint is not known yet; the closure reads it when it runs.
	var lightEndpoint uint16
	var lightKnown bool
	t.Cleanup(func() {
		teardownCtx, teardownCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer teardownCancel()
		if lightKnown {
			if _, err := ctl.invoke(teardownCtx, t, "onoff", "off", lightEndpoint); err != nil {
				t.Logf("teardown: final off failed: %v", err)
			}
		}
		if _, err := ctl.unpair(teardownCtx, t); err != nil {
			t.Logf("teardown: unpair failed: %v", err)
		}
	})

	// --- 2. read back over the operational session ----------------------
	readOK := t.Run("read/basic-information-vendor-id", func(t *testing.T) {
		out, err := ctl.readAttr(ctx, t, "basicinformation", "vendor-id", 0)
		if err != nil {
			br.dump(t)
			t.Fatalf("read BasicInformation.VendorID: %v", err)
		}
		got, ok := findAttrUint(out, "VendorID")
		if !ok {
			t.Fatalf("VendorID not present in chip-tool's output:\n%s", out)
		}
		if got != testVendorID {
			t.Errorf("VendorID = %d (0x%X), want 0x%X", got, got, testVendorID)
		}
	})
	if !readOK {
		t.Fatal("no usable operational session — the remaining stages would measure nothing")
	}

	// --- 3. find the on/off device --------------------------------------
	discoverOK := t.Run("discover/onoff-endpoint", func(t *testing.T) {
		out, err := ctl.readAttr(ctx, t, "descriptor", "parts-list", aggregatorEndpoint)
		if err != nil {
			br.dump(t)
			t.Fatalf("read aggregator PartsList: %v", err)
		}
		parts := listAfter(out, "PartsList:")
		if len(parts) == 0 {
			t.Fatalf("aggregator PartsList is empty — the daemon bridged no devices:\n%s", out)
		}
		t.Logf("aggregator PartsList: %v", parts)

		for _, ep := range parts {
			epID := uint16(ep)
			srvOut, err := ctl.readAttr(ctx, t, "descriptor", "server-list", epID)
			if err != nil {
				t.Fatalf("read ServerList on endpoint %d: %v", epID, err)
			}
			for _, id := range listAfter(srvOut, "ServerList:") {
				if id == onoff.ClusterID {
					lightEndpoint, lightKnown = epID, true
					t.Logf("OnOff (0x%04X) found on endpoint %d", onoff.ClusterID, epID)
					return
				}
			}
		}
		t.Fatalf("no endpoint in %v hosts OnOff (0x%04X)", parts, onoff.ClusterID)
	})
	if !discoverOK || !lightKnown {
		t.Fatal("no OnOff endpoint — the drive stage would measure nothing")
	}

	// --- 4. drive the device --------------------------------------------
	//
	// Each step asserts on both sides: chip-tool's re-read says the cluster
	// answered, and the daemon's own log says the command reached the device
	// behind it. A cluster server that flips its cache and never calls the
	// device passes the first check and fails the second.
	t.Run("drive/on-toggle-off", func(t *testing.T) {
		// Baseline, so the cycle is reproducible whatever the daemon's
		// starting state was.
		mustInvoke(ctx, t, ctl, br, "off", lightEndpoint)
		mustReadOnOff(ctx, t, ctl, br, lightEndpoint, false)

		mustInvoke(ctx, t, ctl, br, "on", lightEndpoint)
		mustReadOnOff(ctx, t, ctl, br, lightEndpoint, true)
		requireDeviceLog(t, br, "on=true")

		mustInvoke(ctx, t, ctl, br, "toggle", lightEndpoint)
		mustReadOnOff(ctx, t, ctl, br, lightEndpoint, false)
		requireDeviceLog(t, br, "on=false")

		// The device is left off by an explicit off, never by a toggle
		// whose parity depends on the state the previous step believed in.
		mustInvoke(ctx, t, ctl, br, "off", lightEndpoint)
		mustReadOnOff(ctx, t, ctl, br, lightEndpoint, false)
	})
}

// mustInvoke issues an OnOff command and fails the test with both sides'
// output when chip-tool reports an error.
func mustInvoke(ctx context.Context, t *testing.T, ctl *controller, br *bridgeProcess, cmd string, ep uint16) {
	t.Helper()
	if _, err := ctl.invoke(ctx, t, "onoff", cmd, ep); err != nil {
		br.dump(t)
		t.Fatalf("invoke onoff %s on endpoint %d: %v", cmd, ep, err)
	}
}

// mustReadOnOff re-reads OnOff.OnOff and asserts the expected state.
func mustReadOnOff(ctx context.Context, t *testing.T, ctl *controller, br *bridgeProcess, ep uint16, want bool) {
	t.Helper()
	out, err := ctl.readAttr(ctx, t, "onoff", "on-off", ep)
	if err != nil {
		br.dump(t)
		t.Fatalf("read OnOff on endpoint %d: %v", ep, err)
	}
	got, ok := findAttrBool(out, "OnOff")
	if !ok {
		t.Fatalf("OnOff not present in chip-tool's output:\n%s", out)
	}
	if got != want {
		t.Fatalf("OnOff on endpoint %d = %v, want %v", ep, got, want)
	}
}

// requireDeviceLog asserts the daemon's own log records the device being
// driven, not just the cluster answering.
//
// The reference daemon's OnOff server logs every applied state as
// `light.set` with the new value (examples/reference-bridge/fleet.go,
// onOffServer.apply). The suite matches the slog text-handler rendering,
// which is `msg=light.set … on=<bool>`. This is the ground-truth half of
// the assertion pair: without it, a cluster server that updated its own
// attribute and never touched the device would look correct.
func requireDeviceLog(t *testing.T, br *bridgeProcess, wantField string) {
	t.Helper()
	logs := br.snapshotStderr()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "light.set") && strings.Contains(line, wantField) {
			return
		}
	}
	t.Errorf("the daemon logged no light.set with %s — the command may not have reached the device\n--- daemon log ---\n%s",
		wantField, logs)
}
