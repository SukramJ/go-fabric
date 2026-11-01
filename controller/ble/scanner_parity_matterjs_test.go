// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

// A port of matter.js packages/protocol/test/common/BleScannerTest.ts at
// 862ad75f: the parser vectors (SERVICE_DATA_A/B, :12-13) and the cases
// of the record store and of the discoveries — deduplication and aging,
// forgetCommissionedDevice, reachability, overlapping discoveries,
// discoveries for one identifier, close. Each case names its matter.js
// title. MockTime is fakeClock; "settleDiscovery" is waiting for the
// waiter; a callback that throws is one that panics.

import (
	"context"
	"encoding/hex"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

var (
	serviceDataA, _ = hex.DecodeString("00c9067c11018000") // D=1737, VP=4476+32769
	serviceDataB, _ = hex.DecodeString("00e8037c11018000") // D=1000, VP=4476+32769
)

const (
	addrA = "aa:aa:aa:aa:aa:aa"
	addrB = "bb:bb:bb:bb:bb:bb"
)

// fakeClock is the test clock, a [clocktest.Fake].
type fakeClock = clocktest.Fake

func newFakeClock() *fakeClock { return clocktest.New(time.Unix(1_000_000, 0)) }

// mockClient is MockOneShotBleScannerClient (BleScannerTest.ts:16-108):
// it records each scan call, can hold a start or a stop in flight, and
// reports no listening time; with a meter it is MockBleScannerClient's
// radio.
type mockClient struct {
	mu        sync.Mutex
	cb        func(Peripheral, []byte)
	scanning  bool
	calls     []string
	startErr  error
	stopErr   error
	startGate chan struct{}
	stopGate  chan struct{}
	meter     *ListeningMeter
	connectFn func(Peripheral) (Connection, error)
}

func (m *mockClient) SetDiscoveryCallback(f func(Peripheral, []byte)) {
	m.mu.Lock()
	m.cb = f
	m.mu.Unlock()
}

func (m *mockClient) StartScanning(context.Context) error {
	m.mu.Lock()
	m.calls = append(m.calls, "start")
	gate := m.startGate
	m.mu.Unlock()
	if gate != nil {
		<-gate
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.listenLocked(true)
	return nil
}

func (m *mockClient) StopScanning(context.Context) error {
	m.mu.Lock()
	m.calls = append(m.calls, "stop")
	gate := m.stopGate
	m.mu.Unlock()
	if gate != nil {
		<-gate
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listenLocked(false)
	return m.stopErr
}

func (m *mockClient) Connect(_ context.Context, p Peripheral) (Connection, error) {
	if m.connectFn != nil {
		return m.connectFn(p)
	}
	return nil, errors.New("no connection")
}

// listenLocked is startListening / stopListening: the radio's own state.
func (m *mockClient) listenLocked(on bool) {
	m.scanning = on
	if m.meter == nil {
		return
	}
	if on {
		m.meter.Start()
	} else {
		m.meter.Stop()
	}
}

// stopListening is the radio stopping on its own (BleScannerTest.ts:89-93).
func (m *mockClient) stopListening() {
	m.mu.Lock()
	m.listenLocked(false)
	m.mu.Unlock()
}

func (m *mockClient) setStartErr(err error) {
	m.mu.Lock()
	m.startErr = err
	m.mu.Unlock()
}

func (m *mockClient) setStopErr(err error) {
	m.mu.Lock()
	m.stopErr = err
	m.mu.Unlock()
}

// holdStart / releaseStart and holdStop / releaseStop leave the next
// call in flight (BleScannerTest.ts:51-75).
func (m *mockClient) holdStart() {
	m.mu.Lock()
	m.startGate = make(chan struct{})
	m.mu.Unlock()
}

func (m *mockClient) releaseStart() {
	m.mu.Lock()
	if m.startGate != nil {
		close(m.startGate)
		m.startGate = nil
	}
	m.mu.Unlock()
}

func (m *mockClient) holdStop() {
	m.mu.Lock()
	m.stopGate = make(chan struct{})
	m.mu.Unlock()
}

func (m *mockClient) releaseStop() {
	m.mu.Lock()
	if m.stopGate != nil {
		close(m.stopGate)
		m.stopGate = nil
	}
	m.mu.Unlock()
}

func (m *mockClient) scanCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func (m *mockClient) isScanning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scanning
}

func (m *mockClient) discover(address string, data []byte) {
	m.mu.Lock()
	cb := m.cb
	m.mu.Unlock()
	cb(Peripheral{Address: address}, data)
}

// listeningClient is MockBleScannerClient: it reports the time its radio
// scanned (BleListeningClock, here [ListeningMeter]).
type listeningClient struct {
	mockClient
}

func (l *listeningClient) ListeningTime() time.Duration { return l.meter.Total() }

// proxyClient is MockProxyingBleScannerClient.
type proxyClient struct {
	listeningClient
	unreachable map[string]bool
}

func (p *proxyClient) IsPeripheralReachable(address string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.unreachable[address]
}

func (p *proxyClient) setReachable(address string, ok bool) {
	p.mu.Lock()
	p.unreachable[address] = !ok
	p.mu.Unlock()
}

func newListening(clock *fakeClock) *listeningClient {
	return &listeningClient{mockClient{meter: NewListeningMeter(clock)}}
}

func (s *Scanner) waiterCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waiters)
}

// settle waits until n discoveries wait for an advertisement.
func settle(t *testing.T, s *Scanner, n int) {
	t.Helper()
	eventually(t, s, func() bool { return s.waiterCount() >= n }, "discoveries waiting")
}

