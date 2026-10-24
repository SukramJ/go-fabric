# read-flake-sweep (inventory; locations and verbatim snippets only)

## 1 Pacing model

### fabrictest/site.go constants, lines 22-41 and 43-69 (Options), 119-150 (NewSite)
```go
 // DefaultStart is the instant a site's [clocktest.Fake] starts at when the
 // test supplies no clock.
 var DefaultStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
 
 // DefaultSettle is the quiet period of [Site.Settle], and of [Site.Run]
 // before it moves a fake clock to its next deadline.
 const DefaultSettle = 10 * time.Millisecond
 
 // DefaultWait is how long [Site.Eventually] and [Site.Settle] wait, on the
 // site's wall clock ([Options].Wall), when their context has no deadline
 // of its own: a condition that never holds ends the test with an error
 // instead of hanging it until go test's timeout.
 const DefaultWait = 30 * time.Second
 
 // ErrWaited is the error of a wait without a context deadline that ran
 // out of the site's own ([Options].Wait).
 var ErrWaited = errors.New("fabrictest: the site's wait ran out (Options.Wait; the context had no deadline)")
 
 // closeTimeout bounds the site's teardown of one host.
 const closeTimeout = 10 * time.Second
 
 // Options configures a [Site].
 type Options struct {
 	// Clock is the time source of every device and controller of the site;
 	// nil is a new [clocktest.Fake] at [DefaultStart]. A test that drives
 	// parts of its own on the system clock against a device of the site
 	// passes clock.System{}.
 	Clock clock.Clock
 	// Net shapes the in-memory network's delivery; [Network.Set] changes
 	// it later. Ignored with Loopback.
 	Net NetOptions
 	// Loopback runs the site's hosts on real UDP sockets on the loopback
 	// address ([::1], an ephemeral port each) instead of the in-memory
 	// network, for a test that needs sockets. The site still answers its
 	// controllers' DNS-SD queries from what the devices publish.
 	Loopback bool
 	// Settle is the quiet period of [Site.Settle]; 0 is [DefaultSettle].
 	Settle time.Duration
 	// Wait bounds [Site.Eventually] and [Site.Settle] on the Wall clock
 	// when their context has no deadline; 0 is [DefaultWait]. A context
 	// with a deadline is waited on as it is.
 	Wait time.Duration
 	// Wall is the clock the quiet period is measured on and the quiet
 	// check paces itself by — the machine's time, whatever Clock the hosts
 	// run on, since "nothing ran for 10 ms" is a statement about the
 	// process; nil is [clock.System].
 	Wall clock.Clock
 	// StoreOnDisk keeps each device's state in a SQLite file in the test's
```
NewSite (site.go:121-150):
```go
func NewSite(tb testing.TB, opts Options) *Site {
	tb.Helper()
	s := &Site{tb: tb, opts: opts, clk: opts.Clock, wall: clock.Or(opts.Wall), log: opts.Logger, nextIndex: 1}
	if s.clk == nil {
		s.fake = clocktest.New(DefaultStart)
		s.clk = s.fake
	} else if f, ok := s.clk.(*clocktest.Fake); ok {
		s.fake = f
	}
	if s.log == nil {
		// A test site is quiet by design: its hosts log only to a logger
		// the test hands it (Options.Logger), never to slog.Default() as a
		// library component's nil logger does (docs/guides/observability.md).
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.opts.Wait <= 0 {
		s.opts.Wait = DefaultWait
	}
	if s.opts.Settle <= 0 {
		s.opts.Settle = DefaultSettle
	}
	if !opts.Loopback {
		s.net = newNetwork(s.clk, opts.Net)
	}
	s.dir = tb.TempDir()
	s.inSyscall.Store(goroutinesInSyscall())
	tb.Cleanup(s.Close)
	return s
}

```
Other constants: site.go:41 closeTimeout = 10 * time.Second (teardown); controller.go:481 pairTimeout = 90 * time.Second (wall clock, via context.WithTimeout at controller.go:498); node.go:46 commissionDiscoveryTimeout = 3 * time.Minute (site clock, AfterFunc node.go:30).

