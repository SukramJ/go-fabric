// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/secure/setup"
)

// flowFlags is a flags value checkFlags accepts, with --custom-flow v.
func flowFlags(v uint) flags {
	return flags{passcode: 20202021, discriminator: 3840, iterations: 1000, dcl: "off", customFlow: v}
}

// TestCustomFlowFlag (decision 96): --custom-flow 0..2 becomes the node's
// Commissioning.Flow, and only 1 leaves the window to the host
// (WindowOnDemand); any other value stops the daemon before the narrowing
// to the node's uint8 could wrap it.
func TestCustomFlowFlag(t *testing.T) {
	t.Parallel()
	for _, v := range []uint{3, 256, 257} {
		if _, _, err := checkFlags(flowFlags(v)); err == nil || !strings.Contains(err.Error(), "--custom-flow") {
			t.Errorf("--custom-flow %d: checkFlags = %v, want a refusal naming the flag", v, err)
		}
	}
	for v, want := range []node.CommissioningFlow{node.FlowStandard, node.FlowUserIntent, node.FlowCustom} {
		f := flowFlags(uint(v))
		if _, _, err := checkFlags(f); err != nil {
			t.Fatalf("--custom-flow %d: checkFlags = %v", v, err)
		}
		cm := nodeConfig(f, 20202021, 3840, node.TCPConfig{}, &otaApplier{}, nil, slog.New(slog.DiscardHandler)).Commissioning
		if cm.Flow != want || cm.WindowOnDemand != (want == node.FlowUserIntent) {
			t.Errorf("--custom-flow %d: Flow %d, WindowOnDemand %v; want Flow %d, WindowOnDemand %v",
				v, cm.Flow, cm.WindowOnDemand, want, want == node.FlowUserIntent)
		}
	}
}

// flowNode is the daemon's node for --custom-flow v, advertising on a
// recording advertiser, unstarted; the fleet carries the app pipe's
// wiring.
func flowNode(t *testing.T, v uint) (*node.Node, *fleet, *mdns.Noop) {
	t.Helper()
	cm := nodeConfig(flowFlags(v), 20202021, 3840, node.TCPConfig{}, &otaApplier{}, nil, slog.New(slog.DiscardHandler)).Commissioning
	devices, adv := newFleet(), mdns.NewNoop()
	n := testNodeWith(t, devices, filepath.Join(t.TempDir(), "flow.db"), func(cfg *node.Config) {
		cfg.Network.MDNS = node.MDNS{}
		cfg.Advertiser = adv
		cfg.Commissioning.Flow, cfg.Commissioning.WindowOnDemand = cm.Flow, cm.WindowOnDemand
	})
	return n, devices, adv
}

// commissionableMode is the published commissionable record's CM key,
// ok false without such a record.
func commissionableMode(adv *mdns.Noop) (mode uint64, ok bool) {
	services := adv.Active()
	for i := range services {
		svc := &services[i]
		if svc.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		for _, txt := range svc.TXT {
			if txt.Key == "CM" {
				mode, _ = strconv.ParseUint(txt.Value, 10, 8)
			}
		}
		return mode, true
	}
	return 0, false
}

// awaitCommissionable waits for a commissionable record with CM=1.
func awaitCommissionable(t *testing.T, adv *mdns.Noop, when string) {
	t.Helper()
	// The window opens and its record is published before Start or the
	// pipe command returns; no wait is needed.
	if mode, ok := commissionableMode(adv); ok && mode == 1 {
		return
	}
	t.Fatalf("%s: no commissionable record with CM=1 (published: %+v)", when, adv.Active())
}

