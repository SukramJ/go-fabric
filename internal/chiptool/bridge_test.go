// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// bridgeBinEnv pins the reference-daemon binary the suite spawns. Without
// it the suite looks for ./bin/reference-bridge under the module root,
// which is what `make chiptool-build` produces.
const bridgeBinEnv = "GOFABRIC_CHIPTOOL_BRIDGE"

// bannerTimeout bounds the wait for the daemon's pairing banner. Startup is
// a SQLite migration, a fleet assembly and a UDP bind; on a loaded arm64 CI
// runner that is seconds, not tens of seconds, but the ceiling is generous
// so a slow runner reports the banner it did print rather than a timeout
// that says nothing.
const bannerTimeout = 60 * time.Second

// The reference daemon prints its pairing information as a fixed block on
// stdout — deliberately stdout and not the log, because it is the one thing
// an operator has to copy (examples/reference-bridge/main.go
// printPairingInfo). The suite reads the passcode, the discriminator and
// the effective listen address out of that block instead of assuming the
// flag defaults: the daemon is the authority on what it is actually
// advertising, and reading it back is also the first assertion that the
// process came up at all.
var (
	reListenLine        = regexp.MustCompile(`listening on\s+(\S+)`)
	rePasscodeLine      = regexp.MustCompile(`setup passcode\s+(\d+)`)
	reDiscriminatorLine = regexp.MustCompile(`(?m)^\s*discriminator\s+(\d+)\s*$`)
	reManualCodeLine    = regexp.MustCompile(`manual code\s+(\d+)`)
	reQRCodeLine        = regexp.MustCompile(`QR payload\s+(MT:\S+)`)

	// Go's flag package renders each flag as a line starting with two
	// spaces and a single dash: "  -db string". Matching that shape is how
	// the suite discovers the daemon's command line rather than hard-coding
	// it — see requireBridgeFlags.
	reHelpFlag = regexp.MustCompile(`(?m)^\s+-([A-Za-z0-9][A-Za-z0-9._-]*)`)
)

// pairingInfo is the daemon's own account of how it can be commissioned.
type pairingInfo struct {
	listenAddr    string
	port          int
	passcode      uint32
	discriminator uint16
	// manualCode and qrCode are the onboarding payloads the banner prints
	// ("" when an older daemon does not print them).
	manualCode string
	qrCode     string
}

// bridgeProcess is a running reference daemon plus its captured output.
// stdout carries the pairing banner; stderr carries the slog stream, which
// is the daemon-side view of a handshake and the ground truth for whether a
// Matter command reached the device behind the cluster server.
type bridgeProcess struct {
	cmd   *exec.Cmd
	info  pairingInfo
	bin   string
	flags map[string]bool
	opts  bridgeOptions
	wg    sync.WaitGroup

	controlMu sync.Mutex

	// previousStderr is the log of the runs before a restart.
	previousStderr string

	mu     sync.Mutex
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// requireBridgeBinary resolves the reference-daemon binary. It does not
// build: a missing binary is a loud failure naming the command to run, so a
// developer never sees a bare exec error.
func requireBridgeBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(bridgeBinEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s=%q not usable: %v", bridgeBinEnv, p, err)
		}
		return p
	}
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	bin := filepath.Join(root, "bin", "reference-bridge")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("reference daemon not found at %s: %v\n"+
			"run `make chiptool-test` (which builds it first), or set %s to a pre-built binary",
			bin, err, bridgeBinEnv)
	}
	return bin
}

// requireBridgeFlags asks the daemon what its command line is, rather than
// asserting one. The reference daemon is a moving target — it is the
// module's second consumer and changes whenever the public API does — so a
// hard-coded argv here would fail as a mysterious startup error long after
// the change that caused it. `--help` is the daemon's own answer.
//
// Go's flag package writes its usage to stderr and exits non-zero for
// `--help`, so the exit status is deliberately ignored and both streams are
// scanned. A flag the suite needs but the daemon does not offer is reported
// by name and fails the test; it is not worked around, and it is certainly
// not added to the daemon from here.
func requireBridgeFlags(t *testing.T, bin string, needed ...string) map[string]bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--help")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()

	usage := buf.String()
	flags := make(map[string]bool)
	for _, m := range reHelpFlag.FindAllStringSubmatch(usage, -1) {
		flags[m[1]] = true
	}
	if len(flags) == 0 {
		t.Fatalf("%s --help printed no recognisable flag list:\n%s", filepath.Base(bin), usage)
	}
	for _, want := range needed {
		if !flags[want] {
			t.Fatalf("the reference daemon has no --%s flag; the suite needs it to run "+
				"hermetically (its own database, its own port). Reported flags: %v\n--- usage ---\n%s",
				want, sortedKeys(flags), usage)
		}
	}
	return flags
}

