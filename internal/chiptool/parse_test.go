// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"strings"
	"testing"
)

// referenceBridgeBanner is the daemon's startup block, captured verbatim
// from a real `./bin/reference-bridge --db <tmp> --listen :0` run. It is a
// recording, not a reconstruction: the ephemeral port below is the one the
// daemon actually bound, which is the property the suite depends on.
const referenceBridgeBanner = `
  go-fabric reference bridge
  --------------------------------------------------------------
  listening on     [::]:56082
  bridged devices  2
  vendor/product   0xFFF1 / 0x8001  (CSA TEST identity — not shippable)

  discriminator    3840
  setup passcode   20202021
  manual code      34970112332
  QR payload       MT:-24J0AFN00KA0648G00

  pair with, e.g.:
    chip-tool pairing onnetwork-long 1 20202021 3840
  --------------------------------------------------------------

`

// TestParseBanner pins the suite's reading of the daemon's pairing block.
//
// It is the one part of this suite that can be verified without chip-tool,
// and it guards the seam that breaks silently: if the daemon reformats its
// banner, the suite would otherwise fail as an unexplained startup timeout
// far from the change that caused it.
//
// The last line of the banner also contains "20202021 3840" as part of an
// example command, which is exactly the kind of near-match a looser regexp
// would pick up — so the case doubles as the check that the parse is
// anchored on the labels rather than on the numbers.
func TestParseBanner(t *testing.T) {
	t.Parallel()

	info, ok := parseBanner(referenceBridgeBanner)
	if !ok {
		t.Fatal("parseBanner rejected a captured reference-bridge banner")
	}
	if info.listenAddr != "[::]:56082" {
		t.Errorf("listenAddr = %q, want %q", info.listenAddr, "[::]:56082")
	}
	if info.port != 56082 {
		t.Errorf("port = %d, want 56082 (the daemon's effective bind, not the --listen default)", info.port)
	}
	if info.passcode != 20202021 {
		t.Errorf("passcode = %d, want 20202021", info.passcode)
	}
	if info.discriminator != 3840 {
		t.Errorf("discriminator = %d, want 3840", info.discriminator)
	}
}

// TestParseBannerIncomplete is the negative control for [TestParseBanner]:
// a banner missing any one field must be rejected rather than half-parsed,
// because a half-parsed banner turns into a chip-tool call against port 0.
func TestParseBannerIncomplete(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no listen line":   "  discriminator    3840\n  setup passcode   20202021\n",
		"no passcode line": "  listening on     [::]:56082\n  discriminator    3840\n",
		"no discriminator": "  listening on     [::]:56082\n  setup passcode   20202021\n",
		"empty":            "",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseBanner(in); ok {
				t.Errorf("parseBanner accepted an incomplete banner:\n%s", in)
			}
		})
	}
}

// referenceBridgeUsage is `./bin/reference-bridge --help`, captured
// verbatim. Go's flag package writes it to stderr and exits non-zero, which
// is why requireBridgeFlags ignores the exit status.
const referenceBridgeUsage = `Usage of ./bin/reference-bridge:
  -advertise
    	publish the commissionable mDNS record (false = no discovery, direct address only) (default true)
  -db string
    	SQLite file holding fabrics, credentials and endpoint numbers (default "reference-bridge.db")
  -discriminator uint
    	12-bit commissioning discriminator (default 3840)
  -listen string
    	UDP listen address for Matter traffic (default ":5540")
  -log-level string
    	debug, info, warn or error (default "info")
`

// TestHelpFlagDiscovery pins the flag-name extraction the suite uses instead
// of hard-coding the daemon's command line. The description lines are
// tab-indented continuations and must not be mistaken for flags.
func TestHelpFlagDiscovery(t *testing.T) {
	t.Parallel()

	found := make(map[string]bool)
	for _, m := range reHelpFlag.FindAllStringSubmatch(referenceBridgeUsage, -1) {
		found[m[1]] = true
	}
	for _, want := range []string{"advertise", "db", "discriminator", "listen", "log-level"} {
		if !found[want] {
			t.Errorf("flag %q not discovered in the captured usage text", want)
		}
	}
	if len(found) != 5 {
		t.Errorf("discovered %d flags (%v), want exactly the 5 in the captured usage",
			len(found), sortedKeys(found))
	}
}

