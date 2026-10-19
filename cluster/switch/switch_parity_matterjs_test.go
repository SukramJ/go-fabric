// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/switch/SwitchServerTest.ts
// at 862ad75f: the Switch server's validators, debounce, the latching and
// momentary press state machines with and without long and multi press, the
// action switch, and resetState.
//
// The harness, translated (SwitchServerTest.ts:13-155):
//
//   - MockEndpoint.createWith(SwitchServer.with(...)) is [New] with those
//     features on a clocktest.Fake (MockTime);
//   - device.set({ switch: { ... } }) is the setter of each key, the
//     settings first and the position last, as one matter.js transaction
//     commits them before the position's reactor runs;
//   - createEventCatcher records, from its creation on, the server's events
//     (through the emitter the bridge injects) and every change of
//     currentPosition and rawPosition (the observe seam), in delivery order;
//   - holdingSwitchLock is the hold/release seam of the switch state lock:
//     a write the actor makes is the holder's own and is decided at once,
//     which is where matter.js decides it too (the reactor runs when the
//     holder commits, before the lock is free);
//   - MockTime.macrotask and the waits for a currentPosition$Changed are
//     no-ops: the Go server delivers before an input or a timer returns.
//
// The expectations — positions, delays and event sequences — are the
// matter.js file's, never the Go server's.

package switchserver

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// device is a MockEndpoint carrying one Switch server.
type device struct {
	t   *testing.T
	s   *Server
	clk *clocktest.Fake
	rec *recorder
}

// recorder is createEventCatcher's array; nil entries until catch.
type recorder struct {
	on     bool
	events []string
}

func (r *recorder) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	if !r.on {
		return
	}
	r.events = append(r.events, eventName(data))
}

func eventName(data any) string {
	switch v := data.(type) {
	case swdef.SwitchLatchedEvent:
		return latched(v.NewPosition)
	case swdef.InitialPressEvent:
		return initialPress(v.NewPosition)
	case swdef.LongPressEvent:
		return longPress(v.NewPosition)
	case swdef.ShortReleaseEvent:
		return shortRelease(v.PreviousPosition)
	case swdef.LongReleaseEvent:
		return longRelease(v.PreviousPosition)
	case swdef.MultiPressOngoingEvent:
		return multiPressOngoing(v.NewPosition, v.CurrentNumberOfPressesCounted)
	case swdef.MultiPressCompleteEvent:
		return multiPressComplete(v.PreviousPosition, v.TotalNumberOfPressesCounted)
	}
	return fmt.Sprintf("unknown %T", data)
}

// The captured entries, by matter.js event name.
func latched(n uint8) string      { return fmt.Sprintf("switchLatched{newPosition: %d}", n) }
func initialPress(n uint8) string { return fmt.Sprintf("initialPress{newPosition: %d}", n) }
func longPress(n uint8) string    { return fmt.Sprintf("longPress{newPosition: %d}", n) }
func shortRelease(p uint8) string { return fmt.Sprintf("shortRelease{previousPosition: %d}", p) }
func longRelease(p uint8) string  { return fmt.Sprintf("longRelease{previousPosition: %d}", p) }

func multiPressOngoing(n, c uint8) string {
	return fmt.Sprintf("multiPressOngoing{newPosition: %d, currentNumberOfPressesCounted: %d}", n, c)
}

func multiPressComplete(p, c uint8) string {
	return fmt.Sprintf("multiPressComplete{previousPosition: %d, totalNumberOfPressesCounted: %d}", p, c)
}

func cur(o, n uint8) string { return fmt.Sprintf("currentPosition$Changed %d->%d", o, n) }
func raw(o, n uint8) string { return fmt.Sprintf("rawPosition$Changed %d->%d", o, n) }

// newDevice is MockEndpoint.createWith(SwitchServer.with(features...)).
func newDevice(t *testing.T, features Feature) *device {
	t.Helper()
	clk := clocktest.New(time.Unix(0, 0))
	s, err := New(Config{Features: features, Clock: clk, Endpoint: 1})
	if err != nil {
		t.Fatalf("New(features 0x%02X): %v", features, err)
	}
	d := &device{t: t, s: s, clk: clk, rec: &recorder{}}
	s.SetMatterEventEmitter(d.rec)
	s.observe = func(kind changeKind, o, n uint8) {
		if !d.rec.on {
			return
		}
		switch kind {
		case changeCurrentPosition:
			d.rec.events = append(d.rec.events, cur(o, n))
		case changeRawPosition:
			d.rec.events = append(d.rec.events, raw(o, n))
		case changeEvent:
		}
	}
	return d
}

