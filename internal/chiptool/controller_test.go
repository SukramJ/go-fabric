// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// chipToolBinEnv pins a non-PATH chip-tool location. CI extracts the
	// binary from the pinned chip-cert-bins image into a cache directory
	// that is not on PATH, so this is the normal way the suite finds it.
	chipToolBinEnv = "GOFABRIC_CHIPTOOL_BIN"

	// kvsBaseEnv overrides the directory the per-controller chip-tool
	// key-value store is created under. A snap-confined chip-tool can only
	// write under $HOME/snap/chip-tool/common; leaving files behind there
	// also lets a CI agent salvage them.
	kvsBaseEnv = "GOFABRIC_CHIPTOOL_KVS_BASE"
)

// pairTargetHost is the loopback address handed to chip-tool's
// `pairing already-discovered`.
//
// IPv6 deliberately. The CSA chip-cert-bins builds are ipv6only
// (INET_CONFIG_ENABLE_IPV4=0) and reject an IPv4 literal while parsing the
// argument, with CHIP_ERROR_INVALID_ARGUMENT (0x2F) — the address never
// reaches the network stack. The reference daemon binds dual-stack (its
// --listen default and the ":0" this suite passes both resolve to [::]), so
// ::1 always reaches it.
const pairTargetHost = "::1"

// controllerNodeID is the operational node ID chip-tool assigns the daemon
// during commissioning; every later read, invoke and unpair addresses the
// daemon by it.
const controllerNodeID uint64 = 0x1234

// chipToolTimeout bounds a single chip-tool invocation. A full
// commissioning — PASE, AddNOC, CASE Sigma1-3, operational discovery — can
// legitimately take tens of seconds on a loaded arm64 runner amid MRP
// retransmits, and a tighter ceiling kills chip-tool before it can either
// finish or print the protocol error that explains itself, converting a
// slow-but-correct run into an uninterpretable timeout.
const chipToolTimeout = 120 * time.Second

// controller is a chip-tool commissioner identity bound to its own on-disk
// key-value store.
type controller struct {
	bin        string
	storageDir string
	nodeID     uint64
}

// requireChipTool resolves the chip-tool binary, skipping the test when it
// is absent. A skip is the right outcome on a developer machine: chip-tool
// does not run on macOS hosts at all, and the guard that matters runs in CI.
func requireChipTool(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(chipToolBinEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("chip-tool: %s=%q not usable: %v", chipToolBinEnv, p, err)
		}
		return p
	}
	p, err := exec.LookPath("chip-tool")
	if err != nil {
		t.Skipf("chip-tool not on PATH; extract it with `make chiptool-extract` or set %s", chipToolBinEnv)
	}
	return p
}

// newController creates the controller's storage directory and returns it.
//
// The directory must exist before the first chip-tool call:
// ExamplePersistentStorage fails Init with 0x000000AF against a missing
// directory, and chip-tool does not create its own storage root.
func newController(t *testing.T, bin string, nodeID uint64) *controller {
	t.Helper()

	base := os.Getenv(kvsBaseEnv)
	if base == "" {
		base = t.TempDir()
	}
	dir := filepath.Join(base, fmt.Sprintf("gofabric-chiptool-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create chip-tool storage directory %s: %v", dir, err)
	}
	return &controller{bin: bin, storageDir: dir, nodeID: nodeID}
}