// sortedKeys renders a flag set deterministically for a failure message.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Insertion sort: the set is a dozen entries and this avoids pulling
	// "sort" in for a diagnostic string.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// bridgeOptions are the knobs a test turns on the reference daemon.
type bridgeOptions struct {
	// dbPath is the daemon's database. Empty creates a fresh one in a
	// t.TempDir(); a restart passes the previous run's path, which is what
	// makes it the same device (fabrics, endpoint numbers, event counter).
	dbPath string
	// listen is the --listen value. Empty is ":0" (an ephemeral port); a
	// restart passes the port the previous run bound, so a controller that
	// cached the address finds the device where it left it.
	listen string
	// appPipe is the daemon's --app-pipe FIFO; empty leaves it off.
	appPipe string
	// enableKey arms TestEventTrigger (--enable-key, hex); empty leaves it
	// off.
	enableKey string
}

// chipTestEnableKey is the test enable key CHIP's apps and certification
// cases use by default (PIXIT.DGGEN.TEST_EVENT_TRIGGER_KEY,
// 000102030405060708090a0b0c0d0e0f).
const chipTestEnableKey = "000102030405060708090a0b0c0d0e0f"

// startBridge spawns the reference daemon on an ephemeral UDP port with a
// throwaway database, waits for its pairing banner, and registers the
// shutdown.
//
// --listen :0 rather than the default :5540 so two runs (or a runner with
// something already on 5540) cannot collide; the daemon reports the port it
// actually bound in the banner, which is why an ephemeral port is usable at
// all. mDNS advertising is left at its default (on): chip-tool commissions
// over `pairing already-discovered`, which needs no discovery, but the
// operational stage after AddNOC resolves the daemon's `_matter._tcp`
// record, and with no record to resolve the commissioning fails at the very
// end with "Incorrect state" — the same failure loom's suite documents in
// .github/workflows/chiptool.yml.
func startBridge(t *testing.T, bin string, flags map[string]bool) *bridgeProcess {
	t.Helper()
	return startBridgeWith(t, bin, flags, bridgeOptions{})
}

// defaultListen is the --listen of a daemon whose options name none: an
// ephemeral port, or the Matter port for the certification families
// (TestChipCertificationFamilies sets it).
//
// The family run starts one daemon per case on the same commissioned
// database, so every one publishes the same operational instance. A resolver
// on the same host keeps a stopped daemon's record — its goodbye reaches
// avahi on another host (checked with two containers) but not the avahi
// beside it — and a controller that picked that record sent Sigma1 to a port
// nobody listened on any more (TC-CC-4.1 and TC-CC-7.3 in CI). On one stable
// port a stale record still names a live daemon, which makes the race
// impossible. The Matter port is the one to take: the cases address group
// messages to it (CHIP_PORT), and the daemon receives those on its
// operational socket, as chip and matter.js do.
var defaultListen = ":0"