// catch is createEventCatcher (SwitchServerTest.ts:13-36).
func (d *device) catch() *recorder {
	d.rec.on = true
	return d.rec
}

// expect is expect(events).deep.equals([...]).
func (r *recorder) expect(t *testing.T, want ...string) {
	t.Helper()
	if !slices.Equal(r.events, want) {
		t.Fatalf("events:\n got  %q\n want %q", r.events, want)
	}
}

// filter is events.filter(({ name }) => ...) on the entry names.
func (r *recorder) filter(keep func(name string) bool) []string {
	var out []string
	for _, e := range r.events {
		name := e
		for i, c := range e {
			if c == '{' || c == ' ' {
				name = e[:i]
				break
			}
		}
		if keep(name) {
			out = append(out, e)
		}
	}
	return out
}

// expectFiltered is expect(events.filter(({ name }) => ...)).deep.equals([...]).
func (r *recorder) expectFiltered(t *testing.T, keep func(name string) bool, want ...string) {
	t.Helper()
	if got := r.filter(keep); !slices.Equal(got, want) {
		t.Fatalf("filtered events:\n got  %q\n want %q", got, want)
	}
}

// set is device.set({ switch: { currentPosition } }).
func (d *device) set(pos uint8) {
	d.t.Helper()
	if err := d.s.SetCurrentPosition(pos); err != nil {
		d.t.Fatalf("currentPosition %d: %v", pos, err)
	}
}

// raw is device.set({ switch: { rawPosition } }).
func (d *device) raw(pos uint8) {
	d.t.Helper()
	if err := d.s.SetRawPosition(pos); err != nil {
		d.t.Fatalf("rawPosition %d: %v", pos, err)
	}
}

// advance is MockTime.advance(ms).
func (d *device) advance(ms int) { d.clk.Advance(time.Duration(ms) * time.Millisecond) }

// expectState is expect(device.stateOf(SwitchServer).<name>).equals(want).
func (d *device) expectState(t *testing.T, name string, want uint8) {
	t.Helper()
	var got uint8
	switch name {
	case "currentPosition":
		got = d.s.CurrentPosition()
	case "rawPosition":
		got = d.s.RawPosition()
	default:
		t.Fatalf("no state %q", name)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

// holdingSwitchLock runs actor while another transaction holds the switch
// state lock (SwitchServerTest.ts:98-104).
func (d *device) holdingSwitchLock(actor func()) {
	d.s.hold()
	actor()
	d.s.release()
}

// advanceHoldingSwitchLock advances mock time while holding the switch state
// lock, so every timer due meanwhile expires before any reaction
// (SwitchServerTest.ts:108-111).
func (d *device) advanceHoldingSwitchLock(ms int) {
	d.holdingSwitchLock(func() { d.advance(ms) })
}

// resetHeld is resetState called by the transaction that holds the lock: it
// runs at once.
func (d *device) resetHeld() {
	d.s.mu.Lock()
	d.s.resetLocked()
	d.s.mu.Unlock()
	d.s.drain()
}

// resetWaiting is resetState called while another transaction holds the
// lock: it returns a channel closed once the reset ran, and the reset has
// not run when it returns.
func (d *device) resetWaiting(t *testing.T) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.s.ResetState()
		close(done)
	}()
	d.clk.Yield()
	select {
	case <-done:
		t.Fatal("resetState ran while another transaction held the switch state lock")
	default:
	}
	return done
}

// shortThenLongPress mirrors SwitchServerTest.ts:118-126.
func (d *device) shortThenLongPress(holdMs, idleMs int) {
	d.set(1)
	d.advance(50)
	d.set(0)
	d.set(1)
	d.advanceHoldingSwitchLock(holdMs)
	d.set(0)
	d.advanceHoldingSwitchLock(idleMs)
}

// doTestPress mirrors SwitchServerTest.ts:127-154.
func (d *device) doTestPress(t *testing.T, delay int, seq []uint8, want ...string) {
	t.Helper()
	d.s.setNumberOfPositions(slices.Max(seq) + 1)
	events := d.catch()
	for i, pos := range seq {
		if i != 0 && delay != 0 {
			d.advance(delay)
		}
		d.set(pos)
	}
	events.expect(t, want...)
}