// yield lets the scanner's goroutines run: the fake clock's Yield.
func yield(s *Scanner) {
	if f, ok := s.clock.(*clocktest.Fake); ok {
		f.Yield()
		return
	}
	runtime.Gosched()
}

// eventually yields until cond holds, failing after 5 s of real time.
func eventually(t *testing.T, s *Scanner, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		yield(s)
	}
}

// armed waits until the fake clock has at least n timers armed, so an
// Advance reaches a timer a goroutine is about to arm.
func armed(t *testing.T, s *Scanner, clock *fakeClock, n int) {
	t.Helper()
	eventually(t, s, func() bool { return clock.Pending() >= n }, "armed timers")
}

// recv receives from ch, failing after 5 s of real time: a discovery
// that should settle without virtual time passing.
func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

// startScan scans the way a discovery does, for an identifier no
// advertisement here matches (BleScannerTest.ts:131-141); the returned
// function ends it.
func startScan(t *testing.T, s *Scanner) func() {
	t.Helper()
	done := make(chan struct{})
	n := s.waiterCount()
	go func() {
		defer close(done)
		_, _ = s.FindContinuously(context.Background(), discovery.ByProduct(1), func(Device) {}, 0)
	}()
	settle(t, s, n+1)
	return func() {
		s.Cancel(discovery.ByProduct(1))
		<-done
	}
}

func addresses(ds []Device) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Peripheral.Address
	}
	return out
}

func want(t *testing.T, got []Device, addrs ...string) {
	t.Helper()
	g := addresses(got)
	if len(g) != len(addrs) {
		t.Fatalf("devices %v, want %v", g, addrs)
	}
	for i := range g {
		if g[i] != addrs[i] {
			t.Fatalf("devices %v, want %v", g, addrs)
		}
	}
}

// --- service-data-based deduplication and aging ---

func TestBleScannerMergesRepeatAdvertisements(t *testing.T) {
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
	c.discover(addrA, serviceDataA)
	c.discover(addrA, serviceDataA)
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)), addrA)
}

func TestBleScannerKeepsBothWithinStaleWindow(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	clock.Advance(60 * time.Second)
	c.discover(addrB, serviceDataA)
	stop()
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)), addrB, addrA)
}

func TestBleScannerReplacesAfterStaleWindow(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	clock.Advance(61 * time.Second)
	c.discover(addrB, serviceDataA)
	stop()
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)), addrB)
	if _, err := s.Device(addrA); !errors.Is(err, ErrNoDevice) {
		t.Fatal("the rotated-away address is still stored")
	}
}

func TestBleScannerDropsRotatedAddressByElapsedTime(t *testing.T) {
	clock := newFakeClock()
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: clock})
	c.discover(addrA, serviceDataA)
	clock.Advance(61 * time.Second)
	c.discover(addrB, serviceDataA)
	if _, err := s.Device(addrA); err == nil {
		t.Fatal("the rotated-away address is still stored")
	}
}

func TestBleScannerKeepsEntryNobodyListenedFor(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	stop()
	clock.Advance(120 * time.Second)
	stop = startScan(t, s)
	c.discover(addrB, serviceDataA)
	stop()
	if _, err := s.Device(addrA); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Discovered(discovery.ByLongDiscriminator(1737))); n != 2 {
		t.Fatalf("%d devices", n)
	}
}

func TestBleScannerKeepsRefreshedEntry(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	clock.Advance(55 * time.Second)
	c.discover(addrA, serviceDataA)
	clock.Advance(55 * time.Second)
	c.discover(addrB, serviceDataA)
	stop()
	if n := len(s.Discovered(discovery.ByLongDiscriminator(1737))); n != 2 {
		t.Fatalf("%d devices", n)
	}
}

func TestBleScannerKeepsEntriesWithOtherServiceData(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	clock.Advance(120 * time.Second)
	c.discover(addrB, serviceDataB)
	stop()
	if _, err := s.Device(addrA); err != nil {
		t.Fatal(err)
	}
	want(t, s.Discovered(discovery.ByLongDiscriminator(1000)), addrB)
}

func TestBleScannerStopsOfferingStalePeripheral(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	defer stop()
	c.discover(addrA, serviceDataA)
	clock.Advance(60 * time.Second)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)), addrA)
	clock.Advance(time.Second)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)))
}

func TestBleScannerOffersStalePeripheralAgain(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	defer stop()
	c.discover(addrA, serviceDataA)
	clock.Advance(61 * time.Second)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)))
	c.discover(addrA, serviceDataA)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)), addrA)
}

func TestBleScannerForgetsOneShotRecordAfter15Minutes(t *testing.T) {
	clock := newFakeClock()
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: clock})
	c.discover(addrA, serviceDataA)
	clock.Advance(15 * time.Minute)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)), addrA)
	clock.Advance(time.Second)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)))
	if _, err := s.Device(addrA); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("Device: %v", err)
	}
}

func TestBleScannerForgetsAfter15MinutesOfListening(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	defer stop()
	c.discover(addrA, serviceDataA)
	clock.Advance(15 * time.Minute)
	s.Discovered(discovery.ByShortDiscriminator(6))
	if _, err := s.Device(addrA); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	s.Discovered(discovery.ByShortDiscriminator(6))
	if _, err := s.Device(addrA); err == nil {
		t.Fatal("not forgotten")
	}
}

func TestBleScannerOneShotRecordDoesNotAgeWhileScanning(t *testing.T) {
	// "keeps a peripheral of a client that reports no listening time while
	// a scan runs, and forgets it 15 minutes after"
	clock := newFakeClock()
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{Clock: clock})
	stop := startScan(t, s)
	c.discover(addrA, serviceDataA)
	clock.Advance(20 * time.Minute)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)), addrA)
	stop()
	clock.Advance(15 * time.Minute)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)), addrA)
	clock.Advance(time.Second)
	want(t, s.Discovered(discovery.ByShortDiscriminator(6)))
}

