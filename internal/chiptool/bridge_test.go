// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
}

// bridgeProcess is a running reference daemon plus its captured output.
// stdout carries the pairing banner; stderr carries the slog stream, which
// is the daemon-side view of a handshake and the ground truth for whether a
// Matter command reached the device behind the cluster server.
type bridgeProcess struct {
	cmd  *exec.Cmd
	info pairingInfo

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

	dir := t.TempDir()
	args := []string{
		"--db", filepath.Join(dir, "reference-bridge.db"),
		"--listen", ":0",
	}
	if flags["log-level"] {
		// The daemon-side view of a failed handshake is the only thing that
		// says which stage stopped; chip-tool's output alone cannot.
		args = append(args, "--log-level", "debug")
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

	b := &bridgeProcess{cmd: cmd}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start reference daemon %s: %v", bin, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); b.drain(stdout, &b.stdout) }()
	go func() { defer wg.Done(); b.drain(stderr, &b.stderr) }()

	t.Cleanup(func() {
		b.stop()
		wg.Wait()
	})

	info, ok := b.awaitBanner(bannerTimeout)
	if !ok {
		t.Fatalf("reference daemon printed no pairing banner within %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			bannerTimeout, b.snapshotStdout(), b.snapshotStderr())
	}
	b.info = info
	t.Logf("reference daemon up: listen=%s port=%d discriminator=%d passcode=%08d",
		info.listenAddr, info.port, info.discriminator, info.passcode)
	return b
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
	return pairingInfo{
		listenAddr:    listen[1],
		port:          port,
		passcode:      uint32(passcode),
		discriminator: uint16(discriminator),
	}, true
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