// TestUserIntentFlowWaitsForThePipe (decision 96): under --custom-flow 1
// the started daemon has no window open and no _matterc record; the app
// pipe's UserIntentCommissioningStart opens its own window, announced
// with CM=1, and is logged applied.
func TestUserIntentFlowWaitsForThePipe(t *testing.T) {
	t.Parallel()
	n, devices, adv := flowNode(t, 1)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if w := n.Window(); w.Open {
		t.Fatalf("--custom-flow 1 after Start: window %+v, want none", w)
	}
	if _, ok := commissionableMode(adv); ok {
		t.Fatalf("--custom-flow 1 after Start: a commissionable record is published: %+v", adv.Active())
	}
	if p, err := n.SetupPayload(); err != nil || p.Flow != node.FlowUserIntent {
		t.Fatalf("SetupPayload = %+v, %v; want Flow User-Intent", p, err)
	}

	var logs bytes.Buffer
	readPipe(devices, strings.NewReader(`{"Name": "UserIntentCommissioningStart"}`), slog.New(slog.NewTextHandler(&logs, nil)))
	if !strings.Contains(logs.String(), "msg=apppipe.applied name=UserIntentCommissioningStart") {
		t.Fatalf("the pipe command was not applied:\n%s", logs.String())
	}
	if w := n.Window(); !w.Open || w.Kind != node.WindowOwn {
		t.Fatalf("after UserIntentCommissioningStart: window %+v, want the node's own", w)
	}
	awaitCommissionable(t, adv, "after UserIntentCommissioningStart")
}

// TestCustomFlowOpensAtStart: --custom-flow 2 only states the flow — the
// started daemon is commissionable at once, and both codes carry flow 2,
// the manual code in its long form.
func TestCustomFlowOpensAtStart(t *testing.T) {
	t.Parallel()
	n, _, adv := flowNode(t, 2)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if w := n.Window(); !w.Open || w.Kind != node.WindowOwn {
		t.Fatalf("--custom-flow 2 after Start: window %+v, want the node's own", w)
	}
	awaitCommissionable(t, adv, "--custom-flow 2 after Start")
	p, err := n.SetupPayload()
	if err != nil {
		t.Fatalf("SetupPayload: %v", err)
	}
	qr, err := setup.DecodeQRCode(p.QR)
	if err != nil {
		t.Fatalf("DecodeQRCode(%s): %v", p.QR, err)
	}
	if p.Flow != node.FlowCustom || qr.CustomFlow != 2 {
		t.Errorf("Flow %d, QR %s flow %d; want 2 in both", p.Flow, p.QR, qr.CustomFlow)
	}
	if len(p.Manual) != 21 {
		t.Errorf("manual code %q: want the 21-digit form a non-standard flow carries", p.Manual)
	}
}

// TestUserIntentCommissioningStartReportsTheNodesError: the pipe command
// answers in the pipe's error form — the node's own error, logged
// apppipe.error — when the window cannot open (here: before Start).
func TestUserIntentCommissioningStartReportsTheNodesError(t *testing.T) {
	t.Parallel()
	_, devices, adv := flowNode(t, 1)
	err := devices.applyPipeCommand(pipeCommand{Name: "UserIntentCommissioningStart"})
	if !errors.Is(err, node.ErrNotStarted) {
		t.Fatalf("UserIntentCommissioningStart before Start = %v, want node.ErrNotStarted", err)
	}
	var logs bytes.Buffer
	readPipe(devices, strings.NewReader(`{"Name": "UserIntentCommissioningStart"}`), slog.New(slog.NewTextHandler(&logs, nil)))
	if !strings.Contains(logs.String(), "msg=apppipe.error name=UserIntentCommissioningStart") {
		t.Fatalf("the refused command was not logged as apppipe.error:\n%s", logs.String())
	}
	if _, ok := commissionableMode(adv); ok {
		t.Fatal("a refused UserIntentCommissioningStart published a commissionable record")
	}
	if err := newFleet().applyPipeCommand(pipeCommand{Name: "UserIntentCommissioningStart"}); err == nil {
		t.Fatal("UserIntentCommissioningStart without a node: nil, want an error")
	}
}