// setNumberOfPositions and setMultiPressMax are device.set of the two fixed
// attributes, which matter.js's tests change on a running server.
func (s *Server) setNumberOfPositions(n uint8) {
	s.mu.Lock()
	s.numberOfPositions = n
	s.mu.Unlock()
	if err := s.Set(swdef.AttrNumberOfPositions, n); err != nil {
		panic(err)
	}
}

func (s *Server) setMultiPressMax(n uint8) {
	s.mu.Lock()
	s.multiPressMax = n
	s.mu.Unlock()
	if err := s.Set(swdef.AttrMultiPressMax, n); err != nil {
		panic(err)
	}
}

// The devices of SwitchServerTest.ts:38-92.
func createLatchingSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureLS)
}

func createMsSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS)
}

func createMsMsrSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS|FeatureMSR)
}

func createMsMsrMslSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS|FeatureMSR|FeatureMSL)
}

func createMsMsrMslMsmSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS|FeatureMSR|FeatureMSL|FeatureMSM)
}

func createMsMsrMsmSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS|FeatureMSR|FeatureMSM)
}

func createMsAsMslMsmSwitch(t *testing.T) *device {
	t.Helper()
	return newDevice(t, FeatureMS|FeatureAS|FeatureMSL|FeatureMSM)
}

// The beforeEach of each describe.

func debounceDevice(t *testing.T) *device { // :191-194
	t.Helper()
	d := createLatchingSwitch(t)
	d.s.SetDebounceDelay(50 * time.Millisecond)
	return d
}

func msMsrMslDevice(t *testing.T) *device { // :816-819
	t.Helper()
	d := createMsMsrMslSwitch(t)
	d.s.SetLongPressDelay(100 * time.Millisecond)
	return d
}

func msMsrDevice(t *testing.T) *device { // :1039-1041
	t.Helper()
	return createMsMsrSwitch(t)
}

func msDevice(t *testing.T) *device { // :1215-1217
	t.Helper()
	return createMsSwitch(t)
}

func msMsrMslMsmDevice(t *testing.T) *device { // :1284-1289
	t.Helper()
	d := createMsMsrMslMsmSwitch(t)
	d.s.SetLongPressDelay(100 * time.Millisecond)
	d.s.SetMultiPressDelay(150 * time.Millisecond)
	d.s.setMultiPressMax(3)
	return d
}

func msAsMslMsmDevice(t *testing.T) *device { // :2361-2366
	t.Helper()
	d := createMsAsMslMsmSwitch(t)
	d.s.SetLongPressDelay(100 * time.Millisecond)
	d.s.SetMultiPressDelay(150 * time.Millisecond)
	d.s.setMultiPressMax(3)
	return d
}

func msMsrMsmDevice(t *testing.T) *device { // :3221-3224
	t.Helper()
	d := createMsMsrMsmSwitch(t)
	d.s.SetMultiPressDelay(150 * time.Millisecond)
	return d
}

func resetStateDevice(t *testing.T) *device { // :3299-3304
	t.Helper()
	d := createMsMsrMslMsmSwitch(t)
	d.s.SetLongPressDelay(100 * time.Millisecond)
	d.s.SetMultiPressDelay(150 * time.Millisecond)
	d.s.setMultiPressMax(3)
	return d
}

// press is the resetState describe's helper (:3310-3314).
func (d *device) press() {
	d.set(1)
	d.advance(50)
	d.set(0)
}

// expectConstraintError is a rejected device.set: the matter.js validator's
// ConstraintError.
func expectConstraintError(t *testing.T, err error) {
	t.Helper()
	var st interface{ MatterStatusCode() im.StatusCode }
	if !errors.Is(err, ErrPositionOutOfRange) || !errors.As(err, &st) || st.MatterStatusCode() != im.StatusConstraintError {
		t.Fatalf("err = %v, want CONSTRAINT_ERROR wrapping ErrPositionOutOfRange", err)
	}
}

