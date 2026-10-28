// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// The node's reported session intervals (Config.Sessions): matter.js
// ServerNetworkRuntime.ts:357-363 spreads sessions.intervals into the
// SessionManager's sessionParameters, which the PASE and CASE responders
// report, and SessionManager.ts:58-67 refuses an active threshold above
// 65535 ms.

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

func TestResponderSessionParamsCarryIntervals(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		in                *SessionIntervals
		idle, active      uint32
		threshold         uint16
		advertisedSAT     time.Duration
		advertisedIdleSII time.Duration
	}{
		"nil is the defaults":     {nil, 500, 300, 4000, 4 * time.Second, 500 * time.Millisecond},
		"zero fields are default": {&SessionIntervals{Active: 200 * time.Millisecond}, 500, 200, 4000, 4 * time.Second, 500 * time.Millisecond},
		"all set":                 {&SessionIntervals{Idle: 5 * time.Second, Active: time.Second, ActiveThreshold: 65535 * time.Millisecond}, 5000, 1000, 65535, 65535 * time.Millisecond, 5 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := responderSessionParams(bridge.TCPConfig{}, 0, tc.in)
			if p.SessionIdleInterval != tc.idle || p.SessionActiveInterval != tc.active || p.SessionActiveThreshold != tc.threshold {
				t.Fatalf("intervals %d/%d/%d, want %d/%d/%d", p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold, tc.idle, tc.active, tc.threshold)
			}
			if a := tc.in.advertised(); a.ActiveThreshold != tc.advertisedSAT || a.Idle != tc.advertisedIdleSII {
				t.Errorf("advertised %+v", a)
			}

			sessions := operational.NewManager(testStore(t))
			ca, err := buildCaseAdapter(sessions, newCaseIdentities(slog.Default(), nil), p, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			if got := ca.SnapshotResponder().SessionParameters(); got == nil || got.SessionIdleInterval != tc.idle || got.SessionActiveThreshold != tc.threshold {
				t.Errorf("CASE responder parameters %+v", got)
			}
			vc, err := spake2.NewVerifierContext(20202021, []byte("SPAKE2P Key Salt"), 1000)
			if err != nil {
				t.Fatal(err)
			}
			pa, err := buildPaseAdapterFromContext(sessions, &RootClusters{}, vc, []byte("SPAKE2P Key Salt"), 1000, p)
			if err != nil {
				t.Fatal(err)
			}
			_, respBytes, err := pa.ProcessPBKDFParamRequest(pbkdfParamRequest())
			if err != nil {
				t.Fatal(err)
			}
			resp, err := spake2.DecodePBKDFParamResponse(respBytes)
			if err != nil {
				t.Fatal(err)
			}
			if resp.ResponderSessionParams == nil || resp.ResponderSessionParams.SessionActiveInterval != tc.active {
				t.Errorf("PASE responder parameters %+v", resp.ResponderSessionParams)
			}
		})
	}
}

func TestSessionIntervalsValidate(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		in   *SessionIntervals
		want string
	}{
		"nil":                  {nil, ""},
		"zero":                 {&SessionIntervals{}, ""},
		"threshold at bound":   {&SessionIntervals{ActiveThreshold: 65535 * time.Millisecond}, ""},
		"threshold over bound": {&SessionIntervals{ActiveThreshold: 65536 * time.Millisecond}, "Sessions.ActiveThreshold"},
		"negative idle":        {&SessionIntervals{Idle: -time.Millisecond}, "Sessions.Idle"},
		"sub-millisecond":      {&SessionIntervals{Active: 1500 * time.Microsecond}, "Sessions.Active"},
		"idle over uint32":     {&SessionIntervals{Idle: maxSessionInterval + time.Millisecond}, "Sessions.Idle"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := tc.in.validate()
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("errors %v, want none", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.want) {
				t.Fatalf("errors %v, want one naming %s", errs, tc.want)
			}
		})
	}
}
