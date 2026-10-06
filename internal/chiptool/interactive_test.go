// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 §4.2.2 fixes SHA-1 for Sec-WebSocket-Accept; it is not used for security here
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file drives chip-tool's `interactive server` — the same websocket
// surface the CSA YAML runner uses — from Go.
//
// Why not one chip-tool process per command, as commission_test.go does: a
// process per command pays chip-tool's start-up, a fresh CASE handshake and
// an avahi resolve every time, a second or more each. The broad suite issues
// hundreds of reads; at that rate it would not finish in the Makefile's
// timeout. One interactive server keeps one commissioner stack and one CASE
// session alive for the whole run, and answers each command with structured
// JSON instead of log lines.
//
// The protocol, read at the pinned commit (examples/chip-tool/commands/
// interactive/InteractiveCommands.cpp, InteractiveServerCommand and
// InteractiveServerResult; examples/common/websocket-server/
// WebSocketServer.cpp):
//
//   - the client sends one text frame holding a command line, tokenised like
//     a shell with single quotes only (Commands.cpp,
//     DecodeArgumentsFromStringStream: std::quoted(arg, '\''));
//   - the server runs it to completion on its own thread and answers with
//     one text frame: {"results": [...], "logs": [...]}. Each result is one
//     RemoteDataModelLogger JSON object — an attribute, a command response,
//     an event, or {"error": ...}; a failed command appends
//     {"error": "FAILURE"} last;
//   - an EMPTY frame, or one holding only a number (a timeout in seconds),
//     does not run anything: it arms the "async report" mode, and the next
//     subscription report or event is sent as its own frame. With a
//     timeout, chip-tool sends {"error": "...TIMEOUT"} when nothing arrives;
//   - the frame `quit()` stops the server.
//
// The server prints "== WebSocket Server Ready" on stdout once it listens
// (WebSocketServer.cpp kWebSocketServerReadyMessage) — the same line the
// YAML runner waits for.

// wsReadyMarker is the server's readiness line.
const wsReadyMarker = "== WebSocket Server Ready"

// interactive is one running `chip-tool interactive server` bound to a
// controller's storage directory.
type interactive struct {
	ctl  *controller
	cmd  *exec.Cmd
	port int

	conn net.Conn
	rd   *bufio.Reader

	// mu serialises commands: the server handles one at a time and answers
	// in order, so a second writer would read the first one's answer.
	mu sync.Mutex

	outMu sync.Mutex
	out   bytes.Buffer
	done  chan struct{}
}

// imResult is one entry of an interactive answer's "results" array.
type imResult map[string]any

// imAnswer is one parsed interactive answer.
type imAnswer struct {
	Results []imResult `json:"results"`
	Logs    []struct {
		Module   string `json:"module"`
		Category string `json:"category"`
		Message  string `json:"message"`
	} `json:"logs"`
}

// failed reports whether chip-tool marked the command as failed — the
// trailing {"error": "FAILURE"} InteractiveServerResult.AsJsonString adds
// for a non-zero status.
func (a *imAnswer) failed() bool {
	for _, r := range a.Results {
		if s, ok := r["error"].(string); ok && s == "FAILURE" {
			return true
		}
	}
	return false
}

// logText decodes the base64 log messages into one string, for failure
// output and for the few assertions that only chip-tool's log can make.
func (a *imAnswer) logText() string {
	var b strings.Builder
	for _, l := range a.Logs {
		msg, err := base64.StdEncoding.DecodeString(l.Message)
		if err != nil {
			msg = []byte(l.Message)
		}
		fmt.Fprintf(&b, "[%s] %s\n", l.Module, msg)
	}
	return b.String()
}

// startInteractive launches `chip-tool interactive server` on the
// controller's storage directory and connects to it.
//
// The storage flag goes after `interactive server` for the reason
// controller.run gives: argv[1] is the command set.
func (c *controller) startInteractive(t *testing.T) *interactive {
	t.Helper()

	port, err := freeTCPPort()
	if err != nil {
		t.Fatalf("pick a websocket port: %v", err)
	}
	args := []string{
		"interactive", "server",
		"--port", strconv.Itoa(port),
		"--storage-directory", c.storageDir,
	}
	if c.commissionerName != "" {
		args = append(args, "--commissioner-name", c.commissionerName)
	}
	cmd := exec.Command(c.bin, args...) //nolint:gosec // c.bin is resolved by requireChipTool
	cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1")

	s := &interactive{ctl: c, cmd: cmd, port: port, done: make(chan struct{})}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chip-tool interactive server: %v", err)
	}
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		signalled := false
		for sc.Scan() {
			line := stripANSI(sc.Text())
			s.outMu.Lock()
			s.out.WriteString(line)
			s.out.WriteByte('\n')
			s.outMu.Unlock()
			if !signalled && strings.Contains(line, wsReadyMarker) {
				signalled = true
				close(ready)
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		_ = pw.Close()
		close(s.done)
	}()
	t.Cleanup(func() { s.stop() })

	select {
	case <-ready:
	case <-s.done:
		t.Fatalf("chip-tool interactive server exited before it was ready:\n%s", s.output())
	case <-time.After(60 * time.Second):
		t.Fatalf("chip-tool interactive server printed no %q within 60s:\n%s", wsReadyMarker, s.output())
	}

	conn, rd, err := wsDial(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("connect to chip-tool's websocket on port %d: %v\n%s", port, err, s.output())
	}
	s.conn, s.rd = conn, rd
	return s
}

// output returns everything the server process printed so far.
func (s *interactive) output() string {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	return s.out.String()
}

