// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	"github.com/SukramJ/go-fabric/contract"
)

// afterEmit is an event emitter that runs fn when an event reaches it.
type afterEmit func()

func (f afterEmit) MatterEmitEvent(uint16, uint32, uint32, any, contract.EventPriority) { f() }

// opServer is a washer-like server with a phase and a countdown served.
func opServer(tb testing.TB) *Server {
	tb.Helper()
	zero := uint8(0)
	srv, err := NewServer(Config{
		States: []StateEntry{{ID: StateStopped}, {ID: StateRunning}, {ID: StateError}},
		Phases: []string{"wash"}, CurrentPhase: &zero, CountdownTime: true,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return srv
}

// TestSetAttributesStopsAtASetterThatRefuses: the values are checked
// before any is applied, so a setter refuses only when the server moved
// under the Set — here from a listener or the emitter the Set itself
// calls — and the Set reports the refusal, leaving what it applied before.
func TestSetAttributesStopsAtASetterThatRefuses(t *testing.T) {
	t.Parallel()

	label := "lid open"
	failure := opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateUnableToStartOrResume, ErrorStateDetails: &label}

	t.Run("the error state", func(t *testing.T) {
		t.Parallel()
		srv := opServer(t)
		// The phase list is applied first; its report is where the
		// cluster's error states go away.
		srv.OnAttributeChange(func(contract.Change) { srv.v.errors = &spec.Enum{} })
		err := srv.SetAttributes(map[uint32]any{
			opdef.AttrPhaseList: []string{"rinse"}, opdef.AttrCurrentPhase: uint8(0), opdef.AttrOperationalError: failure,
		})
		if !errors.Is(err, ErrUnknownError) {
			t.Errorf("SetAttributes = %v, want ErrUnknownError", err)
		}
	})

	t.Run("the operational state", func(t *testing.T) {
		t.Parallel()
		srv := opServer(t)
		// The error is applied before the state; its event is where the
		// state list shrinks.
		srv.SetMatterEventEmitter(afterEmit(func() { srv.states = []StateEntry{{ID: StateError}} }))
		err := srv.SetAttributes(map[uint32]any{
			opdef.AttrOperationalError: failure, opdef.AttrOperationalState: uint8(StateRunning),
		})
		if !errors.Is(err, ErrStateNotListed) {
			t.Errorf("SetAttributes = %v, want ErrStateNotListed", err)
		}
	})
}

// TestUintOf: an unsigned value of any width, or what a pointer to one
// points to; null and any other kind are not.
func TestUintOf(t *testing.T) {
	t.Parallel()

	n := uint16(7)
	var nilPtr *uint8
	for _, c := range []struct {
		name string
		in   any
		want uint64
		ok   bool
	}{
		{"nil", nil, 0, false},
		{"uint8", uint8(3), 3, true},
		{"uint64", uint64(1) << 40, 1 << 40, true},
		{"pointer", &n, 7, true},
		{"nil pointer", nilPtr, 0, false},
		{"string", "7", 0, false},
		{"signed", int8(3), 0, false},
	} {
		got, ok := uintOf(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: uintOf = %d, %v; want %d, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestFieldOf: a field of a struct or of a struct a pointer points to,
// found without regard to case; nil for anything else.
func TestFieldOf(t *testing.T) {
	t.Parallel()

	type s struct{ ErrorStateId uint8 } //nolint:revive // the generated types spell an id "Id"
	if got := fieldOf(s{4}, "ErrorStateID"); got != uint8(4) {
		t.Errorf("fieldOf(struct) = %v", got)
	}
	if got := fieldOf(&s{5}, "errorstateid"); got != uint8(5) {
		t.Errorf("fieldOf(pointer) = %v", got)
	}
	if got := fieldOf(s{}, "Missing"); got != nil {
		t.Errorf("fieldOf(missing) = %v", got)
	}
	if got := fieldOf(3, "X"); got != nil {
		t.Errorf("fieldOf(int) = %v", got)
	}
	if got := fieldOf(nil, "X"); got != nil {
		t.Errorf("fieldOf(nil) = %v", got)
	}
}

// TestStringOf: a string, what a *string points to, "" for anything else.
func TestStringOf(t *testing.T) {
	t.Parallel()

	s := "p"
	var nilPtr *string
	for in, want := range map[string]any{"a": "a", "p": &s, "": nilPtr} {
		if got := stringOf(want); got != in {
			t.Errorf("stringOf(%v) = %q, want %q", want, got, in)
		}
	}
	if got := stringOf(3); got != "" {
		t.Errorf("stringOf(int) = %q", got)
	}
}

// TestErrorStateOf: an ErrorStateStruct of any shape that carries an ID.
func TestErrorStateOf(t *testing.T) {
	t.Parallel()

	label := "lid"
	got, ok := errorStateOf(struct {
		ErrorStateID      uint8
		ErrorStateLabel   *string
		ErrorStateDetails string
	}{0x80, &label, "open"})
	if !ok || got != (ErrorState{ID: 0x80, Label: "lid", Details: "open"}) {
		t.Errorf("errorStateOf = %+v, %v", got, ok)
	}
	if _, ok := errorStateOf(struct{ Other uint8 }{}); ok {
		t.Error("a struct without an ID was accepted")
	}
	if _, ok := errorStateOf(nil); ok {
		t.Error("nil was accepted")
	}
}

// TestStringsOf: null is a nil list, a slice of strings (or of interfaces
// holding strings) is the list, anything else is not.
func TestStringsOf(t *testing.T) {
	t.Parallel()

	if got, ok := stringsOf(nil); !ok || got != nil {
		t.Errorf("stringsOf(nil) = %v, %v", got, ok)
	}
	if got, ok := stringsOf([]string{"a", "b"}); !ok || !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("stringsOf([]string) = %v, %v", got, ok)
	}
	if got, ok := stringsOf([]any{"a"}); !ok || !slices.Equal(got, []string{"a"}) {
		t.Errorf("stringsOf([]any) = %v, %v", got, ok)
	}
	if _, ok := stringsOf([]any{"a", 2}); ok {
		t.Error("a list with a non-string was accepted")
	}
	if _, ok := stringsOf([]int{1}); ok {
		t.Error("a list of ints was accepted")
	}
	if _, ok := stringsOf("a"); ok {
		t.Error("a string was accepted")
	}
	if _, ok := stringsOf([1]string{"a"}); ok {
		t.Error("an array was accepted")
	}
}