### Settle, waitUntil, quiet, busyMetrics, busy (site.go:235-329)
```go
// Settle waits until the site is quiet for the settle period
// ([Options].Settle): no datagram sent and none queued unread on the
// in-memory network, and no goroutine of the test process runnable,
// running (but the one waiting) or in a system call. It moves no clock.
// A ctx without a deadline is bounded by [Options].Wait ([ErrWaited]).
func (s *Site) Settle(ctx context.Context) error { return s.quiet(ctx, nil, s.waitUntil(ctx)) }

// waitUntil is the wall-clock instant a wait on ctx gives up at: the zero
// time for a ctx with a deadline (ctx bounds it), now plus [Options].Wait
// otherwise.
func (s *Site) waitUntil(ctx context.Context) time.Time {
	if _, ok := ctx.Deadline(); ok {
		return time.Time{}
	}
	return s.wall.Now().Add(s.opts.Wait)
}

// quiet is Settle that also returns, with nil, once done is closed, and
// with [ErrWaited] once the wall clock passes until (none when zero).
func (s *Site) quiet(ctx context.Context, done <-chan struct{}, until time.Time) error {
	wall := s.wall
	since := wall.Now()
	lastSent := s.sent()
	samples := make([]metrics.Sample, len(busyMetrics))
	for i, name := range busyMetrics {
		samples[i].Name = name
	}
	for {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !until.IsZero() && !wall.Now().Before(until) {
			return ErrWaited
		}
		if sent := s.sent(); sent != lastSent || s.pending() > 0 || busy(samples, &s.inSyscall) {
			lastSent, since = sent, wall.Now()
		} else if clock.Since(wall, since) >= s.opts.Settle {
			return nil
		}
		clock.Sleep(wall, time.Millisecond, done)
	}
}

// busyMetrics are the goroutine states of work in progress.
var busyMetrics = []string{
	"/sched/goroutines/runnable:goroutines",
	"/sched/goroutines/running:goroutines",
	"/sched/goroutines/not-in-go:goroutines",
}

// busy reports whether a goroutine other than the caller is runnable,
// running or in a system call: a host still working on a message, a
// handshake's cryptography, a store write. It is the Go reading of
// MockTime's "the task queue is empty" — the process-wide count, so the
// work of a parallel test in the same binary delays the step too. The
// standing goroutines in a system call (standing, the fewest seen) are not
// work; a reading below it lowers it.
func busy(samples []metrics.Sample, standing *atomic.Uint64) bool {
	metrics.Read(samples)
	var n, inSyscall uint64
	for i := range samples {
		if samples[i].Value.Kind() == metrics.KindUint64 {
			n += samples[i].Value.Uint64()
			if samples[i].Name == "/sched/goroutines/not-in-go:goroutines" {
				inSyscall = samples[i].Value.Uint64()
			}
		}
	}
	for {
		was := standing.Load()
		if inSyscall >= was || standing.CompareAndSwap(was, inSyscall) {
			break
		}
	}
	return n > 1+standing.Load()
}

func (s *Site) sent() uint64 {
	if s.net == nil {
		return 0
	}
	return s.net.Sent()
}

func (s *Site) pending() int {
	if s.net == nil {
		return 0
	}
	return s.net.pending()
}

```
### Run (site.go:330-364) and Eventually (site.go:366-388)
```go
// Run runs fn and, while it has not returned, moves a fake site clock
// forward: each time the site has been quiet for the settle period
// ([Site.Settle]) it advances the clock to its next deadline, so a timer
// fn waits on — a standalone acknowledgement, a retransmission, a
// report's interval — fires. It returns fn's error, or ctx's when ctx ends
// first. With a clock that is not a [clocktest.Fake] it only runs fn.
//
// This is MockTime.resolve (packages/testing/src/mocks/time.ts), under
// which the matter.js site commissions (mock-site.ts:156): time moves only
// while nothing else can, read in Go as "no datagram in the network and no
// goroutine working".
func (s *Site) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.fake == nil {
		return fn(ctx)
	}
	finished := make(chan struct{})
	var result error
	go func() {
		defer close(finished)
		result = fn(ctx)
	}()
	for {
		if err := s.quiet(ctx, finished, time.Time{}); err != nil {
			return err
		}
		select {
		case <-finished:
			return result
		default:
		}
		if at, ok := s.fake.Next(); ok {
			s.fake.Advance(max(at.Sub(s.fake.Now()), 0))
		}
	}
}

// Eventually runs [Site.Run] until cond holds and returns an error naming
// what when ctx ends first — or, for a ctx without a deadline, once
// [Options].Wait has passed on the wall clock ([ErrWaited]). A test waits
// for the effect of a change — a report that reached the controller, a
// session that closed — with it.
func (s *Site) Eventually(ctx context.Context, what string, cond func() bool) error {
	until := s.waitUntil(ctx)
	for !cond() {
		if err := s.quiet(ctx, nil, until); err != nil {
			return fmt.Errorf("fabrictest: waiting for %s: %w", what, err)
		}
		if cond() {
			return nil
		}
		if s.fake == nil {
			continue // the wall clock moves by itself; quiet paced the loop
		}
		if at, ok := s.fake.Next(); ok {
			s.fake.Advance(max(at.Sub(s.fake.Now()), 0))
		}
	}
	return nil
}
```
### Device.Start / Stop / Restart
Start device.go:156 (signature); the wait: device.go:193-200:
```go
	}
	// Under Site.Run: the node's start re-establishes its former
	// subscriptions and waits for each peer's StatusResponse on a timer
	// of the site's clock (bridge.ReestablishFormerSubscriptions), and a
	// fake clock moves only while the site paces it — a start called
	// directly would wait forever on a response that never comes.
	if err := d.site.Run(ctx, n.Start); err != nil {
		return fmt.Errorf("start: %w", err)
```
Stop device.go:285-298:
```go
// Stop stops the node; its store and address stay the device's. A
// stopped device is not an error.
func (d *Device) Stop(ctx context.Context) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	d.mu.Lock()
	n, running := d.n, d.running
	d.running = false
	d.mu.Unlock()
	if !running {
		return nil
	}
	return d.site.Run(ctx, n.Stop) // the stop's own timers on the site's clock, as Start
}
```
Restart device.go:300-309:
```go
// Restart stops the node and starts a new one on the same store and
// address, serving the same topology: what survives is what the node
// persisted — fabrics, ACL, subscriptions, endpoint numbers.
func (d *Device) Restart(ctx context.Context) error {
	if err := d.Stop(ctx); err != nil {
		return fmt.Errorf("fabrictest: stop: %w", err)
	}
	return d.Start(ctx)
}

```
Teardown waits: device.go:136 and :143 context.WithTimeout(context.Background(), closeTimeout); the AddDevice start at device.go:143-146 uses that 10 s wall ctx; Stop at device.go:137 on site close.