// --- reachability ---

func newProxy(clock *fakeClock) *proxyClient {
	return &proxyClient{listeningClient: listeningClient{mockClient{meter: NewListeningMeter(clock)}}, unreachable: map[string]bool{}}
}

func TestBleScannerReachability(t *testing.T) {
	clock := newFakeClock()
	c := newProxy(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	c.discover(addrA, serviceDataA)
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)), addrA)
	c.setReachable(addrA, false)
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)))
	if _, err := s.Device(addrA); err == nil || !errors.Is(err, ErrNoDevice) {
		t.Fatalf("Device of an unreachable peripheral: %v", err)
	}
	c.setReachable(addrA, true)
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)), addrA)
	if _, err := s.Device(addrA); err != nil {
		t.Fatal(err)
	}
}

func TestBleScannerOneShotWaitsWhileOnlyUnreachableAdvertise(t *testing.T) {
	clock := newFakeClock()
	c := newProxy(clock)
	c.setReachable(addrA, false)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	res := make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByLongDiscriminator(1737), 10*time.Second, false)
		res <- d
	}()
	settle(t, s, 1)
	c.discover(addrA, serviceDataA)
	if s.waiterCount() != 1 {
		t.Fatal("an unreachable peripheral ended the discovery")
	}
	c.discover(addrB, serviceDataA)
	want(t, <-res, addrB)
}

func TestBleScannerPurgesWithIgnoreExisting(t *testing.T) {
	clock := newFakeClock()
	c := newProxy(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	c.discover(addrA, serviceDataA)
	c.setReachable(addrA, false)
	res := make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByLongDiscriminator(1737), 10*time.Second, true)
		res <- d
	}()
	settle(t, s, 1)
	c.setReachable(addrA, true)
	clock.Advance(11 * time.Second)
	want(t, <-res)
	want(t, s.Discovered(discovery.ByLongDiscriminator(1737)))
}

// --- discoveries for one identifier ---

func TestBleScannerHandsAdvertisementToEveryDiscovery(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	var mu sync.Mutex
	var first, second []string
	var wg sync.WaitGroup
	for _, sink := range []*[]string{&first, &second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.FindContinuously(context.Background(), discovery.ByShortDiscriminator(6), func(d Device) {
				mu.Lock()
				*sink = append(*sink, d.Peripheral.Address)
				mu.Unlock()
			}, 0)
		}()
	}
	settle(t, s, 2)
	c.discover(addrA, serviceDataA)
	settle(t, s, 2)
	_ = s.Close(context.Background())
	wg.Wait()
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first %v second %v", first, second)
	}
}

func TestBleScannerTimeoutEndsOneDiscovery(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	found := make(chan string, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.FindContinuously(context.Background(), discovery.ByShortDiscriminator(6), func(d Device) { found <- d.Peripheral.Address }, 0)
	}()
	expiring := make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByShortDiscriminator(6), 10*time.Second, false)
		expiring <- d
	}()
	settle(t, s, 2)
	clock.Advance(11 * time.Second)
	want(t, <-expiring)
	if !c.isScanning() {
		t.Fatal("the scan stopped with a discovery still running")
	}
	c.discover(addrA, serviceDataA)
	if got := <-found; got != addrA {
		t.Fatalf("found %s", got)
	}
	_ = s.Close(context.Background())
	<-done
}

// TestBleScannerCloseDuringContinuousCallback: a Close that runs while a
// continuous discovery is inside its callback still ends the discovery.
// The waiter for the next advertisement is registered before the callback
// runs, in the critical section of the closed check (Scanner.nextRound),
// so the close finds and finishes it; registered after the callback, it
// was never finished and the discovery waited forever — the stress job's
// 10-minute hang of TestBleScannerTimeoutEndsOneDiscovery (#306), whose
// Close follows the callback's send without a pause.
func TestBleScannerCloseDuringContinuousCallback(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.FindContinuously(context.Background(), discovery.ByShortDiscriminator(6), func(Device) {
			close(entered)
			<-release
		}, 0)
	}()
	settle(t, s, 1)
	c.discover(addrA, serviceDataA)
	recv(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- s.Close(context.Background()) }()
	if err := recv(t, closed); err != nil {
		t.Fatal(err)
	}
	close(release)
	recv(t, done)
}

func TestBleScannerAnotherTimeoutDoesNotEndDiscovery(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	short, long := make(chan []Device, 1), make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByShortDiscriminator(6), 10*time.Second, false)
		short <- d
	}()
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByShortDiscriminator(6), 60*time.Second, false)
		long <- d
	}()
	settle(t, s, 2)
	clock.Advance(11 * time.Second)
	want(t, <-short)
	select {
	case <-long:
		t.Fatal("the long discovery ended with the short one")
	default:
	}
	c.discover(addrA, serviceDataA)
	want(t, <-long, addrA)
}

