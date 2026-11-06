// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matter.js packages/node/test/behaviors/general-diagnostics/
// GeneralDiagnosticsServerTest.ts at db021da0 (#4881), ported by meaning on
// the server: matter.js's createDevice configures deviceTestEnableKey and a
// TriggerServer that supports one trigger, here EnableTestEventTriggers
// with a handler that supports one.

var (
	gdEnableKey = []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
	gdOtherKey  = []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f}
	gdZeroKey   = make([]byte, 16)
)

const gdSupportedTrigger = uint64(0x1234)

// gdDevice is createDevice: the server with key configured (nil: none) and
// a trigger handler that supports gdSupportedTrigger and records it.
func gdDevice(t *testing.T, key []byte) (*core.GeneralDiagnostics, *[]uint64) {
	t.Helper()
	g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
	triggered := &[]uint64{}
	if key != nil {
		if err := g.EnableTestEventTriggers(key, func(_ context.Context, trigger uint64) error {
			if trigger != gdSupportedTrigger {
				return errors.New("unsupported trigger")
			}
			*triggered = append(*triggered, trigger)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return g, triggered
}

func gdStatus(t *testing.T, err error) im.StatusCode {
	t.Helper()
	if err == nil {
		return im.StatusSuccess
	}
	sc, ok := err.(genDiagStatusCoder)
	if !ok {
		t.Fatalf("error %v is not a status", err)
	}
	return sc.MatterStatusCode()
}

func gdTrigger(t *testing.T, g *core.GeneralDiagnostics, key []byte, trigger uint64) im.StatusCode {
	t.Helper()
	_, err := g.MatterInvoke(context.Background(), 0x00, core.TestEventTriggerRequest{EnableKey: key, EventTrigger: trigger})
	return gdStatus(t, err)
}

func gdPayload(t *testing.T, g *core.GeneralDiagnostics, key []byte, count uint16, value uint8) (im.StatusCode, []byte) {
	t.Helper()
	resp, err := g.MatterInvoke(context.Background(), 0x03, map[uint8]any{0: key, 1: uint64(value), 2: uint64(count)})
	if err != nil {
		return gdStatus(t, err), nil
	}
	return im.StatusSuccess, resp.(core.PayloadTestResponse).Payload
}

func gdEnabled(t *testing.T, g *core.GeneralDiagnostics) bool {
	t.Helper()
	v, ok := g.MatterRead(0x0008)
	if !ok {
		t.Fatal("TestEventTriggersEnabled is not served")
	}
	return v.(bool)
}

// TestGeneralDiagnosticsServerTestEventTrigger is the describe
// "TestEventTrigger": every key that is all zero, absent from the device,
// of another length or not the configured one answers ConstraintError
// through the one gate (GeneralDiagnosticsServer.ts #assertEnableKey
// :206-223 at db021da0), an unsupported trigger InvalidCommand.
func TestGeneralDiagnosticsServerTestEventTrigger(t *testing.T) {
	t.Parallel()
	refused := []struct {
		name string
		key  []byte // the device's; nil is none
		req  []byte
	}{
		{"rejects an all-zero key", gdEnableKey, gdZeroKey},
		{"rejects an all-zero key on a device without a key", nil, gdZeroKey},
		{"rejects a mismatched key", gdEnableKey, gdOtherKey},
		{"rejects any key on a device without a key", nil, gdEnableKey},
		{"rejects a key shorter than 16 bytes on the wire", gdEnableKey, gdEnableKey[:8]},
		{"rejects a key longer than 16 bytes on the wire", gdEnableKey, append(bytes.Clone(gdEnableKey), 1)},
		{"does not authenticate a request key that is a prefix of the configured key", gdEnableKey, gdEnableKey[:8]},
		{
			"does not authenticate a prefix that leaves out trailing zero bytes of the configured key",
			[]byte{1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 0},
			[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{"does not authenticate a request key that extends the configured key", gdEnableKey, append(bytes.Clone(gdEnableKey), 1)},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g, triggered := gdDevice(t, c.key)
			if s := gdTrigger(t, g, c.req, gdSupportedTrigger); s != im.StatusConstraintError {
				t.Errorf("status %v, want ConstraintError", s)
			}
			if len(*triggered) != 0 {
				t.Errorf("triggered %v", *triggered)
			}
		})
	}

	t.Run("rejects an all-zero key after the configured key was cleared", func(t *testing.T) {
		t.Parallel()
		// Clearing is configuring the all-zero key, matter.js's "not
		// enabled" (GeneralDiagnosticsServer.ts:143-157 at db021da0).
		g, triggered := gdDevice(t, gdEnableKey)
		if err := g.EnableTestEventTriggers(gdZeroKey, nil); err != nil {
			t.Fatal(err)
		}
		if s := gdTrigger(t, g, gdZeroKey, gdSupportedTrigger); s != im.StatusConstraintError || len(*triggered) != 0 {
			t.Errorf("status %v, triggered %v", s, *triggered)
		}
	})

	t.Run("does not authenticate the 16-byte prefix of an over-long configured key", func(t *testing.T) {
		t.Parallel()
		// The Go key has one input, EnableTestEventTriggers, which refuses
		// an over-long key; the prefix therefore never authenticates.
		g, triggered := gdDevice(t, nil)
		if err := g.EnableTestEventTriggers(append(bytes.Clone(gdEnableKey), 0x11), nil); err == nil {
			t.Fatal("a 17-byte key was configured")
		}
		if s := gdTrigger(t, g, gdEnableKey, gdSupportedTrigger); s != im.StatusConstraintError || len(*triggered) != 0 {
			t.Errorf("status %v, triggered %v", s, *triggered)
		}
	})

	t.Run("answers INVALID_COMMAND to an unsupported trigger with a matching key", func(t *testing.T) {
		t.Parallel()
		g, _ := gdDevice(t, gdEnableKey)
		if s := gdTrigger(t, g, gdEnableKey, 0x5678); s != im.StatusInvalidCommand {
			t.Errorf("status %v, want InvalidCommand", s)
		}
	})

	t.Run("answers INVALID_COMMAND to any trigger on the default implementation", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
		if err := g.EnableTestEventTriggers(gdEnableKey, nil); err != nil {
			t.Fatal(err)
		}
		if s := gdTrigger(t, g, gdEnableKey, gdSupportedTrigger); s != im.StatusInvalidCommand {
			t.Errorf("status %v, want InvalidCommand", s)
		}
	})

	t.Run("executes a supported trigger with a matching key", func(t *testing.T) {
		t.Parallel()
		g, triggered := gdDevice(t, gdEnableKey)
		if s := gdTrigger(t, g, gdEnableKey, gdSupportedTrigger); s != im.StatusSuccess {
			t.Fatalf("status %v", s)
		}
		if len(*triggered) != 1 || (*triggered)[0] != gdSupportedTrigger {
			t.Errorf("triggered %v", *triggered)
		}
	})
}

// TestGeneralDiagnosticsServerTestEventTriggersEnabled is the describe
// "TestEventTriggersEnabled": the attribute follows from the key alone
// (GeneralDiagnosticsServer.ts initialize :143-157 at db021da0).
func TestGeneralDiagnosticsServerTestEventTriggersEnabled(t *testing.T) {
	t.Parallel()

	t.Run("is false without a key", func(t *testing.T) {
		t.Parallel()
		g, _ := gdDevice(t, nil)
		if gdEnabled(t, g) {
			t.Error("enabled without a key")
		}
	})

	t.Run("is true when only a non-zero key is configured", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
		if err := g.EnableTestEventTriggers(gdEnableKey, nil); err != nil {
			t.Fatal(err)
		}
		if !gdEnabled(t, g) {
			t.Error("not enabled with a key and no handler")
		}
	})

	t.Run("refuses to start with a non-zero key that is not 16 bytes", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
		for _, key := range [][]byte{gdEnableKey[:8], append(bytes.Clone(gdEnableKey), 1)} {
			if err := g.EnableTestEventTriggers(key, nil); err == nil {
				t.Errorf("a %d-byte key was configured", len(key))
			}
		}
		if gdEnabled(t, g) {
			t.Error("a refused key enabled the triggers")
		}
	})

	t.Run("starts with the all-zero key of another length", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
		if err := g.EnableTestEventTriggers(make([]byte, 8), nil); err != nil {
			t.Fatalf("an all-zero 8-byte key: %v", err)
		}
		if gdEnabled(t, g) {
			t.Error("an all-zero key enabled the triggers")
		}
	})

	t.Run("lets both test commands accept a matching key when only the key is configured", func(t *testing.T) {
		t.Parallel()
		g, _ := gdDevice(t, gdEnableKey)
		if s := gdTrigger(t, g, gdEnableKey, gdSupportedTrigger); s != im.StatusSuccess {
			t.Errorf("TestEventTrigger %v", s)
		}
		if s, _ := gdPayload(t, g, gdEnableKey, 1, 0); s != im.StatusSuccess {
			t.Errorf("PayloadTestRequest %v", s)
		}
	})
}