// test custom validators
func TestSwitchServerCustomValidators(t *testing.T) {
	t.Run("Accept valid currentPosition", func(t *testing.T) {
		d := createLatchingSwitch(t)
		if err := d.s.SetCurrentPosition(1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Reject invalid currentPosition", func(t *testing.T) {
		d := createLatchingSwitch(t)
		// matter.js rejects with 'Validating node0.part0.switch.state: Constraint "max numberOfPositions - 1": Value 2 is not within bounds defined by constraint (135)'
		expectConstraintError(t, d.s.SetCurrentPosition(2))
	})
	t.Run("Accept valid rawPosition", func(t *testing.T) {
		d := createLatchingSwitch(t)
		if err := d.s.SetRawPosition(1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Reject invalid rawPosition", func(t *testing.T) {
		d := createLatchingSwitch(t)
		// matter.js rejects with "Error in reactor<node0.part0.switch.#assertPositionInRange>: Position 2 invalid"
		expectConstraintError(t, d.s.SetRawPosition(2))
	})
}

// Test Debounce
func TestSwitchServerDebounce(t *testing.T) {
	t.Run("set currentState is immediately", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.set(1)

		events.expect(t,
			latched(1),
			cur(0, 1),
		)
	})
	t.Run("set rawPosition with debounceDelay=0 is immediately", func(t *testing.T) {
		d := debounceDevice(t)
		d.s.SetDebounceDelay(0)

		events := d.catch()

		d.raw(1)

		events.expect(t,
			raw(0, 1),
			latched(1),
			cur(0, 1),
		)
	})
	t.Run("set stable rawPosition with debounceDelay is respecting debounceDelay", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)

		events.expect(t,
			raw(0, 1),
		)

		d.advance(30)

		events.expect(t,
			raw(0, 1),
		)

		d.advance(30)

		// We need to trick the event loop a bit - but we do not advance time any further so anything is still "not timer based"
		// (the wait for currentPosition$Changed: the Go server delivered within the advance)

		events.expect(t,
			raw(0, 1),
			latched(1),
			cur(0, 1),
		)
	})
	t.Run("set unstable rawPosition with debounceDelay is respecting debounceDelay", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)

		events.expect(t,
			raw(0, 1),
		)

		d.advance(10)

		// After 10ms set back to 0
		d.raw(0)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
		)

		d.advance(10)

		// After another 10ms set back to 1
		d.raw(1)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
		)

		d.advance(30)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
		)

		d.advance(30)

		// We need to trick the event loop a bit - but we do not advance time any further so anything is still "not timer based"
		// (the wait for currentPosition$Changed: the Go server delivered within the advance)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
			latched(1),
			cur(0, 1),
		)
	})
	t.Run("set unstable rawPosition with debounceDelay ending on source value is respecting debounceDelay", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)

		events.expect(t,
			raw(0, 1),
		)

		d.advance(10)

		// After 10ms set back to 0
		d.raw(0)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
		)

		d.advance(10)

		// After another 10ms set back to 1
		d.raw(1)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
		)

		d.advance(10)

		// After another 10ms set back to 1
		d.raw(0)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
			raw(1, 0),
		)
		d.advance(30)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
			raw(1, 0),
		)

		d.advance(30)

		// Make sure nothing is left somewhere in event loop
		// (20 macrotasks: nothing is left to run in the Go server)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			raw(0, 1),
			raw(1, 0),
		)
	})
	t.Run("reports a stable position even when the raw position changes before the reaction to its expiry runs", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)

		// Forces the raw position to change after the debounce expired but before the reaction to the expiry runs
		d.holdingSwitchLock(func() {
			d.advance(50)
			d.raw(0)
		})
		d.advance(50)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			latched(1),
			cur(0, 1),
			latched(0),
			cur(1, 0),
		)
	})
	t.Run("reports a debounced position before a later direct write without overwriting it", func(t *testing.T) {
		d := debounceDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		d.raw(1)

		// Forces the debounce to expire, then currentPosition to be written, before the reaction to the expiry runs
		d.holdingSwitchLock(func() {
			d.advance(50)
			d.set(2)
		})

		events.expect(t,
			raw(0, 1),
			latched(1),
			latched(2),
			cur(0, 2),
		)
		d.expectState(t, "currentPosition", 2)
	})
	t.Run("drops a pending debounce when the raw position is set while debouncing is off", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)
		d.s.SetDebounceDelay(0 * time.Millisecond)
		d.raw(0)
		d.advance(100)

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
		)
		d.expectState(t, "currentPosition", 0)
	})
	t.Run("applies a raw position set while debouncing is off after a debounced position still to be written", func(t *testing.T) {
		d := debounceDevice(t)
		events := d.catch()

		d.raw(1)

		// Forces the debounce to expire, then debouncing off and the raw position back to the current position,
		// before the debounced position is written
		d.holdingSwitchLock(func() {
			d.advance(50)
			d.s.SetDebounceDelay(0)
			d.raw(0)
		})

		events.expect(t,
			raw(0, 1),
			raw(1, 0),
			latched(1),
			cur(0, 1),
			latched(0),
			cur(1, 0),
		)
		d.expectState(t, "currentPosition", 0)
	})
}

