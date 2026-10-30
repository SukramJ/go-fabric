// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// profileDump is the daemon's --dump-surface report with --profile
// profiles and, when schedules is set, --thermostat-schedules.
func profileDump(t *testing.T, profiles string, schedules bool) surface.Report {
	t.Helper()
	dir := t.TempDir()
	f := flags{
		dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "profile-test", salt: "go-fabric-ref-salt",
		iterations: 1000, passcode: 20202021, discriminator: 3840, advertise: true,
		profiles: profiles, thermostatSchedules: schedules,
	}
	var out bytes.Buffer
	if err := dumpSurface(context.Background(), f, slog.New(slog.DiscardHandler), 20202021, 3840, node.TCPConfig{}, &out); err != nil {
		t.Fatalf("--profile %q: %v", profiles, err)
	}
	var rep surface.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// profileRows are the decisions of the built-in profile policies.
func profileRows(rep surface.Report) []surface.Decision {
	var out []surface.Decision
	for _, d := range rep.Decisions {
		if strings.HasPrefix(d.Policy, "profile:") {
			out = append(out, d)
		}
	}
	return out
}

// TestDaemonProfileAppleHome: the shipped register's one row is a single
// warning decision on the thermostat's MSCH under --profile apple-home,
// and removes nothing — the served surface is the one without the
// profile.
func TestDaemonProfileAppleHome(t *testing.T) {
	base := profileDump(t, "", true)
	rep := profileDump(t, "apple-home", true)
	if !slices.Equal(rep.Profiles, []string{"apple-home"}) || rep.LevelOrigin != "pin" || rep.Level != surface.PinLevel() {
		t.Fatalf("head: profiles %v, level %s from %s", rep.Profiles, rep.Level, rep.LevelOrigin)
	}
	rows := profileRows(rep)
	if len(rows) != 1 {
		t.Fatalf("%d profile decisions, want 1: %+v", len(rows), rows)
	}
	d := rows[0]
	if d.Policy != "profile:apple-home" || d.Verdict != surface.Warn || d.Cluster != 0x0201 || d.Kind != surface.KindFeature || d.ID != 0x80 ||
		!strings.HasPrefix(d.Reason, "warning (Reported) profile apple-home: ") {
		t.Fatalf("decision %+v", d)
	}
	if rep.Effective.Digest() != base.Effective.Digest() {
		t.Fatal("the profile changed the served surface")
	}
	// The warning takes the place of MSCH's silent keep row.
	if rep.Policies == base.Policies || len(rep.Decisions) != len(base.Decisions) {
		t.Fatalf("digest moved %v; decisions %d against %d", rep.Policies != base.Policies, len(rep.Decisions), len(base.Decisions))
	}
	// Without MSCH the row names no served element: no decision.
	if rows := profileRows(profileDump(t, "apple-home", false)); len(rows) != 0 {
		t.Fatalf("decisions without MSCH: %+v", rows)
	}
}

func TestProfileFlag(t *testing.T) {
	_, err := surfacePolicies(flags{profiles: "apple-home,nope"})
	if !errors.Is(err, surface.ErrUnknownProfile) || !strings.HasPrefix(err.Error(), "--profile: ") ||
		!strings.Contains(err.Error(), profileList()) {
		t.Fatalf("unknown profile: %v", err)
	}
	p, err := surfacePolicies(flags{profiles: " apple-home , alexa,"})
	if err != nil || !slices.Equal(p.Profiles, []string{"apple-home", "alexa"}) || p.LevelOrigin() != "pin" {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = surfacePolicies(flags{profiles: "apple-home", specLevel: "1.4"})
	if err != nil || p.EffectiveLevel() != "1.4" || p.LevelOrigin() != "explicit" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := surfacePolicies(flags{specLevel: "0.9", profiles: "apple-home"}); !strings.HasPrefix(err.Error(), "--spec-level: ") {
		t.Fatalf("the level is named first: %v", err)
	}
	if !strings.Contains(profileList(), "apple-home") {
		t.Fatal(profileList())
	}
}
