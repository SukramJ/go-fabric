// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/dcl"
)

// TestMovedForwardersDelegate holds the deprecated function forwarders of
// moved.go to the moved package: each gives the result dcl.<Name> gives
// for the same input.
func TestMovedForwardersDelegate(t *testing.T) {
	t.Parallel()

	t.Run("IsNotFound", func(t *testing.T) {
		t.Parallel()
		for _, err := range []error{
			nil,
			errors.New("other"),
			&dcl.ResponseError{Code: dcl.CodeNotFound},
			fmt.Errorf("wrapped: %w", &dcl.ResponseError{Code: dcl.CodeNotFound}),
			&dcl.ResponseError{Code: dcl.CodeNotFound + 1},
		} {
			if got, want := IsNotFound(err), dcl.IsNotFound(err); got != want {
				t.Errorf("IsNotFound(%v) = %v, dcl.IsNotFound = %v", err, got, want)
			}
		}
		if !IsNotFound(&dcl.ResponseError{Code: dcl.CodeNotFound}) {
			t.Error("IsNotFound(not-found ResponseError) = false, want true")
		}
	})

	t.Run("NewClient", func(t *testing.T) {
		t.Parallel()
		const base = "https://ledger.example"
		got, want := NewClient(base), dcl.NewClient(base)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("NewClient(%q) = %+v, dcl.NewClient = %+v", base, got, want)
		}
		if got.BaseURL != base {
			t.Errorf("NewClient(%q).BaseURL = %q", base, got.BaseURL)
		}
	})

	t.Run("VerifyImage", func(t *testing.T) {
		t.Parallel()
		log := slog.New(slog.DiscardHandler)
		img := []byte("not an OTA image")
		gotH, gotErr := VerifyImage(bytes.NewReader(img), SoftwareVersion{}, log)
		wantH, wantErr := dcl.VerifyImage(bytes.NewReader(img), dcl.SoftwareVersion{}, log)
		if gotErr == nil || wantErr == nil {
			t.Fatalf("VerifyImage(garbage) error = %v, dcl.VerifyImage = %v; want both non-nil", gotErr, wantErr)
		}
		if gotErr.Error() != wantErr.Error() {
			t.Errorf("VerifyImage error %q, dcl.VerifyImage %q", gotErr, wantErr)
		}
		if !reflect.DeepEqual(gotH, wantH) {
			t.Errorf("VerifyImage header %+v, dcl.VerifyImage %+v", gotH, wantH)
		}
	})
}