// chipToolReadOutput reconstructs chip-tool's rendering of the reads this
// suite performs.
//
// It is NOT a capture — there is no chip-tool on the machine this was
// written on. Every character is derived from the pinned connectedhomeip
// commit's own printf calls rather than from a remembered sample, which is
// the only way a reconstruction is worth anything:
//
//   - the Linux logging backend writes, per line, a colour escape, then
//     "[<secs>.<millis>] ", then "[<pid>:<tid>] ", then "[<module>] ", then
//     the message, then the reset escape
//     (src/platform/logging/impl/Stdio.cpp:53, :63, :72, :76, :78).
//   - the chipTool module renders as TOO, hence "[TOO] "
//     (src/lib/support/logging/Constants.h:41, `X(chipTool, "TOO")`).
//   - inside the message, ComputePrefix writes two spaces per indent level,
//     the label, ":" and one space
//     (examples/chip-tool/commands/clusters/DataModelLogger.h:268-282).
//   - a list prints "<n> entries" then one "[i]: <value>" line per element
//     at indent+1 (:122-131); a cluster-id list appends " (<name>)" to each
//     element (:167-176); booleans print TRUE/FALSE (:43).
//
// The colour escapes matter: that backend emits them unconditionally, with
// no TERM or NO_COLOR check anywhere in the file, so stripANSI is the
// mechanism the parsers depend on rather than a precaution. They are
// included here so the test exercises it.
var chipToolReadOutput = "" +
	"\x1b[0;32m[1750000000.000] [100:101] [DMG] ReportDataMessage =\x1b[0m\n" +
	"\x1b[0;32m[1750000000.001] [100:101] [TOO] Endpoint: 1 Cluster: 0x0000_001D Attribute 0x0000_0003 DataVersion: 12\x1b[0m\n" +
	"\x1b[0;32m[1750000000.002] [100:101] [TOO]   PartsList: 2 entries\x1b[0m\n" +
	"\x1b[0;32m[1750000000.003] [100:101] [TOO]     [1]: 2\x1b[0m\n" +
	"\x1b[0;32m[1750000000.004] [100:101] [TOO]     [2]: 3\x1b[0m\n" +
	"\n" +
	"\x1b[0;32m[1750000000.005] [100:101] [TOO]   ServerList: 3 entries\x1b[0m\n" +
	"\x1b[0;32m[1750000000.006] [100:101] [TOO]     [1]: 3 (Identify)\x1b[0m\n" +
	"\x1b[0;32m[1750000000.007] [100:101] [TOO]     [2]: 6 (On/Off)\x1b[0m\n" +
	"\x1b[0;32m[1750000000.008] [100:101] [TOO]     [3]: 29 (Descriptor)\x1b[0m\n" +
	"\n" +
	"\x1b[0;32m[1750000000.009] [100:101] [TOO]   VendorID: 65521\x1b[0m\n" +
	"\x1b[0;32m[1750000000.010] [100:101] [TOO]   OnOff: TRUE\x1b[0m\n"

// TestChipToolOutputParsers exercises the parsers against that
// reconstruction. It is a regression guard on this module's regexps, not
// evidence about chip-tool: see the comment on [chipToolReadOutput].
func TestChipToolOutputParsers(t *testing.T) {
	t.Parallel()

	out := stripANSI(chipToolReadOutput)

	parts := listAfter(out, "PartsList:")
	if len(parts) != 2 || parts[0] != 2 || parts[1] != 3 {
		t.Errorf("listAfter(PartsList) = %v, want [2 3]", parts)
	}

	// The decoded cluster name after each id must not be mistaken for the id.
	servers := listAfter(out, "ServerList:")
	if len(servers) != 3 || servers[0] != 3 || servers[1] != 6 || servers[2] != 29 {
		t.Errorf("listAfter(ServerList) = %v, want [3 6 29]", servers)
	}

	if v, ok := findAttrUint(out, "VendorID"); !ok || v != 0xFFF1 {
		t.Errorf("findAttrUint(VendorID) = %d, %v; want 65521, true", v, ok)
	}
	if _, ok := findAttrUint(out, "ProductID"); ok {
		t.Error("findAttrUint reported a value for an attribute that is not in the output")
	}

	if v, ok := findAttrBool(out, "OnOff"); !ok || !v {
		t.Errorf("findAttrBool(OnOff) = %v, %v; want true, true", v, ok)
	}

	// stripANSI must not swallow payload: the escapes are the only thing it
	// is allowed to remove.
	if got, want := len(out), len(chipToolReadOutput); got >= want {
		t.Errorf("stripANSI removed nothing: %d bytes in, %d out", want, got)
	}
	for _, must := range []string{"[TOO]", "PartsList: 2 entries", "OnOff: TRUE"} {
		if !strings.Contains(out, must) {
			t.Errorf("stripANSI dropped %q from the stream", must)
		}
	}
}

// TestHandshakeStage pins the distinction the guard rests on: chip-tool's
// PASE line is a substring of its CASE line, and neither means the
// commissioning finished. The literals are the ones at the pinned
// connectedhomeip commit (PairingCommand.cpp:489-539).
func TestHandshakeStage(t *testing.T) {
	t.Parallel()

	const (
		paseOnly    = "[TOO] Pairing Success\n[TOO] PASE establishment successful\n"
		throughCASE = paseOnly +
			"[TOO] Secure Pairing Success\n[TOO] CASE establishment successful\n"
		full = throughCASE + "[TOO] Device commissioning completed with success\n"
	)

	cases := []struct {
		name     string
		out      string
		complete bool
		stage    string
	}{
		{"nothing", "", false, "PASE not established"},
		{"pase only", paseOnly, false, "PASE established, CASE not reached"},
		{"through case", throughCASE, false, "CASE established, commissioning did not complete"},
		{"full", full, true, "commissioning complete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := commissioningComplete(tc.out); got != tc.complete {
				t.Errorf("commissioningComplete = %v, want %v", got, tc.complete)
			}
			if got := handshakeStage(tc.out); got != tc.stage {
				t.Errorf("handshakeStage = %q, want %q", got, tc.stage)
			}
		})
	}
}
