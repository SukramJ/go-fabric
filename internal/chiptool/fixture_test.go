// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture is one commissioned reference daemon plus an interactive chip-tool
// session on its fabric — the shared state every leg of the broad suite
// (TestChipToolSuite) runs against. Commissioning once and reusing one CASE
// session is what keeps a few hundred chip-tool operations inside minutes.
type fixture struct {
	t    *testing.T
	ctx  context.Context
	tool chipTool
	br   *bridgeProcess
	ctl  *controller
	s    *interactive

	// topology is what the daemon reports about itself: endpoint id →
	// device types and server clusters, read from the Descriptors.
	topology map[uint16]*endpointInfo
}

// endpointInfo is one endpoint as its Descriptor describes it.
type endpointInfo struct {
	id          uint16
	deviceTypes map[uint32]uint16 // device type → revision
	servers     []uint32
	parts       []uint16
}

// has reports whether the endpoint serves cluster.
func (e *endpointInfo) has(cluster uint32) bool { return slices.Contains(e.servers, cluster) }

// newFixture starts the daemon with its control hook, commissions it with a
// one-shot chip-tool, starts the interactive session and reads the topology.
func newFixture(ctx context.Context, t *testing.T) *fixture {
	t.Helper()
	tool := resolveChipTool(t)
	bridgeBin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bridgeBin, "db", "listen", "app-pipe", "enable-key")
	ctl := newController(t, tool.bin, controllerNodeID)
	// The FIFO and the database live in the controller's directory, which
	// in the image harness is the directory the container shares.
	br := startBridgeWith(t, bridgeBin, flags, bridgeOptions{
		dbPath:    filepath.Join(ctl.storageDir, "dut.db"),
		appPipe:   filepath.Join(ctl.storageDir, "app.fifo"),
		enableKey: chipTestEnableKey,
	})

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
	f := &fixture{t: t, ctx: ctx, tool: tool, br: br, ctl: ctl}
	f.s = ctl.startInteractive(t)
	f.readTopology(t)
	return f
}

// node is the commissioned node id as chip-tool's arguments spell it.
func (f *fixture) node() string { return fmt.Sprintf("0x%X", f.ctl.nodeID) }

// run sends one command line through the interactive session.
func (f *fixture) run(t *testing.T, line string) *imAnswer {
	t.Helper()
	a, err := f.s.exec(f.ctx, line)
	if err != nil {
		f.br.dump(t)
		t.Fatalf("chip-tool %q: %v", line, err)
	}
	return a
}

// mustRun runs a command that has to succeed, and returns its results.
func (f *fixture) mustRun(t *testing.T, line string) []imResult {
	t.Helper()
	a := f.run(t, line)
	if a.failed() || answerError(a) != "" {
		t.Fatalf("chip-tool %q failed: %v\n--- chip-tool log ---\n%s", line, a.Results, a.logText())
	}
	return a.Results
}

// answerError returns the first IM error in an answer other than the
// trailing FAILURE marker: "UNSUPPORTED_ATTRIBUTE", "CONSTRAINT_ERROR", …
func answerError(a *imAnswer) string {
	for _, r := range a.Results {
		if s, ok := r["error"].(string); ok && s != "FAILURE" {
			return s
		}
	}
	return ""
}

// read reads one attribute (chip-tool slugs) and returns its value.
func (f *fixture) read(t *testing.T, cluster, attr string, ep uint16) any {
	t.Helper()
	res := f.mustRun(t, fmt.Sprintf("%s read %s %s %d", cluster, attr, f.node(), ep))
	for _, r := range res {
		if v, ok := r["value"]; ok {
			return v
		}
	}
	t.Fatalf("%s read %s on endpoint %d returned no value: %v", cluster, attr, ep, res)
	return nil
}

// readByID reads attributes by numeric id and returns them keyed by
// attribute id. Errors on individual paths are returned separately.
func (f *fixture) readByID(t *testing.T, cluster uint32, attrs string, ep uint16) (values map[uint32]any, errs map[uint32]string) {
	t.Helper()
	a := f.run(t, fmt.Sprintf("any read-by-id 0x%X %s %s %d", cluster, attrs, f.node(), ep))
	values, errs = map[uint32]any{}, map[uint32]string{}
	for _, r := range a.Results {
		id, ok := asInt(r["attributeId"])
		if !ok {
			continue
		}
		if e, ok := r["error"].(string); ok {
			errs[uint32(id)] = e
			continue
		}
		values[uint32(id)] = r["value"]
	}
	return values, errs
}

// sortedEndpoints lists the topology's endpoint ids ascending.
func (f *fixture) sortedEndpoints() []uint16 {
	out := make([]uint16, 0, len(f.topology))
	for id := range f.topology {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// readTopology walks the Descriptors: the root's PartsList names every
// endpoint (full-family), each endpoint's DeviceTypeList, ServerList and
// PartsList describe it.
func (f *fixture) readTopology(t *testing.T) {
	t.Helper()
	f.topology = map[uint16]*endpointInfo{}
	rootParts, _ := asInts(f.read(t, "descriptor", "parts-list", 0))
	ids := make([]uint16, 0, 1+len(rootParts))
	ids = append(ids, 0)
	for _, p := range rootParts {
		ids = append(ids, uint16(p))
	}
	for _, id := range ids {
		values, errs := f.readByID(t, 0x001D, "0,1,3", id)
		if len(errs) > 0 {
			t.Fatalf("Descriptor on endpoint %d: %v", id, errs)
		}
		ep := &endpointInfo{id: id, deviceTypes: map[uint32]uint16{}}
		if list, ok := values[0].([]any); ok {
			for _, e := range list {
				dt, _ := field(e, 0)
				rev, _ := field(e, 1)
				d, _ := asInt(dt)
				r, _ := asInt(rev)
				ep.deviceTypes[uint32(d)] = uint16(r)
			}
		}
		srv, _ := asInts(values[1])
		for _, s := range srv {
			ep.servers = append(ep.servers, uint32(s))
		}
		parts, _ := asInts(values[3])
		for _, p := range parts {
			ep.parts = append(ep.parts, uint16(p))
		}
		f.topology[id] = ep
	}
	var b strings.Builder
	for _, id := range f.sortedEndpoints() {
		ep := f.topology[id]
		fmt.Fprintf(&b, "\n  endpoint %2d: device types %v, %d server clusters", id, ep.deviceTypes, len(ep.servers))
	}
	t.Logf("daemon topology (from its Descriptors):%s", b.String())
}