// run executes chip-tool and returns its merged, ANSI-stripped output.
//
// --storage-directory is APPENDED after the command arguments, never
// prepended: chip-tool reads its first argument as the cluster or command
// set, so a leading global flag fails with "Unknown cluster or command set"
// before anything else happens.
func (c *controller) run(parent context.Context, t *testing.T, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, chipToolTimeout)
	defer cancel()

	full := append([]string{}, args...)
	full = append(full, "--storage-directory", c.storageDir)

	cmd := exec.CommandContext(ctx, c.bin, full...) //nolint:gosec // c.bin is resolved by requireChipTool
	// TERM/NO_COLOR are set for builds whose logging backend honours them.
	// The one chip-cert-bins ships does not: src/platform/logging/impl/
	// Stdio.cpp writes the colour escape and the reset unconditionally, with
	// no TERM or NO_COLOR check anywhere in the file. stripANSI below is the
	// mechanism the parsers actually depend on; these two are a belt.
	cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1")

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := stripANSI(buf.String())
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out, fmt.Errorf("chip-tool %s timed out after %s: %w\n--- output ---\n%s",
				strings.Join(args, " "), chipToolTimeout, err, out)
		}
		return out, fmt.Errorf("chip-tool %s: %w\n--- output ---\n%s", strings.Join(args, " "), err, out)
	}
	return out, nil
}

// pair commissions the daemon over the full PASE → AddNOC → CASE flow.
//
// `already-discovered` with an explicit address needs no mDNS round trip
// for the PASE stage, which keeps the commissioning failure surface down to
// the handshake itself. --bypass-attestation-verifier is passed because the
// question this guard answers is whether the session establishes, not
// whether chip-tool's compiled-in trust store accepts the daemon's test
// attestation chain; those are separate findings and conflating them makes
// a red run ambiguous. (The daemon presents the CSA test chain from
// attestation.BuildTestChain and its README claims the bypass is not
// needed — that claim is untested here.)
func (c *controller) pair(ctx context.Context, t *testing.T, addr string, port int, passcode uint32) (string, error) {
	t.Helper()
	return c.run(
		ctx, t,
		"pairing", "already-discovered",
		fmt.Sprintf("0x%X", c.nodeID),
		strconv.FormatUint(uint64(passcode), 10),
		addr, strconv.Itoa(port),
		"--bypass-attestation-verifier", "true",
	)
}

// unpair removes the chip-tool side of the fabric.
func (c *controller) unpair(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()
	return c.run(ctx, t, "pairing", "unpair", fmt.Sprintf("0x%X", c.nodeID))
}

// readAttr reads one attribute over the operational session. cluster and
// attr are chip-tool's own slugs ("basicinformation", "vendor-id").
func (c *controller) readAttr(ctx context.Context, t *testing.T, cluster, attr string, endpointID uint16) (string, error) {
	t.Helper()
	return c.run(
		ctx, t,
		cluster, "read", attr,
		fmt.Sprintf("0x%X", c.nodeID),
		strconv.FormatUint(uint64(endpointID), 10),
	)
}

// invoke issues a cluster command over the operational session.
func (c *controller) invoke(ctx context.Context, t *testing.T, cluster, cmd string, endpointID uint16) (string, error) {
	t.Helper()
	return c.run(
		ctx, t,
		cluster, cmd,
		fmt.Sprintf("0x%X", c.nodeID),
		strconv.FormatUint(uint64(endpointID), 10),
	)
}

// --- output parsing -----------------------------------------------------
//
// chip-tool's output is unstructured text. Every shape matched below was
// read out of the connectedhomeip tree at the pinned chip-cert-bins commit
// rather than inferred from a sample run:
//
//   - `[TOO]` is the log tag of the chipTool module
//     (src/lib/support/logging/Constants.h:41, `X(chipTool, "TOO")`).
//     Anchoring on it keeps a value from being lifted out of an unrelated
//     log line that happens to contain "Name: 42".
//   - a value line is `<2 spaces per indent><Label>: <value>`
//     (examples/chip-tool/commands/clusters/DataModelLogger.h:278,
//     ComputePrefix).
//   - booleans render as TRUE / FALSE, not true / false
//     (DataModelLogger.h:43).
//   - a list renders as `<Label>: <n> entries` followed by one
//     `[<index>]: <value>` line per element (DataModelLogger.h:129).
//     Cluster-id lists — ServerList is one — append the decoded name:
//     `[2]: 6 (On/Off)` (DataModelLogger.h:174-175, LogClusterId), which is
//     why the item regexp captures the leading integer and ignores the rest
//     of the line.