// Test LS
func TestSwitchServerLS(t *testing.T) {
	t.Run("Test Single Press with 2 positions", func(t *testing.T) {
		d := createLatchingSwitch(t)

		d.doTestPress(t, 0, []uint8{1, 0},
			latched(1),
			cur(0, 1),
			latched(0),
			cur(1, 0),
		)
	})
	t.Run("reports a move to the position the switch had before a reset", func(t *testing.T) {
		d := createLatchingSwitch(t)

		d.set(1)
		d.s.ResetState()

		events := d.catch()
		d.set(0)

		events.expect(t,
			latched(0),
			cur(1, 0),
		)
	})
	t.Run("Test Single Press with 3 positions", func(t *testing.T) {
		d := createLatchingSwitch(t)

		d.doTestPress(t, 0, []uint8{1, 2, 0},
			latched(1),
			cur(0, 1),
			latched(2),
			cur(1, 2),
			latched(0),
			cur(2, 0),
		)
	})
}

// Test MS & MSR & MSL
func TestSwitchServerMSMSRMSL(t *testing.T) {
	t.Run("Test short Press with 2 positions", func(t *testing.T) {
		d := msMsrMslDevice(t)
		d.doTestPress(t, 50, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test long Press with 2 positions", func(t *testing.T) {
		d := msMsrMslDevice(t)
		d.doTestPress(t, 110, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test even longer Press with 2 positions", func(t *testing.T) {
		d := msMsrMslDevice(t)
		d.doTestPress(t, 300, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test release processed before the reaction to the expired long press timer", func(t *testing.T) {
		d := msMsrMslDevice(t)
		events := d.catch()

		d.set(1)

		// Forces the release to be written after the long press timer expired but before the reaction to it runs
		d.holdingSwitchLock(func() {
			d.advance(100)
			d.set(0)
		})

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test release debounced before the long press timer expires, both before their reactions run", func(t *testing.T) {
		d := msMsrMslDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		events := d.catch()

		d.raw(1)
		d.advance(50)
		d.raw(0)

		// Forces the debounced release and then the long press timer to expire before either reaction runs
		d.advanceHoldingSwitchLock(150)

		events.expect(t,
			raw(0, 1),
			initialPress(1),
			cur(0, 1),
			raw(1, 0),
			shortRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test long press followed by a move releases long without a further InitialPress", func(t *testing.T) {
		d := msMsrMslDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		d.set(1)
		d.advance(150)
		d.set(2)
		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			cur(1, 2),
			longRelease(2),
			cur(2, 0),
		)
	})
}

// Test MS & MSR & !MSL
func TestSwitchServerMSMSRNotMSL(t *testing.T) {
	t.Run("Test short Press with 2 positions", func(t *testing.T) {
		d := msMsrDevice(t)
		d.doTestPress(t, 50, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test short Presses with 3 positions", func(t *testing.T) {
		d := msMsrDevice(t)
		d.doTestPress(t, 50, []uint8{1, 0, 2, 0},
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(2),
			cur(0, 2),
			shortRelease(2),
			cur(2, 0),
		)
	})
	t.Run("Test short Presses joystick-like", func(t *testing.T) {
		d := msMsrDevice(t)
		d.doTestPress(t, 50, []uint8{6, 5, 0},
			initialPress(6),
			cur(0, 6),
			initialPress(5),
			cur(6, 5),
			shortRelease(5),
			cur(5, 0),
		)
	})
	t.Run("Test long Press with 2 positions", func(t *testing.T) {
		d := msMsrDevice(t)
		d.doTestPress(t, 110, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test even longer Press with 2 positions", func(t *testing.T) {
		d := msMsrDevice(t)
		d.doTestPress(t, 300, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)
	})
}

// Test MS & !MSR & !MSL
func TestSwitchServerMSNotMSRNotMSL(t *testing.T) {
	t.Run("Test short Press with 2 positions", func(t *testing.T) {
		d := msDevice(t)
		d.doTestPress(t, 50, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)
	})
	t.Run("Test long Press with 2 positions", func(t *testing.T) {
		d := msDevice(t)
		d.doTestPress(t, 110, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)
	})
	t.Run("Test even longer Press with 2 positions", func(t *testing.T) {
		d := msDevice(t)
		d.doTestPress(t, 300, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)
	})
}

// Test MS & MSR & MSL & MSM
func TestSwitchServerMSMSRMSLMSM(t *testing.T) {
	t.Run("Test long Press with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.doTestPress(t, 110, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test one short Press with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
		)

		d.advance(50)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 1),
		)
	})
	t.Run("Test two short Presses with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			multiPressOngoing(1, 2),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
	t.Run("Test three short Presses with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			multiPressOngoing(1, 2),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			multiPressOngoing(1, 3),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 3),
		)
	})
	t.Run("Test a press after an aborted sequence reports neither InitialPress nor ShortRelease", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()
		d.s.setMultiPressMax(2)

		for range 4 {
			d.set(1)
			d.advance(50)
			d.set(0)
			d.advance(50)
		}
		d.advance(160)

		// The fourth press got no InitialPress, so its release reports nothing either
		events.expectFiltered(t, func(n string) bool { return n != "currentPosition$Changed" },
			initialPress(1),
			shortRelease(1),
			initialPress(1),
			multiPressOngoing(1, 2),
			shortRelease(1),
			initialPress(1),
			shortRelease(1),
			multiPressComplete(1, 0),
		)
	})
	t.Run("Test three short Presses with max 2 abort the sequence with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()
		d.s.setMultiPressMax(2)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			multiPressOngoing(1, 2),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 0),
		)
	})
	t.Run("Test moves between pressed positions without release count as one press", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		// Multi-press detection counts "press-release cycles"; a move between pressed positions has no release
		for _, position := range []uint8{1, 2, 1, 2, 0} {
			d.set(position)
			d.advance(20)
		}
		d.advance(140)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			initialPress(2),
			cur(1, 2),
			initialPress(1),
			cur(2, 1),
			initialPress(2),
			cur(1, 2),
			shortRelease(2),
			cur(2, 0),
			multiPressComplete(2, 1),
		)
	})
	t.Run("Test two short presses on different positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		for _, position := range []uint8{1, 0, 2, 0} {
			d.set(position)
			d.advance(50)
		}
		d.advance(110)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(2),
			multiPressOngoing(2, 2),
			cur(0, 2),
			shortRelease(2),
			cur(2, 0),
			multiPressComplete(2, 2),
		)
	})
	t.Run("Test move restarts the long press delay", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		// The move generates an InitialPress, and LongPress refers to the previous InitialPress
		d.set(1)
		d.advance(60)
		d.set(2)
		d.advance(60)
		d.set(0)
		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			initialPress(2),
			cur(1, 2),
			shortRelease(2),
			cur(2, 0),
			multiPressComplete(2, 1),
		)
	})
	t.Run("Test long press followed by a move to another position", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		d.set(1)
		d.advance(150)
		// A press reported as long stays one press until its release, so the move generates no InitialPress
		d.set(2)
		d.advance(50)
		d.set(0)
		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			cur(1, 2),
			longRelease(2),
			cur(2, 0),
		)
	})
	t.Run("Test one long and one short press with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		d.s.SetLongPressDelay(300 * time.Millisecond)
		d.set(1)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
		)

		d.advance(500)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)

		d.advance(10)

		d.set(1)

		d.advance(10)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test one short press and a long press with 2 positions", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		// Forces the long press timer of the second press to expire before the reaction to it runs
		d.shortThenLongPress(400, 400)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			initialPress(1),
			multiPressOngoing(1, 2),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
	t.Run("Test long press is not followed by a multi press complete", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		events := d.catch()

		d.set(1)
		d.advance(400)
		d.set(0)
		d.advance(400)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test debounced further press stops the multi press timer when it occurs", func(t *testing.T) {
		d := msMsrMslMsmDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		events := d.catch()

		d.raw(1)
		d.advance(50)
		d.raw(0)
		d.advance(50)
		d.raw(1)

		// Forces the further press to be debounced, then the multi press delay counted from the release to end,
		// before the reaction to either runs
		d.advanceHoldingSwitchLock(200)
		d.raw(0)
		d.advance(50)
		d.advance(150)

		events.expect(t,
			raw(0, 1),
			initialPress(1),
			cur(0, 1),
			raw(1, 0),
			shortRelease(1),
			cur(1, 0),
			raw(0, 1),
			initialPress(1),
			multiPressOngoing(1, 2),
			cur(0, 1),
			raw(1, 0),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
}

// Test MS & AS & MSL & MSM
func TestSwitchServerMSASMSLMSM(t *testing.T) {
	t.Run("Test long Press with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.doTestPress(t, 110, []uint8{1, 0},
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)
	})
	t.Run("Test one short Press with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
		)

		d.advance(50)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 1),
		)
	})
	t.Run("Test two short Presses with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
	t.Run("Test three short Presses with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 3),
		)
	})
	t.Run("Test three short Presses with max 2 abort the sequence with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()
		d.s.setMultiPressMax(2)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(50)

		d.set(1)

		d.advance(50)

		d.set(0)

		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 0),
		)
	})
	t.Run("Test moves between pressed positions without release count as one press", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		// Multi-press detection counts "press-release cycles"; a move between pressed positions has no release
		for _, position := range []uint8{1, 2, 1, 2, 0} {
			d.set(position)
			d.advance(20)
		}
		d.advance(140)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 2),
			cur(2, 1),
			cur(1, 2),
			cur(2, 0),
			multiPressComplete(2, 1),
		)
	})
	t.Run("Test two short presses on different positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		for _, position := range []uint8{1, 0, 2, 0} {
			d.set(position)
			d.advance(50)
		}
		d.advance(110)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 2),
			cur(2, 0),
			multiPressComplete(2, 2),
		)
	})
	t.Run("Test press held across a move reaches the long press delay from its start", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		// An action switch generates no InitialPress for the move, so the press keeps its long press timing
		d.set(1)
		d.advance(60)
		d.set(2)
		d.advance(60)
		d.set(0)
		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 2),
			longPress(2),
			longRelease(2),
			cur(2, 0),
		)
	})
	t.Run("Test long press followed by a move to another position", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.s.setNumberOfPositions(3)
		events := d.catch()

		d.set(1)
		d.advance(150)
		// An action switch generates no InitialPress for the move, so LongRelease refers to the InitialPress of the press
		d.set(2)
		d.advance(50)
		d.set(0)
		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			cur(1, 2),
			longRelease(2),
			cur(2, 0),
		)
	})
	t.Run("Test one long and one short press with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()

		d.s.SetLongPressDelay(300 * time.Millisecond)
		d.set(1)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
		)

		d.advance(500)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
		)

		d.advance(10)

		d.set(1)

		d.advance(10)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			longPress(1),
			longRelease(1),
			cur(1, 0),
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)
	})
	t.Run("Test one short press and a long press with 2 positions", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		events := d.catch()

		d.s.SetLongPressDelay(300 * time.Millisecond)
		d.set(1)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
		)

		d.advance(50)

		d.set(0)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
		)

		d.set(1)

		// Forces every timer due during the long press to expire before any reaction to them runs
		d.advanceHoldingSwitchLock(400)

		d.set(0)

		// Forces every timer due while the switch is released to expire before any reaction to them runs
		d.advanceHoldingSwitchLock(400)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
	t.Run("Test one short press and a long press with equal long press and multi press delays", func(t *testing.T) {
		d := msAsMslMsmDevice(t)
		d.s.SetLongPressDelay(150 * time.Millisecond)
		d.s.SetMultiPressDelay(150 * time.Millisecond)
		events := d.catch()

		// Forces the long press timer of the second press to expire before the reaction to it runs
		d.shortThenLongPress(400, 400)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			cur(1, 0),
			cur(0, 1),
			cur(1, 0),
			multiPressComplete(1, 2),
		)
	})
}

