// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modeselect_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// TestSetAttributesRefusesEveryAttribute: every attribute is the
// ModeSource's, so a value for any of them is ErrNotHeld; an empty map is
// nothing to set.
func TestSetAttributesRefusesEveryAttribute(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})
	if err := srv.SetAttributes(nil); err != nil {
		t.Errorf("SetAttributes(nil) = %v, want nil", err)
	}
	err := srv.SetAttributes(map[uint32]any{modeselect.AttrCurrentMode: uint8(1)})
	if !errors.Is(err, spec.ErrNotHeld) {
		t.Fatalf("SetAttributes(CurrentMode) = %v, want ErrNotHeld", err)
	}
	if !strings.Contains(err.Error(), "0x0003") {
		t.Errorf("error %q does not name the attribute", err)
	}
}

// TestHandleReplacesChangeToMode: a host handler replaces the server's
// own handling, nil hands it back, and a command the cluster does not
// define is ErrNotAccepted.
func TestHandleReplacesChangeToMode(t *testing.T) {
	t.Parallel()

	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})
	ctx := context.Background()

	var got any
	if err := srv.Handle(modeselect.CmdChangeToMode, func(_ context.Context, fields any) (any, error) {
		got = fields
		return nil, nil
	}); err != nil {
		t.Fatalf("Handle(ChangeToMode) = %v", err)
	}
	// Mode 42 is not supported; the host handler runs before that check.
	if _, err := srv.MatterInvoke(ctx, modeselect.CmdChangeToMode, modeselect.ChangeToModeRequest{NewMode: 42}); err != nil {
		t.Fatalf("MatterInvoke with a host handler = %v", err)
	}
	if got == nil || len(src.changed) != 0 {
		t.Errorf("handler saw %v, port saw %v; want the handler only", got, src.changed)
	}

	if err := srv.Handle(modeselect.CmdChangeToMode, nil); err != nil {
		t.Fatalf("Handle(ChangeToMode, nil) = %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, modeselect.CmdChangeToMode, modeselect.ChangeToModeRequest{NewMode: 42}); err == nil {
		t.Error("after nil the server's own check should refuse mode 42")
	}

	if err := srv.Handle(0x07, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x07) = %v, want ErrNotAccepted", err)
	}
}

// TestEmitRefusesEveryEvent: ModeSelect defines no event.
func TestEmitRefusesEveryEvent(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})
	if err := srv.Emit(1, 0x00, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit = %v, want ErrNotEmitted", err)
	}
}

// TestSetBeforeWriteInstallsAVeto: the veto can be installed and removed;
// the attributes being read-only, MatterWrite refuses before it.
func TestSetBeforeWriteInstallsAVeto(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})
	calls := 0
	srv.SetBeforeWrite(func(context.Context, uint32, any) error {
		calls++
		return errors.New("vetoed")
	})
	if err := srv.MatterWrite(context.Background(), modeselect.AttrCurrentMode, uint8(1)); err == nil {
		t.Fatal("MatterWrite of a read-only attribute succeeded")
	}
	if calls != 0 {
		t.Errorf("veto ran %d times, want 0 for a read-only attribute", calls)
	}
	srv.SetBeforeWrite(nil)
}

// TestChangeToModeRejectsMalformedPayloads covers every refusal of the
// payload normalisation, each INVALID_COMMAND with its own message.
func TestChangeToModeRejectsMalformedPayloads(t *testing.T) {
	t.Parallel()

	var nilReq *modeselect.ChangeToModeRequest
	tests := []struct {
		name   string
		fields any
		want   string
	}{
		{"nil request pointer", nilReq, "carried no fields"},
		{"missing NewMode", map[uint8]any{}, "missing the mandatory NewMode"},
		{"NewMode of the wrong type", map[uint8]any{modeselect.ChangeToModeFieldNewMode: "one"}, "want a uint8"},
		{"unknown payload type", 42, "expected ChangeToModeRequest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := coffeeSource()
			srv := modeselect.NewServer(modeselect.Config{Source: src})
			_, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode, tt.fields)
			if err == nil {
				t.Fatal("MatterInvoke accepted a malformed payload")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			var sc im.StatusCodeError
			if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusInvalidCommand {
				t.Errorf("err %v is not INVALID_COMMAND", err)
			}
			if len(src.changed) != 0 {
				t.Errorf("port saw %v, want no call", src.changed)
			}
		})
	}

	// A pointer to a request is accepted as the value.
	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})
	if _, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode,
		&modeselect.ChangeToModeRequest{NewMode: 1}); err != nil {
		t.Errorf("MatterInvoke with a request pointer = %v", err)
	}
}