func TestBleScannerDiscoveriesAskingDifferently(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	ids := []discovery.Identifier{
		discovery.ByLongDiscriminator(1737),
		discovery.ByShortDiscriminator(6),
		discovery.ByVendorProduct(4476, 32769),
		discovery.ByProduct(32769),
		discovery.ByVendor(4476),
		discovery.AnyCommissionable(),
	}
	results := make(chan []Device, len(ids))
	for _, id := range ids {
		go func() { d, _ := s.Find(context.Background(), id, 10*time.Second, false); results <- d }()
	}
	settle(t, s, len(ids))
	c.discover(addrA, serviceDataA)
	for range ids {
		want(t, <-results, addrA)
	}
	// BLE cannot select by instance id or device type (packages/protocol/src/common/BleScanner.ts:476-496).
	for _, id := range []discovery.Identifier{discovery.ByInstanceID(addrA), discovery.ByDeviceType(0x100)} {
		if d, err := s.Find(context.Background(), id, time.Second, false); err != nil || d != nil {
			t.Fatalf("Find(%v) = %v, %v", id, d, err)
		}
		if d, err := s.FindContinuously(context.Background(), id, func(Device) {}, time.Second); err != nil || d != nil {
			t.Fatalf("FindContinuously(%v) = %v, %v", id, d, err)
		}
	}
}

func TestBleScannerCancelEndsOnlyTheCanceled(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan struct{})
	go func() {
		defer close(canceled)
		_, _ = s.FindContinuously(ctx, discovery.ByShortDiscriminator(6), func(Device) {}, 0)
	}()
	waiting := make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByShortDiscriminator(6), 60*time.Second, false)
		waiting <- d
	}()
	settle(t, s, 2)
	cancel()
	<-canceled
	select {
	case <-waiting:
		t.Fatal("the other discovery ended")
	default:
	}
	c.discover(addrA, serviceDataA)
	want(t, <-waiting, addrA)
}

func TestBleScannerCancelEndsEveryDiscoveryForIdentifier(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	s := NewScanner(c, ScannerOptions{Clock: clock})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.FindContinuously(context.Background(), discovery.ByShortDiscriminator(6), func(Device) {}, 0)
		}()
	}
	settle(t, s, 2)
	s.Cancel(discovery.ByShortDiscriminator(6))
	wg.Wait()
	if c.isScanning() {
		t.Fatal("still scanning")
	}
}

// --- overlapping discoveries ---

func TestBleScannerFailsDiscoveriesWhenScanCannotStart(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	c.setStartErr(errors.New("start failed"))
	s := NewScanner(c, ScannerOptions{Clock: clock})
	for _, id := range []discovery.Identifier{discovery.ByLongDiscriminator(1737), discovery.ByLongDiscriminator(1000)} {
		if _, err := s.Find(context.Background(), id, 10*time.Second, false); err == nil || err.Error() != "start failed" {
			t.Fatalf("Find: %v", err)
		}
	}
	// A later discovery scans again once the radio starts.
	c.setStartErr(nil)
	res := make(chan []Device, 1)
	go func() {
		d, _ := s.Find(context.Background(), discovery.ByLongDiscriminator(1737), 10*time.Second, false)
		res <- d
	}()
	settle(t, s, 1)
	c.discover(addrA, serviceDataA)
	want(t, <-res, addrA)
}

// --- close ---

func TestBleScannerCloseSettlesContinuousDiscovery(t *testing.T) {
	clock := newFakeClock()
	c := newListening(clock)
	c.setStopErr(errors.New("stop failed"))
	s := NewScanner(c, ScannerOptions{Clock: clock})
	res := make(chan []Device, 1)
	go func() {
		d, _ := s.FindContinuously(context.Background(), discovery.ByLongDiscriminator(1737), func(Device) {}, 0)
		res <- d
	}()
	settle(t, s, 1)
	if err := s.Close(context.Background()); err == nil {
		t.Fatal("the stop failure was not reported")
	}
	want(t, <-res)
	// A discovery after close returns at once, without an error
	// (BleScannerTest.ts:981-999).
	if d, err := s.Find(context.Background(), discovery.ByLongDiscriminator(1737), time.Second, false); err != nil || len(d) != 0 {
		t.Fatalf("Find after Close: %v, %v", d, err)
	}
}

func TestBleScannerIgnoresNonMatterServiceData(t *testing.T) {
	c := &mockClient{}
	s := NewScanner(c, ScannerOptions{})
	c.discover(addrA, []byte{0x01, 0x02})
	want(t, s.Discovered(discovery.AnyCommissionable()))
	d := Device{Peripheral: Peripheral{Address: addrA}, Discriminator: 1737, VendorID: 4476, ProductID: 32769}
	if cd := d.Commissionable(); cd.InstanceID != addrA || cd.D != 1737 || cd.CM != 1 || cd.VP == nil || *cd.VP != "4476+32769" {
		t.Fatalf("Commissionable %+v", cd)
	}
}

// --- the cases ported at 862ad75f, one test per describe group ---

// collector gathers the addresses a continuous discovery's callback
// receives.
type collector struct {
	mu    sync.Mutex
	addrs []string
}

func (c *collector) add(d Device) {
	c.mu.Lock()
	c.addrs = append(c.addrs, d.Peripheral.Address)
	c.mu.Unlock()
}

func (c *collector) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.addrs)
}

type findResult struct {
	devices []Device
	err     error
}

func goFind(ctx context.Context, s *Scanner, id discovery.Identifier, timeout time.Duration, ignoreExisting bool) <-chan findResult {
	ch := make(chan findResult, 1)
	go func() {
		d, err := s.Find(ctx, id, timeout, ignoreExisting)
		ch <- findResult{d, err}
	}()
	return ch
}

func goFindContinuously(ctx context.Context, s *Scanner, id discovery.Identifier, cb func(Device), timeout time.Duration) <-chan findResult {
	if cb == nil {
		cb = func(Device) {}
	}
	ch := make(chan findResult, 1)
	go func() {
		d, err := s.FindContinuously(ctx, id, cb, timeout)
		ch <- findResult{d, err}
	}()
	return ch
}