// Test MS & MSR & !MSL & MSM
func TestSwitchServerMSMSRNotMSLMSM(t *testing.T) {
	t.Run("Test a press that exceeds the maximum keeps its ShortRelease across a move", func(t *testing.T) {
		d := msMsrMsmDevice(t)
		events := d.catch()
		d.s.setNumberOfPositions(3)
		d.s.setMultiPressMax(2)

		for _, position := range []uint8{1, 0, 1, 0, 1, 2, 0} {
			d.set(position)
			d.advance(50)
		}
		d.advance(160)

		if got := len(events.filter(func(n string) bool { return n == "shortRelease" })); got != 3 {
			t.Fatalf("%d matching events, want 3: %q", got, events.events)
		}
	})
	t.Run("Test a press after an aborted sequence reports no ShortRelease", func(t *testing.T) {
		d := msMsrMsmDevice(t)
		events := d.catch()
		d.s.setMultiPressMax(2)

		for range 4 {
			d.set(1)
			d.advance(50)
			d.set(0)
			d.advance(50)
		}
		d.advance(160)

		if got := len(events.filter(func(n string) bool { return n == "initialPress" || n == "shortRelease" })); got != 6 {
			t.Fatalf("%d matching events, want 6: %q", got, events.events)
		}
	})
	t.Run("Test press held longer than the multi press delay", func(t *testing.T) {
		d := msMsrMsmDevice(t)
		events := d.catch()

		d.set(1)
		d.advance(400)
		d.set(0)
		d.advance(400)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 1),
		)
	})
}

