// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestReferenceControllerTrust: --trust production (the DCL snapshot
// only) does not trust the harness's chain, which ends in a CSA test PAA;
// the default, development, does; the
// snapshot's fetch time is in the debug log; --trust-dir loads another
// set; an unknown mode is refused. Not parallel.
func TestReferenceControllerTrust(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tl.runIn("", "--trust", "bogus", "nodes"); err == nil {
		t.Fatal("--trust bogus accepted")
	}
	// Production: the chain's test PAA is not trusted.
	out, err := tl.runIn("n\n", "--attestation", "ask", "--trust", "production", "pair", "--code", manual)
	if err == nil || !strings.Contains(out, "PaaNotTrusted") {
		t.Fatalf("production: %v\n%s", err, out)
	}
	// Development: the chain is trusted; the test declaration's
	// CertificationTypeTest is the only finding.
	out, err = tl.runIn("n\n", "--attestation", "ask", "pair", "--code", manual)
	if err == nil || strings.Contains(out, "PaaNotTrusted") || !strings.Contains(out, "CertificationTypeTest") {
		t.Fatalf("development: %v\n%s", err, out)
	}
	tl.run("--log-level", "debug", "pair", "--code", manual, "--node-id", "0x4321")
	log := stderr.(*bytes.Buffer).String()
	if !strings.Contains(log, "controller.dcl_snapshot") || !strings.Contains(log, "from=embedded") || !strings.Contains(log, "fetched=20") {
		t.Fatalf("no snapshot line in the debug log:\n%s", log)
	}
	tl.run("--trust-dir", t.TempDir(), "--log-level", "debug", "nodes")
	if log := stderr.(*bytes.Buffer).String(); !strings.Contains(log, "paas=0") {
		t.Fatalf("--trust-dir: \n%s", log)
	}
}
