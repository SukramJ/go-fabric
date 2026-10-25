// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestArguments: the argument errors every command reports before it
// opens anything, and the parsers behind them.
func TestArguments(t *testing.T) {
	stderr = &bytes.Buffer{}
	db := t.TempDir() + "/x.db"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "no command"},
		{[]string{"bogus"}, `unknown command "bogus"`},
		{[]string{"--log-level", "loud", "nodes"}, "--log-level"},
		{[]string{"pair"}, "usage: pair"},
		{[]string{"discover", "extra"}, "usage: discover"},
		{[]string{"discover", "--code", "MT:x", "--discriminator", "1"}, "give one"},
		{[]string{"discover", "--discriminator", "4096"}, "out of range"},
		{[]string{"discover", "--code", "12345"}, "invalid pairing code"},
		{[]string{"pair", "--code", "MT:BAD"}, "invalid pairing code"},
		{[]string{"pair", "--code", "12345"}, "invalid pairing code"},
		{[]string{"pair", "--passcode", "12345678", "--discriminator", "1"}, "not a valid setup passcode"},
		{[]string{"pair", "--passcode", "20202021", "--discriminator", "5000"}, "out of range"},
		{[]string{"pair", "--passcode", "20202021", "--node-id", "x"}, "node id"},
		{[]string{"pair", "extra"}, "usage: pair"},
		{[]string{"unpair"}, "usage: unpair"},
		{[]string{"nodes", "x"}, "usage: nodes"},
		{[]string{"node"}, "usage: node"},
		{[]string{"read", "1"}, "usage: read"},
		{[]string{"read", "1", "0/Nope/X"}, `unknown cluster "Nope"`},
		{[]string{"read", "1", "0/OnOff"}, "want <endpoint>/<Cluster>/<element>"},
		{[]string{"read", "1", "x/OnOff/OnOff"}, `endpoint "x"`},
		{[]string{"read", "1", "0/OnOff/Bogus"}, `has no attribute "Bogus"`},
		{[]string{"read", "1", "0/0xFFF1FC00/Bogus"}, "no definition of the cluster"},
		{[]string{"events", "1", "0/OnOff/Bogus"}, "has no event"},
		{[]string{"events", "1", "0/0xFFF1FC00/Bogus"}, "no definition"},
		{[]string{"write", "1", "0/OnOff/OnTime"}, "usage: write"},
		{[]string{"write", "1", "*/OnOff/OnTime", "1"}, "a write path is concrete"},
		{[]string{"write", "1", "1/OnOff/OnTime", `"x"`}, "not a uint16"},
		{[]string{"write", "1", "1/OnOff/OnTime", "1", "--timed", "x"}, "duration"},
		{[]string{"invoke", "1", "*/OnOff/On"}, "a command path is concrete"},
		{[]string{"invoke", "1", "1/OnOff/Bogus"}, `has no command "Bogus"`},
		{[]string{"invoke", "1", "1/0xFFF1FC00/Bogus"}, "no definition"},
		{[]string{"invoke", "1", "1/LevelControl/MoveToLevel", "{}"}, "is required"},
		{[]string{"subscribe", "1"}, "usage: subscribe"},
		{[]string{"subscribe", "1", "1/OnOff/Bogus"}, "has no attribute"},
		{[]string{"group"}, "usage: group"},
		{[]string{"group", "x"}, "usage: group"},
		{[]string{"group", "create"}, "usage: group"},
		{[]string{"group", "create", "0"}, "group id"},
		{[]string{"group", "create", "1", "--keyset", "0"}, "--keyset"},
		{[]string{"group", "create", "1", "--key", "abc"}, "--key"},
		{[]string{"group", "create", "1", "--nodes", "x"}, "list entry"},
		{[]string{"group", "create", "1", "--endpoints", "70000"}, "list entry"},
		{[]string{"group", "send", "1"}, "usage: group"},
		{[]string{"group", "send", "0", "OnOff/On"}, "group id"},
		{[]string{"group", "send", "1", "OnOff"}, "<Cluster>/<Command>"},
		{[]string{"bind", "1", "1"}, "usage: bind"},
		{[]string{"bind", "1", "x", "--group", "1"}, `endpoint "x"`},
		{[]string{"ota"}, "usage: ota"},
		{[]string{"ota", "serve"}, "usage: ota"},
		{[]string{"ota", "serve", "--image", db + ".missing"}, "no such file"},
	} {
		var out bytes.Buffer
		err := run(append([]string{"--db", db}, tc.args...), strings.NewReader(""), &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}

	// The stack's own refusals.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--dcl", "moon", "nodes"}, "--dcl"},
		{[]string{"--attestation", "maybe", "nodes"}, "--attestation"},
		{[]string{"unpair", "0x99"}, "unknown node"},
		{[]string{"unpair", "peer9"}, "unknown node"},
		{[]string{"bind", "1", "1", "--group", "5"}, "unknown node"},
	} {
		err := run(append([]string{"--db", db, "--listen", "127.0.0.1:0"}, tc.args...), strings.NewReader(""), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"--db", db, "--listen", "127.0.0.1:0", "nodes"}, strings.NewReader(""), &out); err != nil || !strings.Contains(out.String(), "no paired nodes") {
		t.Errorf("nodes on an empty database: %v %s", err, out.String())
	}

	qr, err := setup.QRCode(setup.Payload{Passcode: 20202021, Discriminator: 3840, DiscoveryCaps: setup.DiscoveryOnIP})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := parseCode(qr); err != nil || p.code != qr || !strings.Contains(p.what, "long discriminator 3840") {
		t.Errorf("parseCode(%s) = %+v, %v", qr, p, err)
	}
	// --passcode and --discriminator become the QR code controller.Pair
	// takes, which decodes to them.
	if p, err := codeOf(20202021, 3840); err != nil || p.what != "long discriminator 3840" {
		t.Errorf("codeOf = %+v, %v", p, err)
	} else if pc, err := controller.ParsePairingCode(p.code); err != nil || pc.Passcode != 20202021 || pc.Discriminator != 3840 || pc.Manual {
		t.Errorf("codeOf's code %s decodes to %+v, %v", p.code, pc, err)
	}
	if d, err := parseDuration("250ms", time.Second); err != nil || d != 250*time.Millisecond {
		t.Errorf("parseDuration: %v %v", d, err)
	}
	if _, err := parseDuration("-1s", time.Second); err == nil {
		t.Error("a negative duration parsed")
	}
	var f durationFlag
	if f.String() != "" || f.Set("3") != nil || f.String() != "3s" || f.or(time.Hour) != 3*time.Second {
		t.Errorf("durationFlag %v", f)
	}
	if clusterName(6) != "OnOff" || clusterName(0xFFF1FC00) != "0xFFF1FC00" {
		t.Error("clusterName")
	}

	e := &env{stdin: strings.NewReader("yes\n"), stdout: &bytes.Buffer{}, opts: defaultOptions()}
	for mode, want := range map[string]bool{"accept": true, "reject": false} {
		e.opts.attestation = mode
		p, err := attestationPolicy(e)
		if err != nil || p.OnFailure == nil || *p.OnFailure != want {
			t.Errorf("%s: %+v %v", mode, p, err)
		}
	}
	e.opts.attestation = ""
	if p, _ := attestationPolicy(e); p.OnFailure == nil || *p.OnFailure {
		t.Error("a non-terminal stdin does not reject")
	}
	e.opts.attestation = "ask"
	p, _ := attestationPolicy(e)
	if ok, _, _ := p.Decide([]attestation.Finding{{Check: "paa", Message: "unknown"}}); !ok {
		t.Error("ask: yes was not taken")
	}
	if ok, reason, _ := p.Decide(nil); ok || reason == "" {
		t.Error("ask: an empty answer accepted")
	}
	if isTerminal(strings.NewReader("")) {
		t.Error("a reader is a terminal")
	}
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultOptions().register(fs)
	usage(fs)
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "group create") {
		t.Error("usage lacks a command")
	}
}