func wantCalls(t *testing.T, c *mockClient, calls ...string) {
	t.Helper()
	if got := c.scanCalls(); !slices.Equal(got, calls) {
		t.Fatalf("scan calls %v, want %v", got, calls)
	}
}

func wantAddrs(t *testing.T, got []string, addrs ...string) {
	t.Helper()
	if !slices.Equal(got, addrs) {
		t.Fatalf("candidates %v, want %v", got, addrs)
	}
}

var bg = context.Background()

// TestBleScannerDeduplicationAndAging covers the rest of
// "service-data-based deduplication and aging" (BleScannerTest.ts:147-485).
func TestBleScannerDeduplicationAndAging(t *testing.T) {
	sd6 := discovery.ByShortDiscriminator(6)
	t.Run("keeps offering a peripheral while the radio does not scan, though we asked it to", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		stop := startScan(t, s)
		c.discover(addrA, serviceDataA)
		c.stopListening() // the adapter powers off under the scan
		clock.Advance(3600 * time.Second)
		want(t, s.Discovered(sd6), addrA)
		stop()
	})
	t.Run("keeps offering a peripheral that has not advertised while nothing scans", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		stop := startScan(t, s)
		c.discover(addrA, serviceDataA)
		stop()
		clock.Advance(3600 * time.Second)
		want(t, s.Discovered(sd6), addrA)
	})
	t.Run("does not hand a discovery a peripheral that stopped advertising while an earlier scan ran", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		stop := startScan(t, s)
		c.discover(addrA, serviceDataA)
		clock.Advance(61 * time.Second)
		stop()
		var got collector
		res := goFindContinuously(bg, s, sd6, got.add, 0)
		settle(t, s, 1)
		wantAddrs(t, got.get())
		_ = s.Close(bg)
		recv(t, res)
	})
	t.Run("still resolves a stale peripheral for a channel open", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		stop := startScan(t, s)
		c.discover(addrA, serviceDataA)
		clock.Advance(61 * time.Second)
		want(t, s.Discovered(sd6))
		if _, err := s.Device(addrA); err != nil {
			t.Fatal(err)
		}
		stop()
	})
	t.Run("stops offering a peripheral that goes stale while a discovery runs", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		var got collector
		res := goFindContinuously(bg, s, sd6, got.add, 0)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		wantAddrs(t, got.get(), addrA)
		clock.Advance(61 * time.Second)
		want(t, s.Discovered(sd6))
		_ = s.Close(bg)
		recv(t, res)
	})
	t.Run("ages a peripheral of a client that reports no listening time from its report after the last scan", func(t *testing.T) {
		clock := newFakeClock()
		c := &mockClient{}
		s := NewScanner(c, ScannerOptions{Clock: clock})
		startScan(t, s)()
		clock.Advance(10 * time.Minute)
		c.discover(addrA, serviceDataA)
		clock.Advance(15 * time.Minute)
		want(t, s.Discovered(sd6), addrA)
		clock.Advance(time.Second)
		want(t, s.Discovered(sd6))
	})
	t.Run("ends the scan of a discovery whose callback throws", func(t *testing.T) {
		clock := newFakeClock()
		c := &mockClient{}
		s := NewScanner(c, ScannerOptions{Clock: clock})
		c.discover(addrA, serviceDataA)
		recovered := make(chan any, 1)
		go func() {
			defer func() { recovered <- recover() }()
			_, _ = s.FindContinuously(bg, sd6, func(Device) { panic("callback failed") }, 0)
		}()
		if r := recv(t, recovered); r != "callback failed" {
			t.Fatalf("recovered %v", r)
		}
		wantCalls(t, c, "start", "stop")
		if c.isScanning() {
			t.Fatal("still scanning")
		}
		c.discover(addrB, serviceDataB)
		clock.Advance(16 * time.Minute)
		want(t, s.Discovered(discovery.ByShortDiscriminator(3)))
	})
	t.Run("keeps a peripheral of a client that reports no listening time while its scan is still starting", func(t *testing.T) {
		clock := newFakeClock()
		c := &mockClient{}
		s := NewScanner(c, ScannerOptions{Clock: clock})
		c.discover(addrA, serviceDataA)
		clock.Advance(15 * time.Minute)
		c.holdStart()
		res := goFindContinuously(bg, s, discovery.ByProduct(1), nil, 0)
		eventually(t, s, func() bool { return len(c.scanCalls()) == 1 }, "the start")
		clock.Advance(5 * time.Second)
		want(t, s.Discovered(sd6), addrA)
		c.releaseStart()
		settle(t, s, 1)
		s.Cancel(discovery.ByProduct(1))
		recv(t, res)
	})
	t.Run("forgets an aged-out peripheral when another one advertises", func(t *testing.T) {
		clock := newFakeClock()
		c := &mockClient{}
		s := NewScanner(c, ScannerOptions{Clock: clock})
		c.discover(addrA, serviceDataA)
		clock.Advance(16 * time.Minute)
		c.discover(addrB, serviceDataB)
		if _, err := s.Device(addrA); !errors.Is(err, ErrNoDevice) {
			t.Fatalf("Device(A): %v", err)
		}
		if _, err := s.Device(addrB); err != nil {
			t.Fatal(err)
		}
	})
}