// Test resetState
func TestSwitchServerResetState(t *testing.T) {
	t.Run("starts no debounce for the raw position it resynchronizes", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.set(1)
		d.s.ResetState()
		d.set(0)
		d.advance(100)

		d.expectState(t, "currentPosition", 0)
	})
	t.Run("drops a multi-press cycle in progress, so the next press counts from one", func(t *testing.T) {
		d := resetStateDevice(t)
		// Two presses, so the reset has a press count above one to drop
		d.press()
		d.advance(50)
		d.press()

		d.s.ResetState()

		events := d.catch()

		d.press()
		d.advance(160)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			shortRelease(1),
			cur(1, 0),
			multiPressComplete(1, 1),
		)
	})
	t.Run("drops a press still being timed, so no long press is reported after the reset", func(t *testing.T) {
		d := resetStateDevice(t)
		// Held, but not yet long enough to have been reported as a long press
		d.set(1)
		d.advance(50)

		d.s.ResetState()

		events := d.catch()

		d.advance(200)

		events.expect(t)
	})
	t.Run("reports the long press this one would have suppressed, when no reset intervenes", func(t *testing.T) {
		d := resetStateDevice(t)
		d.set(1)
		d.advance(50)

		events := d.catch()

		d.advance(200)

		events.expect(t,
			longPress(1),
		)
	})
	t.Run("drops a debounced release pending at the reset without reporting a new press", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.raw(1)
		d.advance(50)
		d.raw(0)

		events := d.catch()
		d.s.ResetState()
		d.advance(200)

		events.expect(t,
			raw(0, 1),
		)
	})
	t.Run("waits for the switch state lock held by another transaction", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.raw(1)

		var reset <-chan struct{}
		d.holdingSwitchLock(func() {
			reset = d.resetWaiting(t)
		})
		<-reset

		d.expectState(t, "rawPosition", 0)
	})
	t.Run("drops long press timing of a press committed while the reset waits for the lock", func(t *testing.T) {
		d := resetStateDevice(t)
		events := d.catch()

		// Forces the reset to be called while another transaction holds the lock with a press it then commits
		var reset <-chan struct{}
		d.holdingSwitchLock(func() {
			d.set(1)
			reset = d.resetWaiting(t)
		})
		<-reset
		d.advance(200)

		events.expect(t,
			initialPress(1),
			cur(0, 1),
			raw(0, 1),
		)
	})
	t.Run("drops a position still being debounced", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.raw(1)

		d.s.ResetState()

		events := d.catch()

		d.advance(100)

		events.expect(t)
	})
	t.Run("drops a debounced position whose reaction still waits for the lock at the reset", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.raw(1)
		events := d.catch()

		// Forces the debounce to expire, then the reset, before the reaction to the expiry runs
		d.holdingSwitchLock(func() {
			d.advance(50)
			d.resetHeld()
		})
		d.advance(100)

		events.expect(t,
			raw(1, 0),
		)
		d.expectState(t, "currentPosition", 0)
	})
	t.Run("debounces a raw position again that was still being debounced at the reset", func(t *testing.T) {
		d := resetStateDevice(t)
		d.s.SetDebounceDelay(50 * time.Millisecond)
		d.raw(1)
		d.s.ResetState()

		events := d.catch()

		d.raw(1)
		d.advance(50)

		events.expect(t,
			raw(0, 1),
			initialPress(1),
			cur(0, 1),
		)
	})
}
