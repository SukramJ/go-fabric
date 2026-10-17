// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

// A port of matter.js packages/protocol/test/common/BleScannerTest.ts at
// 0ceb202d: the parser vectors (SERVICE_DATA_A/B, :12-13) and the cases
// of the record store and of the discoveries this scanner implements —
// deduplication and aging, reachability, discoveries for one identifier,
// a scan that cannot start, close. Each case names its matter.js title.
// MockTime is fakeClock; "settleDiscovery" is waiting for the waiter.

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
)

var (
	serviceDataA, _ = hex.DecodeString("00c9067c11018000") // D=1737, VP=4476+32769
	serviceDataB, _ = hex.DecodeString("00e8037c11018000") // D=1000, VP=4476+32769
)

const (
	addrA = "aa:aa:aa:aa:aa:aa"
	addrB = "bb:bb:bb:bb:bb:bb"
)

// fakeClock is a manual exchange.Clock.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c         *fakeClock
	at        time.Time
	f         func()
	done      bool
	cancelled bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) exchange.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.done && !t.cancelled
	t.cancelled = true
	return was
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for {
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at.Before(c.timers[j].at) })
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.done && !t.cancelled && !t.at.After(target) {
				next = t
				break
			}
		}
		if next == nil {
			break
		}
		next.done = true
		c.now = next.at
		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

// mockClient is MockOneShotBleScannerClient: no listening time.
type mockClient struct {
	mu        sync.Mutex
	cb        func(Peripheral, []byte)
	scanning  bool
	starts    int
	startErr  error
	stopErr   error
	connectFn func(Peripheral) (Connection, error)
}

func (m *mockClient) SetDiscoveryCallback(f func(Peripheral, []byte)) {
	m.mu.Lock()
	m.cb = f
	m.mu.Unlock()
}

func (m *mockClient) StartScanning(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts++
	if m.startErr != nil {
		return m.startErr
	}
	m.scanning = true
	return nil
}

func (m *mockClient) StopScanning(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanning = false
	return m.stopErr
}

func (m *mockClient) Connect(_ context.Context, p Peripheral) (Connection, error) {
	if m.connectFn != nil {
		return m.connectFn(p)
	}
	return nil, errors.New("no connection")
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
// scanned (BleListeningClock).
type listeningClient struct {
	mockClient
	clock   *fakeClock
	total   time.Duration
	started time.Time
}

func (l *listeningClient) StartScanning(ctx context.Context) error {
	if err := l.mockClient.StartScanning(ctx); err != nil {
		return err
	}
	l.mu.Lock()
	if l.started.IsZero() {
		l.started = l.clock.Now()
	}
	l.mu.Unlock()
	return nil
}

func (l *listeningClient) StopScanning(ctx context.Context) error {
	l.mu.Lock()
	if !l.started.IsZero() {
		l.total += l.clock.Now().Sub(l.started)
		l.started = time.Time{}
	}
	l.mu.Unlock()
	return l.mockClient.StopScanning(ctx)
}

func (l *listeningClient) ListeningTime() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.total
	if !l.started.IsZero() {
		t += l.clock.Now().Sub(l.started)
	}
	return t
}

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

func newListening(clock *fakeClock) *listeningClient { return &listeningClient{clock: clock} }

func (s *Scanner) waiterCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waiters)
}

// settle waits until n discoveries wait for an advertisement.
func settle(t *testing.T, s *Scanner, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.waiterCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d discoveries waiting", s.waiterCount(), n)
		}
		time.Sleep(time.Millisecond)
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
	return &proxyClient{listeningClient: listeningClient{clock: clock}, unreachable: map[string]bool{}}
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
	// BLE cannot select by instance id or device type (BleScanner.ts:476-496).
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
	c.startErr = errors.New("start failed")
	s := NewScanner(c, ScannerOptions{Clock: clock})
	for _, id := range []discovery.Identifier{discovery.ByLongDiscriminator(1737), discovery.ByLongDiscriminator(1000)} {
		if _, err := s.Find(context.Background(), id, 10*time.Second, false); err == nil || err.Error() != "start failed" {
			t.Fatalf("Find: %v", err)
		}
	}
	// A later discovery scans again once the radio starts.
	c.mu.Lock()
	c.startErr = nil
	c.mu.Unlock()
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
	c.stopErr = errors.New("stop failed")
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
	if _, err := s.Find(context.Background(), discovery.ByLongDiscriminator(1737), time.Second, false); !errors.Is(err, ErrScannerClosed) {
		t.Fatalf("Find after Close: %v", err)
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