// stop asks the server to quit and kills it if it does not.
func (s *interactive) stop() {
	if s.conn != nil {
		_ = s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = wsWriteText(s.conn, "quit()")
		_ = s.conn.Close()
	}
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
	}
}

// exec sends one command line and returns the parsed answer. A transport
// failure (the server died, the deadline passed) is an error; a command
// chip-tool ran and reported as failed is not — the caller asserts on
// answer.failed() and on the per-path errors, because a negative test
// expects exactly that.
func (s *interactive) exec(ctx context.Context, line string) (*imAnswer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > chipToolTimeout {
		deadline = time.Now().Add(chipToolTimeout)
	}
	_ = s.conn.SetDeadline(deadline)
	if err := wsWriteText(s.conn, line); err != nil {
		return nil, fmt.Errorf("send %q: %w", line, err)
	}
	return s.readAnswer(line)
}

// readAnswer reads the next frame as an answer.
func (s *interactive) readAnswer(what string) (*imAnswer, error) {
	payload, err := wsReadText(s.rd, s.conn)
	if err != nil {
		return nil, fmt.Errorf("read the answer to %q: %w\n--- chip-tool output ---\n%s", what, err, s.output())
	}
	var a imAnswer
	if err := json.Unmarshal(payload, &a); err != nil {
		return nil, fmt.Errorf("answer to %q is not JSON: %w\n%s", what, err, payload)
	}
	return &a, nil
}

// freeTCPPort returns a currently unused loopback TCP port. The window
// between closing it here and chip-tool binding it is a race in principle;
// in practice the kernel does not hand the same ephemeral port out again
// that quickly.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// --- a minimal RFC 6455 client ------------------------------------------
//
// The module takes no websocket dependency for one test harness; the client
// half of RFC 6455 that talking to libwebsockets needs is short: an HTTP/1.1
// upgrade, masked client frames, and unmasked server frames that may be
// fragmented and may carry 16- or 64-bit lengths.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// wsDial opens the websocket. libwebsockets binds its port shortly after
// printing the ready line, so the dial is retried for a few seconds.
func wsDial(addr string) (net.Conn, *bufio.Reader, error) {
	var conn net.Conn
	var err error
	for range 50 {
		conn, err = net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return nil, nil, err
	}
	var key [16]byte
	_, _ = rand.Read(key[:])
	k := base64.StdEncoding.EncodeToString(key[:])
	req := "GET / HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + k + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	rd := bufio.NewReaderSize(conn, 64*1024)
	resp, err := http.ReadResponse(rd, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("websocket upgrade: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("websocket upgrade answered %s", resp.Status)
	}
	sum := sha1.Sum([]byte(k + wsGUID)) //nolint:gosec // see the import comment
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), base64.StdEncoding.EncodeToString(sum[:]); got != want {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("websocket accept key %q, want %q", got, want)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, rd, nil
}

// wsWriteText sends one masked, unfragmented text frame. chip-tool reads a
// command from a single LWS_CALLBACK_RECEIVE, so a command must never be
// split across frames.
func wsWriteText(w io.Writer, text string) error {
	payload := []byte(text)
	var hdr []byte
	hdr = append(hdr, 0x81) // FIN + text
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	hdr = append(hdr, mask[:]...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	_, err := w.Write(append(hdr, masked...))
	return err
}

// wsReadText reads one complete text message, answering pings and skipping
// pongs on the way.
func wsReadText(rd *bufio.Reader, w io.Writer) ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(rd, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		op := h[0] & 0x0F
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(rd, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(rd, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(rd, mask); err != nil {
				return nil, err
			}
		}
		if n > 64<<20 {
			return nil, fmt.Errorf("websocket frame of %d bytes", n)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(rd, payload); err != nil {
			return nil, err
		}
		for i := range payload {
			if mask != nil {
				payload[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8:
			return nil, errors.New("websocket closed by chip-tool")
		case 0x9: // ping → pong
			if err := wsWriteControl(w, 0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

// wsWriteControl sends a masked control frame.
func wsWriteControl(w io.Writer, op byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	frame := make([]byte, 0, 2+len(mask)+len(payload))
	frame = append(frame, 0x80|op, 0x80|byte(len(payload)))
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.Write(frame)
	return err
}

// --- result helpers -----------------------------------------------------

// reTypeSuffix strips the ":TYPE" suffix chip's TlvToJson appends to struct
// field keys ("0:UINT", "3:ARRAY-STRUCT"; src/lib/support/jsontlv/
// TlvToJson.cpp, JsonObjectElementContext::GenerateJsonElementName).
var reTypeSuffix = regexp.MustCompile(`^(\d+):[A-Z?-]+$`)

// field returns a struct field of a chip-tool JSON struct by its context
// tag, whatever type suffix chip-tool appended.
func field(v any, tag int) (any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	want := strconv.Itoa(tag)
	for k, x := range m {
		if mm := reTypeSuffix.FindStringSubmatch(k); len(mm) > 1 && mm[1] == want {
			return x, true
		}
		if k == want {
			return x, true
		}
	}
	return nil, false
}

// asInt converts a JSON number (or chip-tool's decimal string for values
// beyond 32 bits) to int64.
func asInt(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			u, uerr := strconv.ParseUint(x, 10, 64)
			if uerr != nil {
				return 0, false
			}
			return int64(u), true //nolint:gosec // a 64-bit id compared by bit pattern
		}
		return n, true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	}
	return 0, false
}

// asInts converts a JSON array of numbers.
func asInts(v any) ([]int64, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int64, 0, len(arr))
	for _, e := range arr {
		n, ok := asInt(e)
		if !ok {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
