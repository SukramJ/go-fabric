// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr_test

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/thread"
	"github.com/SukramJ/go-fabric/thread/otbr"
)

// liveDataset is the dataset of connectedhomeip's
// TestThreadBorderRouterManagementCluster.cpp:303-308 at 6170af84
// (Channel 11, "OpenThread", PAN ID 0xc115).
var liveDataset = []byte{
	0x0e, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x0b, 0x35, 0x06,
	0x00, 0x04, 0x00, 0x1f, 0xff, 0xe0, 0x02, 0x08, 0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef, 0x07,
	0x08, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00, 0x05, 0x10, 0xb7, 0x28, 0x08, 0x04, 0x85, 0xcf,
	0xc5, 0x25, 0x7f, 0x68, 0x4c, 0x54, 0x9d, 0x6a, 0x57, 0x5e, 0x03, 0x0a, 0x4f, 0x70, 0x65, 0x6e, 0x54,
	0x68, 0x72, 0x65, 0x61, 0x64, 0x01, 0x02, 0xc1, 0x15, 0x04, 0x10, 0xcb, 0x13, 0x47, 0xeb, 0x0c, 0xd4,
	0xb3, 0x5c, 0xd1, 0x42, 0xda, 0x5e, 0x6d, 0xf1, 0x8b, 0x88, 0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

// TestLiveOTBR runs the adapter against a real otbr-agent — the virtual
// one of .github/workflows/otbr-virtual.yml (an otbr-agent on the
// simulation RCP, as connectedhomeip's CI builds it, tests.yaml:327-359 at
// 6170af84), or a lab border router — named by GOFABRIC_OTBR_URL
// ("http://127.0.0.1:8081"). Skipped without it. The border router must
// have no active dataset: the test forms chip's test network, reads it
// back, and reverts it.
func TestLiveOTBR(t *testing.T) {
	raw := os.Getenv("GOFABRIC_OTBR_URL")
	if raw == "" {
		t.Skip("GOFABRIC_OTBR_URL names no border router")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	capability, err := otbr.Probe(ctx, u.Hostname(), port, 5*time.Second, nil)
	if err != nil || capability == nil {
		t.Fatalf("probe: %+v, %v", capability, err)
	}
	t.Logf("capability: key format %s, diagnostics %s", capability.KeyFormat, capability.DiagnosticsAPI)
	c, err := otbr.NewClient(otbr.Config{Host: u.Hostname(), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "live OTBR", ThreadVersion: 5, PollInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := br.BorderAgentID(ctx); err != nil || len(id) != 16 {
		t.Fatalf("border agent id %x, %v", id, err)
	}
	if active, err := br.Dataset(ctx, false); err != nil || active != nil {
		t.Fatalf("the border router must start without an active dataset: %x, %v", active, err)
	}
	attach, attachCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer attachCancel()
	if err := br.SetActiveDataset(attach, liveDataset); err != nil {
		t.Fatalf("activation: %v", err)
	}
	active, err := br.Dataset(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	view, err := thread.NewDatasetView(active)
	if err != nil {
		t.Fatal(err)
	}
	if name, err := view.NetworkName(); err != nil || name != "OpenThread" {
		t.Errorf("network name %q, %v", name, err)
	}
	d, err := br.Diagnostics(ctx)
	if err != nil || !d.Role.Attached() {
		t.Errorf("diagnostics %+v, %v", d, err)
	}
	t.Logf("role %s, rloc16 %#04x, own record %v, mesh %d", d.Role, d.Rloc16, d.Self != nil, len(d.Mesh))
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatalf("revert: %v", err)
	}
	// ot-br-posix at main has no DELETE route for the active dataset, so the
	// revert leaves the dataset in place and the interface disabled.
	if d, err := br.Diagnostics(ctx); err != nil || d.Role.Attached() {
		t.Errorf("after the revert: role %s, %v, want disabled", d.Role, err)
	}
	active, err = br.Dataset(ctx, false)
	t.Logf("after the revert: active dataset %d bytes, %v", len(active), err)
}