// startBridgeWith is startBridge with options.
func startBridgeWith(t *testing.T, bin string, flags map[string]bool, opts bridgeOptions) *bridgeProcess {
	t.Helper()

	if opts.dbPath == "" {
		opts.dbPath = filepath.Join(t.TempDir(), "reference-bridge.db")
	}
	if opts.listen == "" {
		opts.listen = defaultListen
	}
	args := []string{"--db", opts.dbPath, "--listen", opts.listen}
	if flags["log-level"] {
		// The daemon-side view of a failed handshake is the only thing that
		// says which stage stopped; chip-tool's output alone cannot.
		args = append(args, "--log-level", "debug")
	}
	if opts.appPipe != "" {
		args = append(args, "--app-pipe", opts.appPipe)
	}
	if opts.enableKey != "" {
		args = append(args, "--enable-key", opts.enableKey)
	}
	// On a host whose LAN interfaces have no IPv6, the MAC-derived SRV
	// target carries only IPv4 address records, and the image's chip-tool
	// resolves operational nodes over IPv6 only. The OS host name's
	// records, published by the host's avahi for every interface, include
	// the docker bridges' link-local addresses chip-tool reaches the
	// daemon over. The cases that check the host name itself (TC-SC-4.3)
	// skip on such a host.
	if flags["mdns-os-hostname"] {
		if has, _ := lanIPv6(); !has {
			args = append(args, "--mdns-os-hostname")
		}
	}

	cmd := exec.Command(bin, args...) //nolint:gosec // bin is this module's own build output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	b := &bridgeProcess{cmd: cmd, bin: bin, flags: flags, opts: opts}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start reference daemon %s: %v", bin, err)
	}

	b.wg.Add(2)
	go func() { defer b.wg.Done(); b.drain(stdout, &b.stdout) }()
	go func() { defer b.wg.Done(); b.drain(stderr, &b.stderr) }()

	t.Cleanup(func() {
		b.stop()
		b.wg.Wait()
	})

	info, ok := b.awaitBanner(bannerTimeout)
	if !ok {
		t.Fatalf("reference daemon printed no pairing banner within %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			bannerTimeout, b.snapshotStdout(), b.snapshotStderr())
	}
	b.info = info
	t.Logf("reference daemon up: listen=%s port=%d discriminator=%d passcode=%08d",
		info.listenAddr, info.port, info.discriminator, info.passcode)
	b.awaitOperationalRecords(t)
	return b
}

// reFabricPublished is the daemon's log line for an operational record it
// published: one per installed fabric, at start and after AddNOC.
var reFabricPublished = regexp.MustCompile(`msg=matter\.mdns\.fabric_published .*?instance=([0-9A-F]{16}-[0-9A-F]{16})`)

// operationalRecordWait bounds how long a started daemon's operational
// records may take to reach the resolver.
const operationalRecordWait = 30 * time.Second

// awaitOperationalRecords holds a daemon that starts on a database with
// fabrics until the resolver the cases use — avahi, through the harness
// container or the host — hands out its operational instances at the port
// it listens on now, and over IPv6 at no other.
//
// Each restarted daemon publishes the same operational instances. On the
// stable port of a family run ([defaultListen]) that is all there is to
// wait for: the announcement reaching the resolver. A daemon on a new port
// beside a stale record of an old one would fail here, naming both ports,
// instead of failing a case at random with a Sigma1 to the old port.
func (b *bridgeProcess) awaitOperationalRecords(t *testing.T) {
	t.Helper()
	browse := avahiBrowser()
	if browse == nil {
		return
	}
	// The daemon logs its operational records right after the banner; a
	// daemon without fabrics logs none.
	var instances []string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		instances = instances[:0]
		for _, m := range reFabricPublished.FindAllStringSubmatch(b.snapshotStderr(), -1) {
			if !slices.Contains(instances, m[1]) {
				instances = append(instances, m[1])
			}
		}
		if len(instances) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(instances) == 0 {
		return
	}
	want := b.info.port
	var stale map[string]string
	deadline = time.Now().Add(operationalRecordWait)
	for time.Now().Before(deadline) {
		ports := resolvedPorts(browse())
		stale = map[string]string{}
		for _, inst := range instances {
			if why := staleRecord(ports[inst], want); why != "" {
				stale[inst] = why
			}
		}
		if len(stale) == 0 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("after %s the resolver still does not hand out port %d, the one the daemon listens on, for its operational "+
		"instances: %v", operationalRecordWait, want, stale)
}

// staleRecord says what is wrong with an instance's resolved records for a
// daemon on port want, or "" when a controller resolving it now reaches the
// daemon. Controllers prefer the IPv6 records (chip-tool resolves
// operational nodes over IPv6 only, and chip scores an IPv6 address above
// an IPv4 one), so every IPv6 record must name want. Over IPv4 the record
// for want must be present; a same-host avahi keeps an IPv4 record a stopped
// daemon withdrew (its goodbye reaches a resolver on another host — checked
// with two containers — but not avahi on the host the daemon runs on), so a
// stale IPv4 port alongside it cannot be waited away.
func staleRecord(byProto map[string][]int, want int) string {
	v6, v4 := byProto["IPv6"], byProto["IPv4"]
	switch {
	case len(v6) > 0 && slices.ContainsFunc(v6, func(p int) bool { return p != want }):
		return fmt.Sprintf("IPv6 resolves to %v (stale port)", v6)
	case len(v6) > 0:
		return ""
	case slices.Contains(v4, want):
		return ""
	case len(v4) > 0:
		return fmt.Sprintf("IPv4 resolves to %v only (stale port)", v4)
	}
	return "not resolved yet"
}

// avahiBrowser returns a function that lists the resolved _matter._tcp
// services as `avahi-browse -r -p -t` prints them — in the harness
// container when there is one, else on the host — or nil without avahi.
func avahiBrowser() func() string {
	args := []string{"avahi-browse", "-r", "-p", "-t", "_matter._tcp"}
	run := func(argv ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, argv[0], argv[1:]...).Output() //nolint:gosec // fixed argv
		return string(out)
	}
	if harnessVal != nil {
		h := harnessVal
		return func() string {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, _ := h.exec(ctx, args...)
			return out
		}
	}
	if _, err := exec.LookPath("avahi-browse"); err == nil {
		return func() string { return run(args...) }
	}
	return nil
}