// TestBleScannerForgetCommissionedDevice covers "forgetCommissionedDevice"
// (BleScannerTest.ts:487-580). matter.js passes ServerAddress values and
// skips those of another transport; ForgetCommissioned takes the BLE
// peripheral addresses alone, so a UDP address is no entry.
func TestBleScannerForgetCommissionedDevice(t *testing.T) {
	d1737, d1000 := discovery.ByLongDiscriminator(1737), discovery.ByLongDiscriminator(1000)
	noDevice := func(t *testing.T, s *Scanner, addr string) {
		t.Helper()
		if _, err := s.Device(addr); !errors.Is(err, ErrNoDevice) || !strings.Contains(err.Error(), "no device found") {
			t.Fatalf("Device(%s): %v", addr, err)
		}
	}
	t.Run("keeps offering another device advertising identical service data", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		c.discover(addrA, serviceDataA)
		clock.Advance(10 * time.Second)
		c.discover(addrB, serviceDataA)
		s.ForgetCommissioned([]string{addrB}, nil)
		want(t, s.Discovered(d1737), addrA)
	})
	t.Run("ignores addresses of another transport", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		s.ForgetCommissioned(nil, nil) // { type: "udp", ip: "fe80::1", port: 5540 }
		want(t, s.Discovered(d1737), addrA)
	})
	t.Run("stops offering a peripheral that was commissioned", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		c.discover(addrB, serviceDataB)
		s.ForgetCommissioned([]string{addrA}, nil)
		want(t, s.Discovered(d1737))
		noDevice(t, s, addrA)
		want(t, s.Discovered(d1000), addrB)
	})
	t.Run("offers a forgotten peripheral again once it advertises again", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		s.ForgetCommissioned([]string{addrA}, nil)
		c.discover(addrA, serviceDataA)
		want(t, s.Discovered(d1737), addrA)
	})
	t.Run("forgets the BLE record of a device commissioned over another transport", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		c.discover(addrB, serviceDataB)
		s.ForgetCommissioned(nil, &CommissionedIdentity{D: 1737, VP: "4476+32769"})
		noDevice(t, s, addrA)
		want(t, s.Discovered(d1000), addrB)
	})
	t.Run("matches an identity that names only the vendor", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		s.ForgetCommissioned(nil, &CommissionedIdentity{D: 1737, VP: "4476"})
		noDevice(t, s, addrA)
	})
	t.Run("keeps a record whose vendor or product differs from the identity", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		for _, vp := range []string{"4477+32769", "4476+32770", "4477"} {
			s.ForgetCommissioned(nil, &CommissionedIdentity{D: 1737, VP: vp})
		}
		if _, err := s.Device(addrA); err != nil {
			t.Fatal(err)
		}
	})
}