### Waits in controller.go / node.go / raw.go / wire.go / pase.go (grep Eventually|Settle|Run(|time.|Wall|ctx.Done)
- fabrictest/controller.go:141:		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
- fabrictest/controller.go:243:		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
- fabrictest/controller.go:262:	if err := c.site.Run(ctx, func(ctx context.Context) error {
- fabrictest/controller.go:418:	err := c.site.Run(ctx, func(ctx context.Context) error {
- fabrictest/controller.go:498:	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
- fabrictest/device.go:136:		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
- fabrictest/device.go:143:	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
- fabrictest/device.go:199:	if err := d.site.Run(ctx, n.Start); err != nil {
- fabrictest/device.go:297:	return d.site.Run(ctx, n.Stop) // the stop's own timers on the site's clock, as Start
- fabrictest/wire.go:35:func run(ctx context.Context, p *Pair, fn func(ctx context.Context) error) error {
- fabrictest/wire.go:39:	return p.Controller.site.Run(ctx, fn)
- fabrictest/wire.go:47:	err := run(ctx, p, func(ctx context.Context) error {
- fabrictest/wire.go:85:	err := run(ctx, p, func(ctx context.Context) error {
- fabrictest/wire.go:162:	return run(ctx, p, func(ctx context.Context) error {
- fabrictest/wire.go:188:	err := run(ctx, p, func(ctx context.Context) error {
- fabrictest/wire.go:202:	err := run(ctx, p, func(ctx context.Context) error {
- fabrictest/pase.go:28:	err := c.site.Run(ctx, func(ctx context.Context) error {
- fabrictest/node.go:26:	err := c.site.Run(ctx, func(ctx context.Context) error {
- fabrictest/node.go:29:		t := c.site.clk.AfterFunc(timeout, cancel)
- fabrictest/node.go:71:	if err := c.site.Run(ctx, func(ctx context.Context) error { return reg.Commission(ctx, n, o) }); err != nil {
- fabrictest/raw.go:26:	err := run(ctx, p, func(ctx context.Context) error {
- fabrictest/raw.go:54:	return run(ctx, p, func(ctx context.Context) error {
- wire.go:35-40 run(): returns ErrNotCommissioned when p is nil or neither Commissioned nor PASESession; else p.Controller.site.Run(ctx, fn)
- wire.go:46-57 Connect: Peer.Connect only if !p.Peer.HasSession()
- controller.go:141, :243 closeTimeout contexts on start-failure and Close; controller.go:259-270 Controller.Restart runs c.shutdown under site.Run
- controller.go:430 Pair{..., Interaction: c.com.Interaction(p)} (the Pair's Interaction is built on the Peer)
- node.go:30 t := c.site.clk.AfterFunc(timeout, cancel)  (site clock, i.e. fake by default)
- raw.go: no direct waits; Exchange (:24) and Send (:53) call run()
- Wall clock users in fabrictest non-test: site.go:97 (field), :123 (clock.Or(opts.Wall)), :247 waitUntil, :255-285 quiet. fabrictest/wall_internal_test.go:34,61 are the only tests setting Wall:.

## 2 Clock use in production code the site paces

### Direct real-time uses (time.Now/After/NewTimer/NewTicker/Sleep/Tick/AfterFunc, context.WithTimeout/WithDeadline, Set*Deadline), non-test
- bridge:
    bridge/securechannel.go:1297:		ctx, cancel := context.WithTimeout(base, timeout)
    bridge/subscription_persistence.go:139:	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
    bridge/subscription_persistence.go:213:	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subscriptionStoreTimeout)
    bridge/subscription_persistence.go:232:	ctx, cancel := context.WithTimeout(context.Background(), subscriptionStoreTimeout)
    bridge/subscription_persistence.go:253:	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subscriptionStoreTimeout)
    bridge/group_messaging.go:152:	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    bridge/bridge.go:1792:		pumpStopCtx, pumpStopCancel := context.WithTimeout(context.Background(), pumpStopGrace)
    bridge/bridge.go:1818:			withdrawCtx, withdrawCancel := context.WithTimeout(context.Background(), b.cfg.AdvertiseTimeout)
    bridge/bridge.go:2057:	pubCtx, cancel := context.WithTimeout(ctx, b.cfg.AdvertiseTimeout)
    bridge/bridge.go:2102:	wCtx, cancel := context.WithTimeout(ctx, b.cfg.AdvertiseTimeout)
    bridge/bridge.go:2162:	pubCtx, cancel := context.WithTimeout(ctx, b.cfg.AdvertiseTimeout)
    bridge/bridge.go:2230:	withdrawCtx, cancel := context.WithTimeout(ctx, b.cfg.AdvertiseTimeout)
    bridge/ota_provider_link.go:62:	ctx, cancel := context.WithTimeout(ctx, otaProviderConnectTimeout)
    bridge/ota_provider_link.go:127:	waitCtx, cancel := context.WithTimeout(ctx, s.b.chunkStatusResponseTimeout(s.link.sessionID))
    bridge/subscription_reestablish.go:165:	connCtx, cancel := context.WithTimeout(ctx, subscription.ReestablishTimeout)
    bridge/bridgetest/commissioner.go:275:		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
    bridge/bridgetest/commissioner.go:609:	return context.WithTimeout(ctx, defaultTimeout)
    bridge/bridgetest/session.go:65:		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
- im/subscription:
    (none)
- im:
    (none)
- node:
    node/node.go:798:	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
- client/exchange:
    (none)
- client/interaction:
    (none)
- client/peer:
    (none)
- transport/mrp:
    (none)
- transport/udp:
    (none)
- secure/channel:
    (none)
- mdns:
    mdns/subtype_responder.go:361:		_ = r.pc4.SetReadDeadline(time.Now().Add(time.Second))
    mdns/subtype_responder.go:389:		_ = r.pc6.SetReadDeadline(time.Now().Add(time.Second))
- groups:
    (none)
- Other real-time constructs outside the forms above: bridge/health_probe.go:72 StartHealthProbeWithClock(ctx, clock.System{}, ...); groups/manager.go:191 clock: clock.System{}; node/security.go:440 sessions.StartReaper(ctx, operational.SessionIdleTimeout, time.Minute) (duration only); mdns/subtype_responder.go:361,389 SetReadDeadline(time.Now().Add(time.Second)).
- Counts above for bridge include bridge/bridgetest (session.go:65, commissioner.go:275, :609); bridge non-bridgetest direct WithTimeout count: 15.

### Injected clock use per file (matches of clock.Or( | .Clock() | clk. | c.clock. | .clock.), non-test, count per file
- bridge:
    bridge/pase_provider.go:5
    bridge/bdx_responder.go:2
    bridge/health_probe.go:1
    bridge/outbound_reliable.go:2
    bridge/bridge.go:2
    bridge/commissioning_window.go:5
    bridge/case_provider.go:5
- im/subscription:
    im/subscription/manager.go:1
- im:
    im/eventlog.go:1
    im/subscription/manager.go:1
- node:
    node/root.go:1
    node/persist.go:1
    node/security.go:6
    node/ota.go:4
    node/commissioning.go:1
    node/node.go:1
- client/exchange:
    client/exchange/exchange.go:13
    client/exchange/manager.go:3
    client/exchange/session.go:3
- client/interaction:
    client/interaction/sustained.go:1
    client/interaction/subscriptions.go:2
- client/peer:
    client/peer/monitor.go:2
    client/peer/connection.go:10
- transport/mrp:
    (none)
- transport/udp:
    (none)
- secure/channel:
    (none)
- mdns:
    mdns/subtype_responder.go:3
    mdns/host_responder.go:1
    mdns/zeroconf.go:4
    mdns/operational_resolver.go:2
- groups:
    groups/manager.go:2
- transport/mrp, transport/udp, secure/channel: no direct real-time forms and no clock.* / Clock matches in non-test code (grep 'Now|AfterFunc|Clock|clock\.' empty).

### clock.Or( fallbacks to the wall clock (clock/clock.go:99-105: Or returns System{} when c is nil)
- bridge/health_probe.go:84:	c = clock.Or(c)
- bridge/outbound_reliable.go:102:		clock:           clock.Or(clk),
- bridge/bridge.go:764:		clock:          clock.Or(cfg.Clock),
- bridge/bridge.go:1567:func (b *Bridge) clk() clock.Clock { return clock.Or(b.clock) }
- bridge/bdx_responder.go:195:	return &responderTurn{idleAt: -1, changed: make(chan struct{}), clock: clock.Or(clk)}
- im/eventlog.go:196:		rec.EpochMS = uint64(clock.Or(l.clock).Now().UnixMilli()) //nolint:gosec // the clock's time is non-negative; see #20
- node/root.go:348:		if chain, err = attestation.BuildTestChainAt(pd.VendorID, pd.ProductID, clock.Or(cfg.Clock).Now()); err != nil {
- im/subscription/manager.go:209:	cfg.Clock = clock.Or(cfg.Clock)
- node/ota.go:155:	t := clock.Or(clk).NewTimer(d)
- node/ota.go:200:	clk := clock.Or(a.clock)
- node/persist.go:304:	tick := clock.Or(clk).NewTicker(interval)
- node/security.go:272:	verifier, err := mattercert.NewVerifier(fabric.RootPublicKey, clock.Or(c.clock))
- node/node.go:155:	cfg.Clock = clock.Or(cfg.Clock)
- controller/ca/ca.go:264:	cfg.Clock = clock.Or(cfg.Clock)
- transport/tcp/listener.go:123:		clock:          clock.Or(cfg.Clock),
- transport/btp/session.go:239:		role: role, cb: cb, clock: clock.Or(opts.TimeSource), log: opts.Logger,
- secure/operational/manager.go:350:	now := clock.Or(e.clock).Now()
- secure/operational/manager.go:425:	e.lastActivity = clock.Or(e.clock).Now()
- groups/manager.go:171:	m.clock = clock.Or(c)

## 3 The flaky tests

All sites below use the default fake clock (no `Clock:` and no `Wall:` set) unless stated. Only fabrictest/wall_internal_test.go:34,61 sets `Wall:`; fabrictest/accessors_test.go:23 sets `Clock: clock.System{}`.

### 3.1 fabrictest.TestHostStatementName
- Test: fabrictest/host_statement_test.go:26 (t.Parallel :27). Site: `fabrictest.NewSite(t, fabrictest.Options{})` :28 (fake clock, wall = System). ctx: `context.WithTimeout(context.Background(), 30*time.Second)` :33.
- Commissioning: `site.CommissionedPair(...)` :32 (90 s wall ctx, controller.go:498).
- `label()` :39-47 calls `fabrictest.ReadCluster(ctx, pair, ep, bridgedinfodef.ClusterID)` (wire.go:105 -> ReadWith -> run() -> Site.Run, wire.go:35-40); the wait is Site.Run's quiet/advance loop under the 30 s ctx; no Eventually/Settle in the test.
- Restart: `pair.Device.Restart(ctx)` :89 (Stop under Site.Run device.go:297, then Start under Site.Run device.go:199); the next `label()` is :92 (`if got := label(); got != "Coffee"` is :92; the read inside `label()` is :40 / t.Fatal :42).
- Waits for: a ReadCluster read over the controller's pair.Interaction (built on pair.Peer, controller.go:430) after the device restarted; no step in the test waits for the controller's peer to notice the restart.

### 3.2 controller/client.TestTypedReadsConsumer
- Test: controller/client/typed_reads_consumer_test.go:45 (t.Parallel :46). ctx 30 s wall `context.WithTimeout` :47.
- Site: `fabrictest.NewSite(t, fabrictest.Options{})` :49 (fake clock). No holdSite call in this test (holdSite exists at controller/client/parity_matterjs_helpers_test.go:45 and is called at live_test.go:43, parity_matterjs_helpers_test.go:55).
- Device: `site.AddDevice(...)` :68; controller `site.AddController` :69; `ctl.CommissionNode(ctx, dev, client.CommissionOptions{})` :70 (node.go:62-75: Discover under Site.Run, Registry.Commission under Site.Run).
- The failing wait, :127-131:
    
    	// The host switches the light off and dims it: both reach the image,
    	// in the generated types.
    	if err := dev.Act(func(*node.Node) error {
    		return errors.Join(light.OnOff().Set(onoffdef.Patch{OnOff: new(false)}),
    			light.LevelControl().Set(levelcontroldef.Patch{CurrentLevel: new(uint8(20))}))
    	}); err != nil {
    		t.Fatal(err)
    	}
    	if err := site.Eventually(ctx, "the OnOff and level reports", func() bool {
    		l := level().CurrentLevel
    		return !onOff().OnOff && l != nil && *l == 20
    	}); err != nil {
    		t.Fatalf("%v: OnOff %+v, level %+v", err, onOff(), level())
    	}
- Waits for: a report (subscription created at :76-ish via cn.Subscribe) carrying OnOff false and CurrentLevel 20 after `dev.Act` (:121-125) sets both on the device; Site.Eventually(ctx 30 s wall, fake clock advanced to fake.Next() between quiet periods).
- No Device.Restart/Stop in the test (grep Restart|Stop: only `Stop` at the interface method of a different type; none in this file).

### 3.3 node.TestLevelControlTransitionReachesHostAndSubscriber
- Test: node/gate2_defects_test.go:301-ish (func line: `grep -n` below), t.Parallel, `ctx := parityCtx(t)` (node/parity_matterjs_helpers_test.go:34-39: 90 s wall WithTimeout), `site := quietSite(t)` (parity_matterjs_helpers_test.go:84-88: holdSite + `fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})`, fake clock, default Settle 10 ms).
- Failing wait, gate2_defects_test.go:321-334 (verbatim above in this file's section 3.3 snippet):
    				t.Errorf("typed OnChanged saw %v, want %v", got, want)
    			}
    			if got := d.untyped.get(); !slices.Equal(got, want) {
    				t.Errorf("Device.OnChanged saw %v, want %v", got, want)
    			}
    			waitFor(t, site, "the command's level report", func() bool { return slices.Contains(reports.get(), 200) })
    			waitFor(t, site, "OnChangedAsync of the command's level", func() bool { return slices.Equal(d.as.get(), want) })
    		})
    	}
    }
    
    // TestLevelControlTransitionReachesHostAndSubscriber is defect 1 for a
    // managed transition (Declare.Transitions): every step reaches the host
    // from OriginCommand, rising, the target once; the subscriber is reported
    // the target at the end.
    func TestLevelControlTransitionReachesHostAndSubscriber(t *testing.T) {
    	t.Parallel()
    	ctx := parityCtx(t)
    	site := quietSite(t)
    	d := newDimmable(t, nil, true)
    	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{d.light}}})
    	ep, _ := d.light.EndpointNumber()
    	reports := subscribeLevel(t, pair, ep)
    	move := im.ConcreteCommandPath{Endpoint: ep, Cluster: levelcontroldef.ClusterID, Command: levelcontroldef.CmdMoveToLevel}
    	// 2 s
    	if _, err := fabrictest.Invoke(ctx, pair, move, levelcontroldef.MoveToLevelRequest{Level: 200, TransitionTime: spec.ValueOf(uint16(20))}); err != nil {
    		t.Fatal(err)
    	}
    	waitFor(t, site, "the transition's end", func() bool {
    		seen := d.typed.get()
321:func TestLevelControlTransitionReachesHostAndSubscriber(t *testing.T) {
- waitFor helper node/gate2_defects_test.go:248-258:
    func waitFor(t *testing.T, site *fabrictest.Site, what string, cond func() bool) {
    	t.Helper()
    	short, cancel := context.WithTimeout(parityCtx(t), 10*time.Second)
    	defer cancel()
    	if err := site.Eventually(short, what, func() bool {
    		site.Fake().Advance(100 * time.Millisecond)
    		return cond()
    	}); err != nil {
    		t.Fatal(err)
    	}
    }
- Waits for: the typed OnChanged recorder d.typed to see a last Level == 200 after a MoveToLevel with TransitionTime 20 (tenths: 2 s of fake time); waitFor wraps Site.Eventually in a 10 s wall ctx (`short`, :250) and itself calls `site.Fake().Advance(100 * time.Millisecond)` at every cond evaluation (:253).
- No Device.Restart/Stop in the test.

### 3.4 client/interaction.TestLiveSustainedSurvivesRestart (note: package client/interaction, not controller/client)
- Test: client/interaction/live_test.go:328 (t.Parallel :329). Site via `newLiveOn(t, discardLog(), fabrictest.Options{StoreOnDisk: true})` :330 -> holdSite(t) (live_test.go:~85, siteSlots chan size 1, comment :74 names this test "a named flake of gate 1") + `fabrictest.NewSite(t, opts)`; fake clock; no Wall.
- Waits: lv.run (= site.Run, live_test.go:121-123) Subscribe :333-340 under context.Background(); waitOnOff :149-165 (Site.Eventually, 30 s wall ctx :151) at :344, :352, :359; `lv.wait("the subscription active", 10*time.Second, sub.Active)` :345 (live_test.go:170-177 Site.Eventually); waitPersisted(t, lv, 1) :346 (live_test.go:~390-404, 30 s); restartCtx 30 s wall :350-351; `lv.run(restartCtx, lv.pair.Device.Restart)` :352 (Restart involved); assertion on the newest session :355-358 (`s.Addr().String() != lv.pair.Device.Bridge().LocalAddr()`) and :359-361 `sub.ID() != id || !sub.Active()`.
- Waits for: after Device.Restart the node's re-established subscription (CASE initiated by the device to the controller), the re-established priming report (waitOnOff false), then a Toggle's report (waitOnOff true :364).

### 3.5 controller/client.TestLiveClientNode
- Test: controller/client/live_test.go:168 (t.Parallel :169). `lv := newPairLive(t)` :170 -> live_test.go:41-68: holdSite :43, `fabrictest.NewSite(t, fabrictest.Options{})` :44 (fake clock), onoff server `TimeSource: site.Clock()`. ctx 3 min wall :171.
- Waits (all `h.waitFor`/`h.waitState` -> pairLive.eventually live_test.go:70-77 = Site.Eventually under a 60 s wall ctx): waitState Connected :200-ish, waitFor "OnOff true" :259, Device Restart `lv.dev.Restart(ctx)` :263 (Restart involved), then `h.waitFor(t, lv, "the subscription after the restart", n.SubscriptionActive)` :267, "OnOff false after the restart" :269, "a re-subscription" :281-285, "the re-established subscription" :292, "the plug's erasure" :301; `lv.site.Run(ctx, n.Start)` :193.
- Waits for: client.Node's subscription to be active again after the device restarted (Node.SubscriptionActive), the re-subscription request count to rise, and OnOff false visible in the image.

### 3.6 controller/client.TestRegistryDecommission
- Test: controller/client/registry_test.go:296 (t.Parallel :297). No fabrictest: `newScriptedRegistry(t, seedCommissioned(0), nil)` :298 (registry_test.go: sr.clock is `*fakeClock` = `clocktest.Fake`, helpers_test.go:21-25, `newFakeClock()` = clocktest.New(time.Unix(3_000_000, 0))); scripted conn.
- Waits: `sr.next(t)` :305, :315 (helpers_test.go:93-116: select on scripted conn channel vs `time.After(scriptedHangBound)` at helpers_test.go:114, scriptedHangBound = time.Minute :89 real time); `<-done` :321 (no timeout); Decommission runs in a goroutine `go func() { done <- sr.reg.Decommission(context.Background(), n) }()` :304.
- No Device.Restart/Stop; no site; the registry on a fake clock that no one advances in this test (grep Advance in :296-339: none).

### 3.7 controller/commissioner errWon
- Definition: controller/commissioner/connection.go:305-306 `var errWon = errors.New("commissioner: another PASE attempt won")`; set at connection.go:284 `c.abort(errWon)`; winner recorded :283; loser branch :277-281 (`loser, loserCause = s, context.Cause(actx)`; `if loserCause == nil { loserCause = errLostRace }`); `loser.HandlePeerLoss(loserCause)` :296.
- No test file references errWon or the string "another PASE attempt won" (grep over *.go outside connection.go: no match). The notes name it as "controller/commissioner errWon surfacing" (see section 5). Test files in controller/commissioner using the connection: controller/commissioner/connection_test.go, connection_parity_matterjs_test.go, node_live_test.go, node_test.go.
- notes/briefs/reports/BY-2.md:238: "The commissioner `errWon` flake: not reproduced in 20 runs."

### 3.8 node.TestOtaParityMatterJS
- Test: node/ota_parity_matterjs_test.go:399; first statement `holdSite(t)` :400 (no t.Parallel). Each subtest calls `newOTASite(t, otaSiteOptions{})` (:404, :441, :499, :537, :571, :639) -> `steppedSite(t)` (parity_matterjs_helpers_test.go:92-96: holdSite + `fabrictest.NewSite(t, fabrictest.Options{Logger: ..., Settle: 2 * time.Millisecond})`, fake clock).
- Waits: `s.eventually` (ota_parity_matterjs_test.go:343-355): `ctx := parityCtx(t)` (90 s wall), loop { `s.site.Settle(ctx)`; if cond return; `s.site.Fake().Advance(otaStep)` } with `const otaStep = time.Second` :340. It uses Settle, not Site.Eventually.
- Restarts: newOTASite `s.req.Restart(parityCtx(t))` ~:235; `restartInto` :371-380 = `s.site.Run(parityCtx(t), s.req.Stop)` then `s.site.Run(parityCtx(t), s.req.Start)`; used at :412 ("s.restartInto(t, 1)"). 5 matches of restartInto|.Restart( in the file.
- Waits for: applyUpdate recorded, transitions count, NotifyUpdateApplied status after the requestor device restarts into version 1 (:410-414).

### 3.9 controller/client registry hook, registry.go:550-582 (verbatim)
    // before the host's Event.
    func (r *Registry) hooksFor(id string, holder *nodeHolder) Hooks {
    	h := r.cfg.Node.Hooks
    	if r.cfg.HooksFor != nil {
    		h = r.cfg.HooksFor(id)
    	}
    	host := h.Event
    	h.Event = func(endpoint uint16, cluster, event uint32, e EventData, value any) {
    		if n := holder.get(); n != nil {
    			r.onEvent(n, endpoint, cluster, event, value)
    		}
    		if host != nil {
    			host(endpoint, cluster, event, e, value)
    		}
    	}
    	hostAlive := h.SubscriptionAlive
    	h.SubscriptionAlive = func() {
    		if n := holder.get(); n != nil && n.latchLabel() {
    			r.goWork(func() { r.reconcileLabel(n) })
    		}
    		if hostAlive != nil {
    			hostAlive()
    		}
    	}
    	return h
    }
- Line 568 is `r.goWork(func() { r.reconcileLabel(n) })` inside the `h.SubscriptionAlive` wrapper (set :564-571).
- goWork controller/client/reactions.go:46-59 (verbatim):
    // goWork runs fn as background work of the registry, unless it is
    // closed; Close waits for it.
    func (r *Registry) goWork(fn func()) {
    	r.mu.Lock()
    	defer r.mu.Unlock()
    	if r.closed {
    		return
    	}
    	r.work.Add(1)
    	go func() {
    		defer r.work.Done()
    		fn()
    	}()
    }
- goWork returns without running when r.closed (reactions.go:51-53); on a running registry it adds to r.work (reactions.go:54) and runs fn on a goroutine.
- Other goWork call sites: reactions.go:38, :40, :42.

## 4 CI shape

- ci.yml job test: lines 98-116, name `test (linux, race)` :99, `runs-on: ubuntu-latest` :100, step :113-116 env CGO_ENABLED: "1", run: make race.
- ci.yml job coverage: lines 118-138, name `coverage floor` :119, runs-on ubuntu-latest :120, run: make cover-check :138.
- ci.yml job build: lines 33-45, name build :34, runs-on ubuntu-latest :35, run: make build :45.
- Makefile:89-90: `race:` `CGO_ENABLED=1 $(GO) test -race -count=1 ./...`
- Makefile:93-94: `cover:` `$(GO) test -count=1 -cover ./... 2>&1 | tee $(COVER_REPORT)`
- Makefile:97-104: `cover-check:` `$(GO) test -count=1 -cover ./... > $(COVER_REPORT) 2>&1 || { cat $(COVER_REPORT); exit 1; }` then `$(GO) run ./script/coverfloor -report $(COVER_REPORT)`
- Makefile:85-86: `test:` `$(GO) test -count=1 ./...`
- grep for -p / -cpu / -timeout / -shuffle / -parallel / GOMAXPROCS in Makefile, .github/workflows/ci.yml, script/coverfloor/*.go:
    script/coverfloor/floors.go:154:	// Measured 91.7-92.7 across six runs at varying GOMAXPROCS: this
    Makefile:127:			$(GO) test $$dir -fuzz=^$${fn}$$ -fuzztime=$(FUZZTIME) -timeout=$(FUZZ_TIMEOUT) -run=^$$ || exit 1; \
    Makefile:289:	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIPTOOL_TIMEOUT) -v \
    Makefile:294:	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIP_FAMILIES_TIMEOUT) -v \
    Makefile:299:	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIP_FAMILIES_TIMEOUT) -v \
    Makefile:313:	$(GOLANGCI_LINT) run --timeout=5m ./...
    Makefile:316:	$(GOLANGCI_LINT) run --timeout=5m --build-tags chiptool ./internal/chiptool/...
    Makefile:376:	cd $(BLUEZ_DIR) && $(GOLANGCI_LINT) run --timeout=5m ./...
  (the Makefile -timeout hits are fuzz :127, chiptool :289/:294/:299 and golangci-lint --timeout :313/:316/:376; none on the race, cover, cover-check or test targets; ci.yml has none; script/coverfloor/floors.go:154 mentions GOMAXPROCS only in a comment about measured coverage.)
- Other ci.yml env: lines 1-32:
    
    env:
      CGO_ENABLED: "0"
      GO_VERSION: "1.27.2"
      # The lint tools are pinned on purpose, and pinned to the same versions
      # the reference daemon uses. A new lint release adds checks that are style
      # decisions, and floating on @latest makes this gate report on the calendar
      # rather than on the diff — main goes from green to red overnight on an
      # unchanged tree.
      #
      # Bump deliberately: raise the version here, run the linter locally, and fix
      # or justify whatever the new release finds in the same change. The gofumpt
      # step below runs the standalone formatter, which is stricter than the one
      # golangci-lint bundles, so check `gofumpt -l .` against GOFUMPT_VERSION too.
      GOFUMPT_VERSION: "v0.12.0"
      GOLANGCI_LINT_VERSION: "v2.14.0"
    
    jobs:
- Parallelism in tests: siteSlots/holdSite serialisers: node/parity_matterjs_helpers_test.go:41-60, client/interaction/live_test.go:74-84, controller/client/parity_matterjs_helpers_test.go:44-52. node comment at parity_matterjs_helpers_test.go:41-46: "A fake site clock moves only while the whole test process is quiet ... so busy sites in parallel starve each other's clocks — under the race detector past parityCtx's budget".

## 5 Existing notes on flakes (grep -i 'flake|flaky|intermittent' under notes/ and docs/)
- notes/handover.md:375:read as a timing flake, to be confirmed by the owner's fixture run. No
- notes/handover.md:725:three by_design entries, open items (the `secure/operational` flake, the
- notes/handover.md:1330:  `CHIP_ROOT`); `make race` flaked once in the agent's run on
- notes/handover.md:1514:CI: one `TestSessionAndSubscriptionDiagnostics` flake, rerun.
- notes/handover.md:1598:  after two CI flakes.
- notes/handover.md:1628:- **Flakes seen:** `TestSessionAndSubscriptionDiagnostics` (known),
- notes/parity/matter_behaviour_findings.md:878:  worktree; the three flakes; `in` constraints on initial values and host
- notes/briefs/2026-10-16-AX-findings-codec-im-controller-tooling.md:74:15. **Flakes (452, 453):** `TestReactionLeave` waits on `NodeIDs`;
- notes/briefs/2026-10-18-BG-m2-phase-15b-fabrictest.md:72:   the wall clock would make a stepped test flaky). Field names: `Clock`
- notes/briefs/2026-10-20-BX2-ownership-locks.md:1:# Brief BX2 — gate-1 fixes: goroutine ownership, locks, the flakes
- notes/briefs/2026-10-20-BX2-ownership-locks.md:49:## 2 The restart flake (G1-15; P5, P10)
- notes/briefs/2026-10-20-BX2-ownership-locks.md:111:test), **The restart flake** (the cause, file:line, the fix),
- notes/briefs/2026-10-14-AK-controller-client.md:559:server list (NPP:31); `IntermittentlyConnected` = IcdManagement 0x0046 in
- notes/briefs/2026-10-20-BX1-kit-defaults.md:12:mounts). Parallel to BX2 (goroutines, locks, flakes: `secure/operational`,
- notes/briefs/2026-10-20-G1-gate1-audit.md:41:     from one `go test -json ./...` run (top 15); flaky tests known
- notes/briefs/2026-10-20-G1-gate1-audit.md:72:   conversion vs generated per-field code (0026 P10); flaky tests and
- notes/briefs/2026-10-20-G1-gate1-audit.md:88:- **Restart flake "[fe80::1]:5540 is in use"** (CI race job of #198, run
- notes/briefs/2026-10-20-G1-gate1-audit.md:105:  ADR 0026 principle 5 (goroutine/resource ownership) and 10 (flaky
- notes/briefs/reads/read-gate2-inventory.md:5:Sections: 1 import graph (incl. client/* and controller/*), 2 goroutines and timers, 3 locks, 4 time and randomness, 5 errors, 6 logging, 7 persistence outside the seam, 8 facade surface (node, device, contract, fabrictest, controller/client, controller/commissioner, controller/ca, controller/dcl, controller/attestation), 9 tests and flakes, 10 the declaration, 11 the refe
- notes/briefs/reads/read-gate2-inventory.md:534:### 9c the named flaky tests (file:line; what each waits on)
- notes/briefs/reads/read-gate2-inventory.md:762:- 9: 7 packages without any test file; 15 slowest listed; 1 failing test in a full run (client/interaction); 9 flaky-test entries
- notes/briefs/2026-10-22-BY2-gate2-fixes.md:125:## 7 Guards as code (ADR 0026 P11) and the flakes (P10; G2-15, G2-16)
- notes/briefs/2026-10-22-BY2-gate2-fixes.md:138:- The flake sweep: `controller/commissioner` `errWon` surfacing
- notes/briefs/2026-10-22-BY2-gate2-fixes.md:156:per § what moved, **Flakes** (per test: cause, fix, 20/20 before/after),
- notes/briefs/reads/open-findings.md:48:| 452 | Tier 3 | Flake under -race: controller/client TestReactionLeave reads store without waiting | test-flake | wait on NodeIDs in the test |
- notes/briefs/reads/open-findings.md:49:| 453 | Tier 3 | Timing flake: TestStartReaper_ActiveSessionSurvivesSweeps window too tight under load | test-flake | fake clock in the test |
- notes/briefs/reads/open-findings.md:73:- test-flake: 2
- notes/briefs/reports/AI.md:24:| 9b97154 `test(controller/interaction): prove the InvalidSubscription answer from the registry's log` | §4, a flaky proof fixed |
- notes/briefs/reports/AI.md:25:| 4c8bbac `test(controller/exchange): wait for the inbound session's release` | §0, a flaky check fixed |
- notes/briefs/reports/AI.md:407:`BD-Matter-Controller-InvokeRevision12`. The two flaky-test fixes
- notes/briefs/reports/BT.md:127:| `make cover-check` | 8d14e734 | 2 | the report carried the flaky FAIL of `controller/interaction`; with that package's isolated rerun (92.7 %) spliced in, `script/coverfloor`: 228 packages at or above their floor, no floor moved |
- notes/briefs/reports/AX.md:239:### 15. Flakes (452, 453)
- notes/briefs/reports/AX.md:255:| `make race` | ceb020c | exit 2: `bridge` `TestReportSubscriptionChunksAnOversizedOngoingReport` (`subscribe_report_chunking_test.go:281`, 14.6 s under load); rerun alone: ok (1.5 s) — a timing flake, not in this brief's paths |
- notes/briefs/reports/AJ.md:38:  passed, so I can't name the test. Treat it as a possible flake to watch.
- notes/briefs/reports/W.md:179:    timeout (116 s alone); I read it as a timing flake under the long run,
- notes/briefs/reports/BI.md:214:went from 170-190 s and flaky to 97 s.
- notes/briefs/reports/BI.md:232:| `make race` | 409ada1 | exit 0 (at f694e01 exit 2: the node package's flake, see fabrictest additions) |
- notes/briefs/reports/BF.md:203:(wall-clock, load-sensitive, its own comment records an earlier flake); 5/5
- notes/briefs/reads/read-gate1-inventory.md:521:### 9c the named flaky tests (file:line; what each waits on)
- notes/briefs/reads/read-gate1-inventory.md:539:- §9: 6 packages without any test file (43 by the briefed template), 15 slowest listed, 7 flaky entries
- notes/briefs/reports/S.md:194:   This was seen as a `-race` flake. Both restart tests now wait on
- notes/briefs/reports/AP.md:107:  the brief's known load flake; passes alone and in a package run); the
- notes/briefs/reports/AU.md:179:0. `TestReactionLeave` (controller/client) looks timing-flaky under
- notes/briefs/reports/AF.md:191:7. `secure/operational TestStartReaper_ActiveSessionSurvivesSweeps` flaked once
- docs/roadmap.md:216:**Status 2026-10-22.** Phase 20 complete, released as `v0.10.0`: phase 20b (brief BT, pull request 198) — the typed cluster layer, a typed server over every cluster server (`State`, `Patch`, `Set`, `Get`, `OnChanged`, `OnWrite`, `Handle<Command>`, `Emit<Event>`), every device accessor returning it; the first architecture gate (report G1, pull requests 200/201, ADR 0026 amended) with its fix waves 
- notes/briefs/2026-10-22-G2-gate2-audit.md:19:   an alias, matter.js-internal vocabulary; §9 tests and flakes) plus:
- notes/briefs/2026-10-22-G2-gate2-audit.md:43:   - the two CI flakes: `controller/commissioner` errWon surfacing
- notes/briefs/reports/G2.md:151:controller ran on `fabrictest` in 5 s. The flakes are under ADR 0026 P10.
- notes/briefs/reports/G2.md:207:**P10 Tests are part of the architecture.** The flakes (§9b/c):
- notes/briefs/reports/BX3.md:212:## Flakes seen during the guard runs (not from this branch)
- docs/history/concept-matter-1.6.1-and-device-layer.md:560:| ICDManagement | Attribute 0..2, nicht montiert | Eine netzbetriebene Bridge ist kein Intermittently Connected Device; ein Controller hielte den Node für schläfrig. | Nur für ein Batteriegerät auf go-fabric, das ohne Thread und BLE nicht entsteht. ICDM-Familie (7 Fälle). |
- notes/briefs/reports/BY-2.md:133:## Flakes
- notes/briefs/reports/BY-2.md:238:- The commissioner `errWon` flake: not reproduced in 20 runs. The read
- notes/briefs/reports/BY-2.md:244:  `make ci` it hit that timeout while the flake runs loaded the machine;
- notes/briefs/reports/BY-2.md:247:- The two `make race` runs used only as background load for the flake
- notes/briefs/reports/AT.md:394:  flakes on 2026-10-14).- **Owner's `make race` at bab36cf** failed in `TestReferenceControllerOTA`:
- notes/briefs/reports/BX2.md:1:# Report BX2 — gate-1 fixes: goroutine ownership, locks, the flakes
- notes/briefs/reports/BX2.md:18:| `transport/udp/listener.go` `Serve` cancel watcher | `Serve` | returns after the watcher | `TestCloseWaitsForTheClosingCall` (the restart flake) |
- notes/briefs/reports/BX2.md:36:## The restart flake (G1-15)
- notes/briefs/reports/BX2.md:203:1. **The restart flake had no second socket.** The brief's two
- notes/briefs/reports/BA.md:194:- Flake seen once under the parallel race run, passing on rerun ×3:
- notes/briefs/reports/CB.md:235:  its test under race (see Guards): a follow-up for the flake sweep?
- notes/briefs/reports/G1.md:197:`bdx_responder.go:108`. **G1-15 The restart flake** "[fe80::1]:5540 is in
- notes/briefs/reports/G1.md:204:WaitGroup or done channel per owner above, `Stop` waits; the flake's
- notes/briefs/reports/G1.md:243:flaky tests have their wait named (inventory §9c): the reaper test polls
- docs/matterjs-comparison.md:111:| ICDManagement | full, incl. check-in sender (`protocol/src/icd`) | attributes 0x0000–0x0002, not mounted | ◐ | **Low** | `BD-chip-ICD-Attrs-0x3-0x5`. A mains-powered bridge is not an intermittently connected device, and the server is mounted nowhere: RootNode requires it under "Sit \| Lit", matter.js mounts it on no node that is not an ICD, and a controller takes its presence for a s

## 6 Session end on restart

- The string "exchange: session ended by peer": client/exchange/errors.go:121 `func (*PeerInitiatedCloseError) Error() string { return "exchange: session ended by peer" }`; type at :115-119; `Is` makes it ErrTransient :124. Only constructor outside tests: client/exchange/manager.go:993 `x.session.handlePeerLoss(&PeerInitiatedCloseError{}, x)` in handleSecureChannel (manager.go:982-1000), on an initial StatusReport Success/CloseSession (manager.go:984-994, closed_by_peer log :992). Also referenced (errors.As) controller/client/fabricops.go:369-376.
- handlePeerLoss: client/exchange/session.go:274-292 (HandlePeerLoss :280; sets peerLost :292; IsPeerLost :256-261); Session closing: session.go:265.
- Producing side on the device: bridge/bridge.go:89-96 (doc: "Inbound CloseSession ... [Stop] tears sessions down without the CloseSession" when no session registry attached); bridge/bridge.go:651-656 ("[Bridge.Stop] uses it to drain every session with a graceful CloseSession farewell before the ..."); bridge/bridge.go:1328-1345 (CloseSession on fabric removal); bridge/conn.go:523-525, :558 (peer-lost close, no CloseSession echo).
- Peer-side reaction to a lost session: client/peer/peer.go:350-360 NewestSession skips `s.IsClosed() || s.IsPeerLost()`; peer.go:347-348 HasSession; peer.go:398-405 initiateForceClose -> Exchanges.HandlePeerLoss(p.address, cause, time.Time{}); peer.go:410-430 Close -> mgr.CloseSession; peer.go:434-442 Delete (resumption record); client/peer/monitor.go:24-31 ReasonSessionSuspect, :173-180 check on NewestSession, :240 `s.HandlePeerLoss(&exchange.PeerUnresponsiveError{})`; client/peer/connection.go:197 NewestSession, :726-766 kick restart gate (lastRestartAt :129), :775-860 NoSharedTrustRoots/resumption rejected -> deleteResumptionRecord, :839-843 ErrTransient classification.
- Interaction re-dial: client/interaction/provider.go:138-155 peerProvider.InitiateExchange: `s := p.p.NewestSession()` nil -> (RequireExistingSession ? ErrNoSession : continue / connect again 'The session closed between connect and here: connect again (:117-125)'), and :152 `errors.Is(err, exchange.ErrSessionClosed) && !opts.RequireExistingSession -> continue`. Lines 138-155 verbatim above in the grep section.
- fabrictest/controller.go (non-test): Restart/restart words only about Controller.Restart (:249-270) and stores kept across it (:82-95, resume `*peer.MemoryResumptionStore` :82); no reconnect/redial code for a Device.Restart. Device.Restart device.go:300-309 = Stop then Start; fabrictest.Pair holds Peer and Interaction (controller.go:430) and does not replace them on Device.Restart.
- Test counterpart: fabrictest/host_statement_test.go:89-92; client/interaction/live_test.go:352-361; controller/client/live_test.go:263-269.