// resolvedPorts maps each resolved instance in avahi-browse's parsable
// output ("=;iface;proto;instance;type;domain;host;address;port;txt") to the
// ports it resolved to, per protocol ("IPv4", "IPv6").
func resolvedPorts(out string) map[string]map[string][]int {
	ports := map[string]map[string][]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ";")
		if len(f) < 9 || f[0] != "=" {
			continue
		}
		p, err := strconv.Atoi(f[8])
		if err != nil {
			continue
		}
		if ports[f[3]] == nil {
			ports[f[3]] = map[string][]int{}
		}
		if !slices.Contains(ports[f[3]][f[2]], p) {
			ports[f[3]][f[2]] = append(ports[f[3]][f[2]], p)
		}
	}
	return ports
}

// restart stops the daemon the way an operator would and starts it again on
// the same database and the same port: the same device, rebooted. The old
// process's output stays readable through the returned value's
// previousStderr.
func (b *bridgeProcess) restart(t *testing.T) *bridgeProcess {
	t.Helper()
	b.stop()
	b.wg.Wait()
	opts := b.opts
	opts.listen = fmt.Sprintf(":%d", b.info.port)
	next := startBridgeWith(t, b.bin, b.flags, opts)
	next.previousStderr = b.previousStderr + b.snapshotStderr()
	return next
}

// factoryReset stops the daemon and starts it on a fresh database at the
// same port — the device as it leaves the factory, at the address a
// controller last saw it.
func (b *bridgeProcess) factoryReset(t *testing.T) *bridgeProcess {
	t.Helper()
	b.stop()
	b.wg.Wait()
	opts := b.opts
	opts.listen = fmt.Sprintf(":%d", b.info.port)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(opts.dbPath + suffix)
	}
	next := startBridgeWith(t, b.bin, b.flags, opts)
	next.previousStderr = b.previousStderr + b.snapshotStderr()
	return next
}