// TestBleScannerReachabilityCases covers the rest of "reachability"
// (BleScannerTest.ts:582-771).
func TestBleScannerReachabilityCases(t *testing.T) {
	d1737 := discovery.ByLongDiscriminator(1737)
	t.Run("refuses to hand out a peripheral the transport can no longer reach", func(t *testing.T) {
		c := newProxy(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		c.setReachable(addrA, false)
		if _, err := s.Device(addrA); !errors.Is(err, ErrNoDevice) || !strings.Contains(err.Error(), "is currently not reachable") {
			t.Fatalf("Device: %v", err)
		}
	})
	t.Run("offers a peripheral again once the transport reaches it again", func(t *testing.T) {
		c := newProxy(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		c.setReachable(addrA, false)
		want(t, s.Discovered(d1737))
		c.setReachable(addrA, true)
		want(t, s.Discovered(d1737), addrA)
		if _, err := s.Device(addrA); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ends a one-shot discovery as soon as a reachable peripheral advertises", func(t *testing.T) {
		clock := newFakeClock()
		c := newProxy(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		res := goFind(bg, s, d1737, 10*time.Second, false)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		want(t, recv(t, res).devices, addrA)
	})
	t.Run("purges a stale cached peripheral when a discovery asks to ignore existing records", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		stop := startScan(t, s)
		c.discover(addrA, serviceDataA)
		clock.Advance(61 * time.Second)
		stop()
		res := goFind(bg, s, d1737, 10*time.Second, true)
		settle(t, s, 1)
		armed(t, s, clock, 1)
		clock.Advance(11 * time.Second)
		want(t, recv(t, res).devices)
		if _, err := s.Device(addrA); err == nil {
			t.Fatal("the stale record is still stored")
		}
	})
	t.Run("hands a running discovery a peripheral the transport reaches again", func(t *testing.T) {
		c := newProxy(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		var got collector
		res := goFindContinuously(bg, s, d1737, got.add, 0)
		settle(t, s, 1)
		c.setReachable(addrA, false)
		c.discover(addrA, serviceDataA)
		yield(s)
		wantAddrs(t, got.get())
		c.setReachable(addrA, true)
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		wantAddrs(t, got.get(), addrA)
		_ = s.Close(bg)
		recv(t, res)
	})
	t.Run("hands a discovery a peripheral that became unreachable before the discovery started", func(t *testing.T) {
		c := newProxy(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		c.setReachable(addrA, false)
		var got collector
		res := goFindContinuously(bg, s, d1737, got.add, 0)
		settle(t, s, 1)
		wantAddrs(t, got.get())
		c.setReachable(addrA, true)
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		wantAddrs(t, got.get(), addrA)
		_ = s.Close(bg)
		recv(t, res)
	})
	t.Run("offers a peripheral once however often it advertises", func(t *testing.T) {
		c := newProxy(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		var got collector
		res := goFindContinuously(bg, s, d1737, got.add, 0)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		wantAddrs(t, got.get(), addrA)
		_ = s.Close(bg)
		recv(t, res)
	})
	t.Run("keeps offering peripherals for a client that states no reachability", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.discover(addrA, serviceDataA)
		want(t, s.Discovered(d1737), addrA)
	})
}

// TestBleScannerOverlapping covers the rest of "overlapping discoveries"
// (BleScannerTest.ts:798-1177).
func TestBleScannerOverlapping(t *testing.T) {
	d1737, sd6, sd7 := discovery.ByLongDiscriminator(1737), discovery.ByShortDiscriminator(6), discovery.ByShortDiscriminator(7)
	t.Run("scans again for a later discovery after a scan could not start", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.setStartErr(errors.New("start failed"))
		if _, err := s.Find(bg, d1737, 10*time.Second, false); err == nil || err.Error() != "start failed" {
			t.Fatalf("Find: %v", err)
		}
		c.setStartErr(nil)
		res := goFind(bg, s, d1737, 10*time.Second, false)
		settle(t, s, 1)
		if !c.isScanning() {
			t.Fatal("not scanning")
		}
		c.discover(addrA, serviceDataA)
		want(t, recv(t, res).devices, addrA)
		// The failed start left the radio in an unknown state, so it is stopped at once.
		wantCalls(t, &c.mockClient, "start", "stop", "start", "stop")
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
	t.Run("stops the scan once a one-shot discovery found its device", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		res := goFind(bg, s, d1737, 10*time.Second, false)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		want(t, recv(t, res).devices, addrA)
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
	t.Run("hands a one-shot discovery an advertisement that arrived while its scan started", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.holdStart()
		res := goFind(bg, s, d1737, 10*time.Second, false)
		eventually(t, s, func() bool { return len(c.scanCalls()) == 1 }, "the start")
		c.discover(addrA, serviceDataA)
		c.releaseStart()
		// No virtual time passes, so the discovery does not wait out its timeout.
		want(t, recv(t, res).devices, addrA)
	})
	t.Run("fails a discovery whose scan does not start in time and serves the next one", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		c.holdStart()
		stuck := goFind(bg, s, d1737, 60*time.Second, false)
		eventually(t, s, func() bool { return len(c.scanCalls()) == 1 }, "the start")
		armed(t, s, clock, 1)
		clock.Advance(11 * time.Second)
		if r := recv(t, stuck); !errors.Is(r.err, ErrScanTransitionTimeout) {
			t.Fatalf("stuck discovery: %v", r.err)
		}
		c.releaseStart()
		res := goFind(bg, s, d1737, 10*time.Second, false)
		settle(t, s, 1)
		c.discover(addrA, serviceDataA)
		want(t, recv(t, res).devices, addrA)
	})
	t.Run("fails a discovery that still runs when restarting its scan fails as another ends", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		ctx, end := context.WithCancel(bg)
		defer end()
		ended := goFindContinuously(ctx, s, sd6, nil, 0)
		remaining := goFindContinuously(bg, s, sd7, nil, 0)
		settle(t, s, 2)
		c.setStartErr(errors.New("restart failed"))
		end()
		// The restart is the remaining discovery's concern, so the ending one ends normally.
		if r := recv(t, ended); r.err != nil || len(r.devices) != 0 {
			t.Fatalf("ended: %v, %v", r.devices, r.err)
		}
		if r := recv(t, remaining); r.err == nil || r.err.Error() != "restart failed" {
			t.Fatalf("remaining: %v", r.err)
		}
	})
	t.Run("still stops the radio once the discoveries a failed start ended are gone", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		running := goFindContinuously(bg, s, sd6, nil, 0)
		settle(t, s, 1)
		c.setStartErr(errors.New("start failed"))
		if _, err := s.Find(bg, discovery.ByLongDiscriminator(1000), 10*time.Second, false); err == nil || err.Error() != "start failed" {
			t.Fatalf("Find: %v", err)
		}
		if r := recv(t, running); r.err == nil || r.err.Error() != "start failed" {
			t.Fatalf("running: %v", r.err)
		}
		if calls := c.scanCalls(); calls[len(calls)-1] != "stop" {
			t.Fatalf("scan calls %v end without a stop", calls)
		}
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
	t.Run("stops the radio after the only discovery's scan could not start", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.setStartErr(errors.New("start failed"))
		if _, err := s.Find(bg, d1737, 10*time.Second, false); err == nil || err.Error() != "start failed" {
			t.Fatalf("Find: %v", err)
		}
		wantCalls(t, &c.mockClient, "start", "stop")
	})
	t.Run("keeps a discovery's result when stopping its scan fails", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		res := goFind(bg, s, d1737, 10*time.Second, false)
		settle(t, s, 1)
		c.setStopErr(errors.New("stop failed"))
		c.discover(addrA, serviceDataA)
		r := recv(t, res)
		if r.err != nil {
			t.Fatal(r.err)
		}
		want(t, r.devices, addrA)
	})
	t.Run("closes even when the client's stop never settles", func(t *testing.T) {
		clock := newFakeClock()
		c := newListening(clock)
		s := NewScanner(c, ScannerOptions{Clock: clock})
		res := goFindContinuously(bg, s, sd6, nil, 0)
		settle(t, s, 1)
		c.holdStop()
		defer c.releaseStop()
		closed := make(chan error, 1)
		go func() { closed <- s.Close(bg) }()
		eventually(t, s, func() bool { return len(c.scanCalls()) == 2 }, "the stop")
		armed(t, s, clock, 1)
		clock.Advance(11 * time.Second)
		if err := recv(t, closed); !errors.Is(err, ErrScanTransitionTimeout) {
			t.Fatalf("Close: %v", err)
		}
		recv(t, res)
	})
	t.Run("does not scan for a discovery started after the scanner closed", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		_ = s.Close(bg)
		if r := recv(t, goFindContinuously(bg, s, sd6, nil, 0)); r.err != nil {
			t.Fatal(r.err)
		}
		if slices.Contains(c.scanCalls(), "start") {
			t.Fatalf("scan calls %v", c.scanCalls())
		}
	})
	t.Run("returns a one-shot discovery started after the scanner closed at once", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		_ = s.Close(bg)
		// No virtual time passes, so the discovery does not wait out its timeout.
		r := recv(t, goFind(bg, s, d1737, 60*time.Second, false))
		if r.err != nil || len(r.devices) != 0 {
			t.Fatalf("Find: %v, %v", r.devices, r.err)
		}
		if slices.Contains(c.scanCalls(), "start") {
			t.Fatalf("scan calls %v", c.scanCalls())
		}
	})
	t.Run("ends a one-shot discovery that is canceled", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		res := goFind(bg, s, d1737, 60*time.Second, false)
		settle(t, s, 1)
		s.Cancel(d1737)
		if r := recv(t, res); r.err != nil || len(r.devices) != 0 {
			t.Fatalf("Find: %v, %v", r.devices, r.err)
		}
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
	t.Run("starts the next scan only once the previous stop finished", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		first := goFindContinuously(bg, s, sd6, nil, 0)
		settle(t, s, 1)
		c.holdStop()
		s.Cancel(sd6)
		eventually(t, s, func() bool { return len(c.scanCalls()) == 2 }, "the stop")
		second := goFindContinuously(bg, s, sd7, nil, 0)
		yield(s)
		// The stop is still in flight, so the radio is not asked to start again yet.
		wantCalls(t, &c.mockClient, "start", "stop")
		c.releaseStop()
		recv(t, first)
		settle(t, s, 1)
		wantCalls(t, &c.mockClient, "start", "stop", "start")
		if !c.isScanning() {
			t.Fatal("not scanning")
		}
		_ = s.Close(bg)
		recv(t, second)
	})
	t.Run("leaves no scan running when it closes while one is starting", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		c.holdStart()
		res := goFindContinuously(bg, s, sd6, nil, 0)
		eventually(t, s, func() bool { return len(c.scanCalls()) == 1 }, "the start")
		closed := make(chan error, 1)
		go func() { closed <- s.Close(bg) }()
		yield(s)
		c.releaseStart()
		recv(t, closed)
		recv(t, res)
		wantCalls(t, &c.mockClient, "start", "stop")
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
	t.Run("asks the client to scan again after its radio stopped on its own", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		first := goFindContinuously(bg, s, sd6, nil, 0)
		settle(t, s, 1)
		c.stopListening() // the adapter power-cycles
		second := goFindContinuously(bg, s, sd7, nil, 0)
		settle(t, s, 2)
		if !c.isScanning() {
			t.Fatal("the client was not asked to scan again")
		}
		_ = s.Close(bg)
		recv(t, first)
		recv(t, second)
	})
	t.Run("keeps scanning for a discovery that still runs when another ends", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		var got collector
		running := goFindContinuously(bg, s, sd6, got.add, 0)
		ending := goFindContinuously(bg, s, sd7, nil, 0)
		settle(t, s, 2)
		if !c.isScanning() {
			t.Fatal("not scanning")
		}
		s.Cancel(sd7)
		recv(t, ending)
		if !c.isScanning() {
			t.Fatal("the scan stopped with a discovery still running")
		}
		c.discover(addrA, serviceDataA)
		settle(t, s, 1)
		wantAddrs(t, got.get(), addrA)
		s.Cancel(sd6)
		recv(t, running)
		if c.isScanning() {
			t.Fatal("still scanning")
		}
	})
}