// TestGeneralDiagnosticsServerPayloadTestRequest is the describe
// "PayloadTestRequest" (GeneralDiagnosticsServer.ts payloadTestRequest
// :253-298 at db021da0).
func TestGeneralDiagnosticsServerPayloadTestRequest(t *testing.T) {
	t.Parallel()
	g, _ := gdDevice(t, gdEnableKey)
	without, _ := gdDevice(t, nil)
	checks := []struct {
		name  string
		g     *core.GeneralDiagnostics
		key   []byte
		count uint16
		want  im.StatusCode
	}{
		{"rejects any key when test event triggers are disabled", without, gdEnableKey, 1, im.StatusConstraintError},
		{"rejects any key when test event triggers are disabled (all zero)", without, gdZeroKey, 1, im.StatusConstraintError},
		{"rejects a mismatched key", g, gdOtherKey, 1, im.StatusConstraintError},
		{"rejects an all-zero key", g, gdZeroKey, 1, im.StatusConstraintError},
		{"rejects a count above 2048", g, gdEnableKey, 2049, im.StatusConstraintError},
		{"answers RESOURCE_EXHAUSTED when the response does not fit the exchange", g, gdEnableKey, 2048, im.StatusResourceExhausted},
	}
	for _, c := range checks {
		if s, _ := gdPayload(t, c.g, c.key, c.count, 0); s != c.want {
			t.Errorf("%s: %v, want %v", c.name, s, c.want)
		}
	}
	if s, payload := gdPayload(t, g, gdEnableKey, 10, 0xab); s != im.StatusSuccess || !bytes.Equal(payload, bytes.Repeat([]byte{0xab}, 10)) {
		t.Errorf("answers with Count instances of the Value byte: %v %x", s, payload)
	}
	if s, payload := gdPayload(t, g, gdEnableKey, 0, 0xab); s != im.StatusSuccess || len(payload) != 0 {
		t.Errorf("answers with an empty payload for a count of zero: %v %x", s, payload)
	}
}