// endpointFor returns the first endpoint advertising deviceType, read from
// the topology block the daemon prints after its banner; 0 when deviceType
// is 0 or no endpoint carries it.
func (b *bridgeProcess) endpointFor(t *testing.T, deviceType uint32) uint16 {
	t.Helper()
	if deviceType == 0 {
		return 0
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out := b.snapshotStdout()
		if strings.Contains(out, "topology end") {
			for _, m := range reTopologyLine.FindAllStringSubmatch(out, -1) {
				for _, dt := range strings.Split(m[2], ",") {
					v, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(dt), "0x"), 16, 32)
					if err == nil && v == uint64(deviceType) {
						ep, _ := strconv.Atoi(m[1])
						return uint16(ep)
					}
				}
			}
			t.Fatalf("no endpoint of the reference daemon advertises device type 0x%04X\n%s", deviceType, out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the reference daemon printed no topology block")
	return 0
}

// reTopologyLine matches one line of the daemon's topology block
// (examples/reference-bridge/main.go printTopology).
var reTopologyLine = regexp.MustCompile(`(?m)^\s*endpoint\s+(\d+)\s+device types\s+([0-9a-fA-FxX, ]+)`)

// pipe writes one CHIP-style JSON command to the daemon's --app-pipe and
// waits for the daemon to log it as applied, failing the test on an error.
// A FIFO has no answer channel; the daemon's log is the acknowledgement
// (examples/reference-bridge/control.go).
func (b *bridgeProcess) pipe(t *testing.T, command map[string]any) {
	t.Helper()
	if b.opts.appPipe == "" {
		t.Fatalf("app-pipe command %v: the daemon was started without --app-pipe", command)
	}
	line, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	name, _ := command["Name"].(string)
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	offset := len(b.snapshotStderr())
	w, err := os.OpenFile(b.opts.appPipe, os.O_WRONLY, 0) //nolint:gosec // the daemon's own FIFO
	if err != nil {
		t.Fatalf("open the daemon's app pipe: %v", err)
	}
	_, err = w.Write(append(line, '\n'))
	_ = w.Close()
	if err != nil {
		t.Fatalf("write to the daemon's app pipe: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		logs := b.snapshotStderr()
		if offset <= len(logs) {
			tail := logs[offset:]
			if strings.Contains(tail, "msg=apppipe.applied name="+name) {
				return
			}
			if i := strings.Index(tail, "msg=apppipe.error"); i >= 0 {
				t.Fatalf("app-pipe command %s refused: %s", line, strings.SplitN(tail[i:], "\n", 2)[0])
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("app-pipe command %s: the daemon logged no apppipe.applied within 10s", line)
}

// drain copies one of the daemon's streams into a buffer line by line, so a
// failure message can quote everything the daemon said up to that point
// instead of whatever happened to be flushed.
func (b *bridgeProcess) drain(r io.Reader, into *bytes.Buffer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		b.mu.Lock()
		into.WriteString(sc.Text())
		into.WriteByte('\n')
		b.mu.Unlock()
	}
}

// awaitBanner polls the captured stdout until all three banner fields are
// present. Polling rather than blocking on a single scan keeps the timeout
// meaningful: a daemon that prints half a banner and hangs is reported with
// the half it printed.
func (b *bridgeProcess) awaitBanner(timeout time.Duration) (pairingInfo, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, ok := parseBanner(b.snapshotStdout()); ok {
			return info, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return pairingInfo{}, false
}

// parseBanner extracts the pairing information from the daemon's stdout.
func parseBanner(out string) (pairingInfo, bool) {
	listen := reListenLine.FindStringSubmatch(out)
	pass := rePasscodeLine.FindStringSubmatch(out)
	disc := reDiscriminatorLine.FindStringSubmatch(out)
	if listen == nil || pass == nil || disc == nil {
		return pairingInfo{}, false
	}

	_, portStr, err := net.SplitHostPort(listen[1])
	if err != nil {
		return pairingInfo{}, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		return pairingInfo{}, false
	}
	passcode, err := strconv.ParseUint(pass[1], 10, 32)
	if err != nil {
		return pairingInfo{}, false
	}
	discriminator, err := strconv.ParseUint(disc[1], 10, 16)
	if err != nil {
		return pairingInfo{}, false
	}
	info := pairingInfo{
		listenAddr:    listen[1],
		port:          port,
		passcode:      uint32(passcode),
		discriminator: uint16(discriminator),
	}
	if m := reManualCodeLine.FindStringSubmatch(out); m != nil {
		info.manualCode = m[1]
	}
	if m := reQRCodeLine.FindStringSubmatch(out); m != nil {
		info.qrCode = m[1]
	}
	return info, true
}

// stop asks the daemon to shut down the way an operator would — the
// composition root installs a signal.NotifyContext on os.Interrupt and
// SIGTERM and runs its deferred teardown on it — and only kills the process
// if that does not land. A killed daemon skips the teardown path, which is
// exactly the path a "tear down cleanly" assertion is about.
func (b *bridgeProcess) stop() {
	if b.cmd == nil || b.cmd.Process == nil {
		return
	}
	_ = b.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = b.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = b.cmd.Process.Kill()
		<-done
	}
}

func (b *bridgeProcess) snapshotStdout() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.String()
}

func (b *bridgeProcess) snapshotStderr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stderr.String()
}

// dump writes both daemon streams into the test log. Called from every
// failure path that involves the wire, because chip-tool's own output names
// the stage that failed but never the reason the daemon had for it.
func (b *bridgeProcess) dump(t *testing.T) {
	t.Helper()
	t.Logf("reference daemon stdout:\n%s", b.snapshotStdout())
	t.Logf("reference daemon stderr:\n%s", b.snapshotStderr())
}