// TestBleScannerDiscoveriesForOneIdentifier covers the rest of
// "discoveries for one identifier" (BleScannerTest.ts:1179-1309).
func TestBleScannerDiscoveriesForOneIdentifier(t *testing.T) {
	t.Run("hands an advertisement to discoveries by vendor, product and without identifier", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		ids := []discovery.Identifier{
			discovery.ByVendorProduct(4476, 32769),
			discovery.ByProduct(32769),
			discovery.ByVendor(4476),
			discovery.AnyCommissionable(),
		}
		results := make([]<-chan findResult, len(ids))
		for i, id := range ids {
			results[i] = goFind(bg, s, id, 10*time.Second, false)
		}
		settle(t, s, len(ids))
		c.discover(addrA, serviceDataA)
		for _, res := range results {
			want(t, recv(t, res).devices, addrA)
		}
	})
}

// TestBleScannerClose covers the rest of "close" (BleScannerTest.ts:1311-1341).
func TestBleScannerClose(t *testing.T) {
	d1737 := discovery.ByLongDiscriminator(1737)
	t.Run("settles a timeout-less continuous discovery with an internal cancel signal", func(t *testing.T) {
		c := newListening(newFakeClock())
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		res := goFindContinuously(bg, s, d1737, nil, 0)
		settle(t, s, 1)
		select {
		case <-res:
			t.Fatal("settled before close")
		default:
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		recv(t, res)
	})
	t.Run("releases waiters and rethrows when the client fails to stop scanning", func(t *testing.T) {
		c := newListening(newFakeClock())
		c.setStopErr(errors.New("stop failed"))
		s := NewScanner(c, ScannerOptions{Clock: newFakeClock()})
		res := goFindContinuously(bg, s, d1737, nil, 0)
		settle(t, s, 1)
		// Close owns the stop, so it alone reports the failure and the discovery simply ends.
		if err := s.Close(bg); err == nil || !strings.Contains(err.Error(), "stop failed") {
			t.Fatalf("Close: %v", err)
		}
		if r := recv(t, res); r.err != nil || len(r.devices) != 0 {
			t.Fatalf("discovery: %v, %v", r.devices, r.err)
		}
	})
}