// TestGeneralDiagnosticsServerTimeSnapshot is the describe "TimeSnapshot":
// PosixTimeMs only with a TimeSynchronization UTC time, and SystemTimeMs
// on the System Time UpTime reads (GeneralDiagnosticsServer.ts systemTime
// :61-85, timeSnapshot :235-251 at db021da0) — also across a suspend, which
// advances the wall clock but not the monotonic one.
func TestGeneralDiagnosticsServerTimeSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := func(t *testing.T, g *core.GeneralDiagnostics) core.TimeSnapshotResponse {
		t.Helper()
		resp, err := g.MatterInvoke(context.Background(), 0x01, nil)
		if err != nil {
			t.Fatal(err)
		}
		return resp.(core.TimeSnapshotResponse)
	}

	t.Run("answers a null PosixTimeMs without Time Synchronization", func(t *testing.T) {
		t.Parallel()
		if p := snapshot(t, core.NewGeneralDiagnostics(0)).PosixTimeMs; p != nil {
			t.Errorf("PosixTimeMs %d", *p)
		}
	})

	t.Run("answers a null PosixTimeMs when UTCTime is null", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(0)
		g.SetUTCClock(func() (time.Time, bool) { return time.Time{}, false })
		if p := snapshot(t, g).PosixTimeMs; p != nil {
			t.Errorf("PosixTimeMs %d", *p)
		}
	})

	t.Run("answers a PosixTimeMs when UTCTime is set", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnostics(0)
		utc := time.UnixMilli(1_700_000_000_123)
		g.SetUTCClock(func() (time.Time, bool) { return utc, true })
		if p := snapshot(t, g).PosixTimeMs; p == nil || *p != uint64(utc.UnixMilli()) {
			t.Errorf("PosixTimeMs %v", p)
		}
	})

	t.Run("reports SystemTimeMs on the same base as UpTime when the host has been up far longer", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(1_000_000, 0))
		g := core.NewGeneralDiagnostics(0)
		g.SetClock(clk)
		agree := func(what string) {
			t.Helper()
			sys := snapshot(t, g).SystemTimeMs
			up, _ := g.MatterRead(0x0002)
			if d := int64(sys) - int64(up.(uint64))*1000; d < 0 || d >= 1000 {
				t.Errorf("%s: SystemTimeMs %d, UpTime %d s", what, sys, up)
			}
		}
		clk.Advance(90 * time.Second)
		agree("after 90 s")
		clk.StepWall(time.Hour)
		agree("after an hour's suspend")
		if sys := snapshot(t, g).SystemTimeMs; sys < uint64((time.Hour + 90*time.Second).Milliseconds()) {
			t.Errorf("SystemTimeMs %d does not count the suspend", sys)
		}
	})
}