var (
	reAttrUint = regexp.MustCompile(`\[TOO\]\s+([A-Za-z][A-Za-z0-9]*):\s+(-?\d+)`)
	reAttrBool = regexp.MustCompile(`\[TOO\]\s+([A-Za-z][A-Za-z0-9]*):\s+(TRUE|FALSE)`)
	reListItem = regexp.MustCompile(`\[\d+\]:\s+(\d+)`)
)

// chip-tool prints three different success lines during a commissioning and
// only the last of them means the flow finished. All three were read out of
// the connectedhomeip tree AT THE PINNED COMMIT — the tag in the Makefile's
// CHIP_CERT_BINS_IMAGE — not from a newer checkout and not from convention
// (examples/chip-tool/commands/pairing/PairingCommand.cpp at
// 6feac778f196483b6355d35fc529f183b293b71f):
//
//	:489  "Secure Pairing Success"
//	:490  "CASE establishment successful"   — OnStatusUpdate
//	:503  "Pairing Success"
//	:504  "PASE establishment successful"   — OnPairingComplete
//	:539  "Device commissioning completed with success" — OnCommissioningComplete
//
// The distinction is the whole point of this guard. "Pairing Success" is
// printed when PASE alone has finished; a run that establishes PASE and then
// dies in AddNOC or CASE prints it and exits non-zero. Worse, it is a
// substring of "Secure Pairing Success", so a naive Contains cannot even
// tell the PASE line from the CASE line. Only the OnCommissioningComplete
// marker says the operational session is usable.
const (
	markerPASE          = "PASE establishment successful"
	markerCASE          = "CASE establishment successful"
	markerCommissioning = "Device commissioning completed with success"
)

// commissioningComplete reports whether the whole commissioning finished.
func commissioningComplete(out string) bool {
	return strings.Contains(out, markerCommissioning)
}

// handshakeStage names the furthest stage chip-tool reported, so a failed
// commissioning says where it stopped instead of only that it did.
func handshakeStage(out string) string {
	switch {
	case strings.Contains(out, markerCommissioning):
		return "commissioning complete"
	case strings.Contains(out, markerCASE):
		return "CASE established, commissioning did not complete"
	case strings.Contains(out, markerPASE):
		return "PASE established, CASE not reached"
	default:
		return "PASE not established"
	}
}

// findAttrUint returns the first integer value printed for the named
// attribute.
func findAttrUint(out, name string) (int64, bool) {
	for _, m := range reAttrUint.FindAllStringSubmatch(out, -1) {
		if strings.EqualFold(m[1], name) {
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// findAttrBool returns the first TRUE/FALSE value printed for the named
// attribute.
func findAttrBool(out, name string) (bool, bool) {
	for _, m := range reAttrBool.FindAllStringSubmatch(out, -1) {
		if strings.EqualFold(m[1], name) {
			return m[2] == "TRUE", true
		}
	}
	return false, false
}

// listAfter parses one of chip-tool's printed list attributes — the
// `[idx]: <n>` lines that follow a "<Name>:" header — and returns the
// values. The block ends at the first blank line, which is how chip-tool
// separates one attribute's rendering from the next.
func listAfter(out, marker string) []uint32 {
	idx := strings.Index(out, marker)
	if idx < 0 {
		return nil
	}
	block := out[idx+len(marker):]
	if end := strings.Index(block, "\n\n"); end >= 0 {
		block = block[:end]
	}
	var ids []uint32
	for _, m := range reListItem.FindAllStringSubmatch(block, -1) {
		if n, err := strconv.ParseUint(m[1], 10, 32); err == nil {
			ids = append(ids, uint32(n))
		}
	}
	return ids
}

// stripANSI removes ECMA-48 colour escape sequences, so a runner that reads
// TERM differently than expected cannot break a substring match.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7E) {
				j++
			}
			if j < len(s) {
				i = j
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
