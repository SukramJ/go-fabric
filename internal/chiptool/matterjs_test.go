// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The matter.js controller leg: matter.js's CommissioningController
// (../matter.js, packages/matter.js) as a second, independent controller
// against the reference daemon (ADR 0011, layer 2). The controller runs
// from testdata/matterjs/controller.mjs in a scratch directory whose
// node_modules links ../matter.js/node_modules — nothing is built or written
// inside the matter.js tree, which must be built already (`npm run build`
// there).

// matterJSDir is the matter.js checkout CLAUDE.md names.
func matterJSDir() string {
	if d := os.Getenv("GOFABRIC_MATTERJS_DIR"); d != "" {
		return d
	}
	return filepath.Join(moduleRoot(), "..", "matter.js")
}

// mjsController is one running controller.mjs.
type mjsController struct {
	t     *testing.T
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   *bufio.Scanner
	log   *strings.Builder
	start time.Time
}

func startMatterJSController(t *testing.T, id string) *mjsController {
	t.Helper()
	mjs := matterJSDir()
	if _, err := os.Stat(filepath.Join(mjs, "packages", "matter.js", "dist", "esm", "CommissioningController.js")); err != nil {
		t.Skipf("no built matter.js at %s (%v): run `npm install && npm run build` in that checkout, "+
			"or point GOFABRIC_MATTERJS_DIR at one", mjs, err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node is not on PATH: install Node.js 20 or later for the matter.js controller leg")
	}
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(mjs, "node_modules"), filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("testdata", "matterjs", "controller.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "controller.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "controller.mjs") //nolint:gosec // our own script
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MATTERJS_STORAGE="+filepath.Join(dir, "store"), "MATTERJS_ID="+id)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	log := &strings.Builder{}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	c := &mjsController{t: t, cmd: cmd, in: in, out: sc, log: log, start: time.Now()}
	t.Cleanup(func() {
		_, _ = io.WriteString(in, `{"cmd":"exit"}`+"\n")
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	if r := c.read(); r["ready"] != true {
		t.Fatalf("matter.js controller did not start: %v\n%s", r, log)
	}
	t.Logf("matter.js controller %s from %s", id, mjs)
	return c
}

func (c *mjsController) read() map[string]any {
	c.t.Helper()
	lines := make(chan string, 1)
	go func() {
		// matter.js's logger writes to stdout too; the replies are the
		// lines that start with {"ok".
		for c.out.Scan() {
			if line := c.out.Text(); strings.HasPrefix(line, `{"ok"`) {
				lines <- line
				return
			} else {
				c.log.WriteString(line + "\n")
			}
		}
		close(lines)
	}()
	select {
	case line, ok := <-lines:
		if !ok {
			c.t.Fatalf("matter.js controller exited\n%s", tail(c.log.String(), 40))
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			c.t.Fatalf("matter.js controller said %q: %v", line, err)
		}
		return m
	case <-time.After(3 * time.Minute):
		c.t.Fatalf("matter.js controller did not answer within 3 minutes\n%s", tail(c.log.String(), 40))
	}
	return nil
}

// try sends one command and returns its reply, failed or not.
func (c *mjsController) try(cmd map[string]any) map[string]any {
	c.t.Helper()
	data, _ := json.Marshal(cmd)
	if _, err := c.in.Write(append(data, '\n')); err != nil {
		c.t.Fatal(err)
	}
	return c.read()
}

// do sends one command and returns its reply; a failed command fails the
// test.
func (c *mjsController) do(cmd map[string]any) map[string]any {
	c.t.Helper()
	r := c.try(cmd)
	if r["ok"] != true {
		c.t.Fatalf("matter.js controller: %v failed:\n%v", cmd, r["error"])
	}
	return r
}

// TestMatterJSController drives the reference daemon from matter.js's
// controller: commission, read, invoke with the subscription report that
// follows, write, a second matter.js controller on its own fabric through an
// enhanced commissioning window, and what a subscription does across a
// daemon restart.
func TestMatterJSController(t *testing.T) {
	bin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bin, "db", "listen")
	work := t.TempDir()
	br := startBridgeWith(t, bin, flags, bridgeOptions{
		dbPath:    filepath.Join(work, "dut.db"),
		appPipe:   filepath.Join(work, "app.fifo"),
		enableKey: chipTestEnableKey,
	})
	light := br.endpointFor(t, dtOnOffLight)
	contact := br.endpointFor(t, dtContactSensor)

	first := startMatterJSController(t, "gofabric-first")
	first.do(map[string]any{
		"cmd": "commission", "ip": "127.0.0.1", "port": br.info.port,
		"passcode": br.info.passcode, "discriminator": br.info.discriminator,
	})

	t.Run("read", func(t *testing.T) {
		r := first.do(map[string]any{"cmd": "read", "endpoint": 0, "cluster": "BasicInformation", "attribute": "vendorId"})
		if r["value"] == nil {
			t.Fatalf("BasicInformation.VendorID read back %v", r)
		}
	})
	t.Run("invoke and report", func(t *testing.T) {
		before := first.do(map[string]any{"cmd": "read", "endpoint": light, "cluster": "OnOff", "attribute": "onOff"})["value"]
		since := time.Now().UnixMilli()
		first.do(map[string]any{"cmd": "invoke", "endpoint": light, "cluster": "OnOff", "command": "toggle"})
		ch := first.do(map[string]any{"cmd": "waitChange", "endpoint": light, "cluster": "OnOff", "attribute": "onOff", "since": since, "timeoutMs": 10000})
		got := ch["change"].(map[string]any)["value"]
		if got == before {
			t.Fatalf("OnOff reported %v after Toggle from %v", got, before)
		}
	})
	t.Run("write", func(t *testing.T) {
		first.do(map[string]any{"cmd": "write", "endpoint": 0, "cluster": "BasicInformation", "attribute": "nodeLabel", "value": "matter.js"})
		if v := first.do(map[string]any{"cmd": "read", "endpoint": 0, "cluster": "BasicInformation", "attribute": "nodeLabel"})["value"]; v != "matter.js" {
			t.Fatalf("NodeLabel read back %v", v)
		}
	})
	t.Run("second fabric", func(t *testing.T) {
		win := first.do(map[string]any{"cmd": "openWindow", "timeout": 180})
		code, _ := win["manualPairingCode"].(string)
		passcode, short, err := decodeManualCode(code)
		if err != nil {
			t.Fatalf("manual pairing code %q: %v", code, err)
		}
		second := startMatterJSController(t, "gofabric-second")
		second.do(map[string]any{
			"cmd": "commission", "ip": "127.0.0.1", "port": br.info.port,
			"passcode": passcode, "shortDiscriminator": short,
		})
		since := time.Now().UnixMilli()
		second.do(map[string]any{"cmd": "invoke", "endpoint": light, "cluster": "OnOff", "command": "toggle"})
		// The first fabric's subscription sees the second fabric's change.
		first.do(map[string]any{"cmd": "waitChange", "endpoint": light, "cluster": "OnOff", "attribute": "onOff", "since": since, "timeoutMs": 10000})
	})
	t.Run("subscription across a restart", func(t *testing.T) {
		before := strings.Count(br.snapshotStderr(), "msg=matter.rx.im.subscribe ")
		current, _ := first.do(map[string]any{"cmd": "read", "endpoint": contact, "cluster": "BooleanState", "attribute": "stateValue"})["value"].(bool)
		br = br.restart(t)
		start := time.Now()
		since := start.UnixMilli()
		br.pipe(t, map[string]any{"Name": "SetBooleanState", "EndpointId": contact, "NewState": !current})
		r := first.try(map[string]any{"cmd": "waitChange", "endpoint": contact, "cluster": "BooleanState", "attribute": "stateValue", "since": since, "timeoutMs": 60000})
		if r["ok"] != true {
			state := first.do(map[string]any{"cmd": "state"})
			// A goroutine dump tells a hung daemon from a quiet one.
			_ = br.cmd.Process.Signal(syscall.SIGQUIT)
			time.Sleep(time.Second)
			daemonLog := saveCaseLog("matterjs/restart-daemon", br.snapshotStderr())
			controllerLog := saveCaseLog("matterjs/restart-controller", first.log.String())
			t.Fatalf("the first controller saw no change in 60 s after the restart: %v\ncontroller state: %v\n"+
				"daemon log: %s\ncontroller log: %s", r["error"], state, daemonLog, controllerLog)
		}
		elapsed := time.Since(start).Round(100 * time.Millisecond)
		logs := br.snapshotStderr()
		resubscribed := strings.Count(logs, "msg=matter.rx.im.subscribe ")
		resumed := strings.Contains(logs, "subscription.reestablish") && !strings.Contains(logs, "matter.subscription.reestablish.none")
		t.Logf("after the restart the first controller got the change in %s; daemon log: re-established=%v, "+
			"SubscribeRequests since the restart=%d (before: %d); change=%v",
			elapsed, resumed, resubscribed, before, r["change"])
		// The daemon re-establishes the subscription itself (ADR 0008,
		// matter.js InteractionServer.establishFormerSubscription): the
		// change reaches the controller without it subscribing again.
		if !resumed || resubscribed != 0 {
			t.Errorf("the subscription was not resumed by the daemon: re-established=%v, new SubscribeRequests=%d", resumed, resubscribed)
		}
		if elapsed > 10*time.Second {
			t.Errorf("the change took %s to arrive; a resumed subscription reports it within the min interval", elapsed)
		}
	})
}

// decodeManualCode extracts the passcode and the short discriminator of an
// 11-digit manual pairing code (Matter §5.1.4.1).
func decodeManualCode(code string) (passcode uint32, short uint8, err error) {
	if len(code) != 11 {
		return 0, 0, fmt.Errorf("want 11 digits, got %d", len(code))
	}
	var d [10]uint32
	for i := range 10 {
		if code[i] < '0' || code[i] > '9' {
			return 0, 0, fmt.Errorf("not a digit at %d", i)
		}
		d[i] = uint32(code[i] - '0')
	}
	chunk1 := d[0]
	chunk2 := d[1]*10000 + d[2]*1000 + d[3]*100 + d[4]*10 + d[5]
	chunk3 := d[6]*1000 + d[7]*100 + d[8]*10 + d[9]
	short = uint8((chunk1&0x3)<<2 | (chunk2>>14)&0x3)
	passcode = chunk2&0x3FFF | chunk3<<14
	return passcode, short, nil
}
