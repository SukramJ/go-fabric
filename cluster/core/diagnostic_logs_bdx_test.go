// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/cluster/core"
	diaglogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	"github.com/SukramJ/go-fabric/im"
)

// The DiagnosticLogs BDX tests follow chip's diagnostic-logs-server at the
// harness pin 6170af84 (DLC = DiagnosticLogsCluster.cpp, BDLP =
// BDXDiagnosticLogsProvider.cpp).

// dlPipe is one end of an in-memory exchange.
type dlPipe struct {
	in, out chan bdx.Message
	closed  chan struct{}
	once    sync.Once
}

func newDLPipes() (node, peer *dlPipe) {
	a, b := make(chan bdx.Message, 64), make(chan bdx.Message, 64)
	return &dlPipe{in: a, out: b, closed: make(chan struct{})}, &dlPipe{in: b, out: a, closed: make(chan struct{})}
}

func (p *dlPipe) Send(_ context.Context, proto uint16, op uint8, payload []byte) error {
	p.out <- bdx.Message{ProtocolID: proto, Opcode: op, Payload: append([]byte(nil), payload...)}
	return nil
}

func (p *dlPipe) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}
func (p *dlPipe) MaxPayloadSize() int { return 1178 }
func (p *dlPipe) Close() error        { p.once.Do(func() { close(p.closed) }); return nil }

// dlOpener hands out the node end of a fresh pipe per exchange and
// records the session the exchange was asked for.
type dlOpener struct {
	mu       sync.Mutex
	sessions []uint16
	peers    chan *dlPipe
	nodes    []*dlPipe
	err      error
}

func newDLOpener() *dlOpener { return &dlOpener{peers: make(chan *dlPipe, 4)} }

func (o *dlOpener) OpenSessionExchange(_ context.Context, sessionID uint16) (bdx.Exchange, error) {
	if o.err != nil {
		return nil, o.err
	}
	n, p := newDLPipes()
	o.mu.Lock()
	o.sessions = append(o.sessions, sessionID)
	o.nodes = append(o.nodes, n)
	o.mu.Unlock()
	o.peers <- p
	return n, nil
}

// receive serves the next exchange as chip-tool's BDX receiver does:
// SendAccept, then the blocks into the returned buffer.
func (o *dlOpener) receive(t *testing.T, refuse bool) (*bytes.Buffer, chan error, bdx.Init) {
	t.Helper()
	var p *dlPipe
	// The exchange opens on the server's own goroutine; a server that
	// never opens one hangs the test until go test's timeout dumps it —
	// no wall-clock bound that a loaded machine could outrun.
	p = <-o.peers
	first, err := p.Receive(context.Background())
	if err != nil || bdx.MessageType(first.Opcode) != bdx.MessageSendInit {
		t.Fatalf("first message %+v %v", first, err)
	}
	init, err := bdx.DecodeInit(first.Payload)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	buf := &bytes.Buffer{}
	if refuse {
		_ = p.Send(context.Background(), bdx.SecureChannelProtocolID, bdx.OpcodeStatusReport, bdx.EncodeStatusReport(bdx.GeneralFailure, bdx.StatusFileDesignatorUnknown))
		done <- nil
		return buf, done, init
	}
	go func() {
		// The receiver's idle bound is not under test: an hour, so the
		// gap a test leaves between the SendAccept and the blocks (the
		// response held back until after.Run) never times it out on a
		// loaded machine, as 5 s could.
		_, err := bdx.Serve(context.Background(), bdx.NewMessenger(p, time.Hour), bdx.ServeOptions{
			InitType: bdx.MessageSendInit, Init: init, TransferFileDesignator: bdx.FileDesignator(init.FileDesignator),
			Sink: &bufSink{buf: buf},
		})
		done <- err
	}()
	return buf, done, init
}

type bufSink struct{ buf *bytes.Buffer }

func (s *bufSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *bufSink) Close(error) error           { return nil }

// dlCollector is a LogCollector over a byte slice with oldest-entry
// timestamps, recording the end of each collection.
type dlCollector struct {
	logs     []byte
	utc      *uint64
	boot     *uint64
	startErr error
	sizeErr  error
	ended    chan error
	gate     chan struct{} // when set, CollectLog waits for it
}

func (c *dlCollector) GetSizeForIntent(context.Context, uint8) (uint64, error) {
	return uint64(len(c.logs)), c.sizeErr
}

func (c *dlCollector) GetLogForIntent(_ context.Context, _ uint8, maxBytes int) ([]byte, core.LogTimestamps, error) {
	b := c.logs
	if len(b) > maxBytes {
		b = b[len(b)-maxBytes:]
	}
	return b, core.LogTimestamps{UTC: c.utc, SinceBoot: c.boot}, nil
}

func (c *dlCollector) StartLogCollection(context.Context, uint8) (core.LogSession, core.LogTimestamps, error) {
	if c.startErr != nil {
		return nil, core.LogTimestamps{}, c.startErr
	}
	return &dlSession{c: c, rest: c.logs}, core.LogTimestamps{UTC: c.utc, SinceBoot: c.boot}, nil
}

type dlSession struct {
	c    *dlCollector
	rest []byte
}

func (s *dlSession) CollectLog(ctx context.Context, buf []byte) (n int, endOfLog bool, err error) {
	if s.c.gate != nil {
		select {
		case <-s.c.gate:
		case <-ctx.Done():
			return 0, false, ctx.Err()
		}
	}
	n = copy(buf, s.rest)
	s.rest = s.rest[n:]
	return n, len(s.rest) == 0, nil
}

func (s *dlSession) EndLogCollection(err error) {
	if s.c.ended != nil {
		s.c.ended <- err
	}
}

func ptr64(v uint64) *uint64 { return &v }

func bdxRequest(designator string) diaglogsdef.RetrieveLogsRequestRequest {
	return diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEndUserSupport, RequestedProtocol: diaglogsdef.TransferProtocolBdx, TransferFileDesignator: &designator}
}

// invokeCtx is a command on CASE session 0x0102 that defers work until
// its response is out, as the bridge's receive path does.
func invokeCtx() (context.Context, *im.AfterResponse) {
	after := &im.AfterResponse{}
	return im.WithAfterResponse(core.WithInvokeSessionID(context.Background(), 0x0102), after), after
}

func dlStatusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var se interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want an IM status", err)
	}
	return se.MatterStatusCode()
}

// TestDiagLogsBDX_RequestRules: BDX without a designator is
// InvalidCommand, one over 32 characters ConstraintError (DLC:121-125); an
// intent or protocol outside its enum InvalidCommand (DLC:194-197).
func TestDiagLogsBDX_RequestRules(t *testing.T) {
	t.Parallel()
	d := core.NewDiagnosticLogs()
	d.AttachCollector(&dlCollector{logs: bytes.Repeat([]byte{1}, 5000)})
	ctx, _ := invokeCtx()
	req := bdxRequest("x")
	req.TransferFileDesignator = nil
	if _, err := d.MatterInvoke(ctx, 0x00, req); dlStatusOf(t, err) != im.StatusInvalidCommand {
		t.Fatalf("no designator: %v", err)
	}
	if _, err := d.MatterInvoke(ctx, 0x00, bdxRequest(strings.Repeat("a", 33))); dlStatusOf(t, err) != im.StatusConstraintError {
		t.Fatalf("33 characters: %v", err)
	}
	if _, err := d.MatterInvoke(ctx, 0x00, diaglogsdef.RetrieveLogsRequestRequest{Intent: 3}); dlStatusOf(t, err) != im.StatusInvalidCommand {
		t.Fatalf("intent 3: %v", err)
	}
	if _, err := d.MatterInvoke(ctx, 0x00, diaglogsdef.RetrieveLogsRequestRequest{RequestedProtocol: 2}); dlStatusOf(t, err) != im.StatusInvalidCommand {
		t.Fatalf("protocol 2: %v", err)
	}
}

// TestDiagLogsBDX_WithoutTransfer: no collector is NoLogs (DLC:126-131),
// no logs NoLogs (:133-135), logs that fit the response Exhausted with the
// content and no BDX session (:137-140), and a node without BDX the newest
// 1024 bytes, Exhausted (:158-160).
func TestDiagLogsBDX_WithoutTransfer(t *testing.T) {
	t.Parallel()
	ctx, _ := invokeCtx()
	opener := newDLOpener()
	d := core.NewDiagnosticLogs()
	d.AttachExchangeOpener(opener)
	resp, err := d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt"))
	if err != nil || resp.(core.RetrieveLogsResponse).Status != core.LogStatusNoLogs {
		t.Fatalf("no collector: %+v %v", resp, err)
	}
	d.AttachCollector(&dlCollector{})
	if resp, _ := d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt")); resp.(core.RetrieveLogsResponse).Status != core.LogStatusNoLogs {
		t.Fatalf("no logs: %+v", resp)
	}
	small := bytes.Repeat([]byte("s"), 100)
	d.AttachCollector(&dlCollector{logs: small, utc: ptr64(7)})
	r := mustResp(t, d, ctx, bdxRequest("logs.txt"))
	if r.Status != core.LogStatusExhausted || !bytes.Equal(r.LogContent, small) || r.UTCTimeStamp != 7 {
		t.Fatalf("100 bytes: %+v", r)
	}
	if utc, boot := r.HasTimestamps(); !utc || boot {
		t.Fatal("the collector's UTC timestamp alone")
	}
	if len(opener.sessions) != 0 {
		t.Fatal("a BDX exchange was opened for logs that fit the response")
	}

	big := append(bytes.Repeat([]byte("b"), 2000), []byte("end")...)
	d2 := core.NewDiagnosticLogs()
	d2.AttachCollector(&dlCollector{logs: big})
	r = mustResp(t, d2, ctx, bdxRequest("logs.txt"))
	if r.Status != core.LogStatusExhausted || len(r.LogContent) != 1024 || !bytes.HasSuffix(r.LogContent, []byte("end")) {
		t.Fatalf("no BDX support: status %d, %d bytes", r.Status, len(r.LogContent))
	}
}

func mustResp(t *testing.T, d *core.DiagnosticLogs, ctx context.Context, req diaglogsdef.RetrieveLogsRequestRequest) core.RetrieveLogsResponse { //nolint:revive // test helper, t first
	t.Helper()
	resp, err := d.MatterInvoke(ctx, 0x00, req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.(core.RetrieveLogsResponse)
}

// TestDiagLogsBDX_Transfer: logs larger than the response go over BDX —
// a new exchange on the command's session, a SendInit offering sender
// drive and 1024-byte blocks with the request's designator and no length
// (BDLP:67-79), Success with the collection's oldest-entry timestamps once
// the receiver accepted (BDLP:167-174, :250-263), the blocks only after the
// response is out, EndLogCollection on completion. While it runs a second
// request is Busy (DLC:146-150).
func TestDiagLogsBDX_Transfer(t *testing.T) {
	t.Parallel()
	logs := make([]byte, 5*1024)
	for i := range logs {
		logs[i] = byte('a' + i%26)
	}
	gate := make(chan struct{})
	c := &dlCollector{logs: logs, utc: ptr64(1234), boot: ptr64(99), ended: make(chan error, 1), gate: gate}
	opener := newDLOpener()
	d := core.NewDiagnosticLogs()
	d.AttachCollector(c)
	d.AttachExchangeOpener(opener)

	ctx, after := invokeCtx()
	type answer struct {
		r   core.RetrieveLogsResponse
		err error
	}
	answered := make(chan answer, 1)
	go func() {
		resp, err := d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt"))
		r, _ := resp.(core.RetrieveLogsResponse)
		answered <- answer{r, err}
	}()
	buf, done, init := opener.receive(t, false)
	a := <-answered
	if a.err != nil || a.r.Status != core.LogStatusSuccess || len(a.r.LogContent) != 0 || a.r.UTCTimeStamp != 1234 || a.r.TimeSinceBoot != 99 {
		t.Fatalf("response %+v %v", a.r, a.err)
	}
	if !init.TransferControl.SenderDrive || init.TransferControl.ReceiverDrive || init.MaxBlockSize != 1024 || init.MaxLength != nil || string(init.FileDesignator) != "logs.txt" {
		t.Fatalf("SendInit %+v", init)
	}
	if opener.sessions[0] != 0x0102 {
		t.Fatalf("exchange on session %d", opener.sessions[0])
	}
	// Busy while the transfer runs.
	ctx2, _ := invokeCtx()
	if r := mustResp(t, d, ctx2, bdxRequest("logs2.txt")); r.Status != core.LogStatusBusy {
		t.Fatalf("second request: %+v", r)
	}
	// Nothing streams before the response is out.
	select {
	case <-done:
		t.Fatal("the transfer ended before the response went out")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	after.Run()
	if err := <-done; err != nil {
		t.Fatalf("receiver: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), logs) {
		t.Fatalf("received %d bytes, want %d", buf.Len(), len(logs))
	}
	if err := <-c.ended; err != nil {
		t.Fatalf("EndLogCollection(%v), want nil", err)
	}
	// The next request is no longer Busy.
	ctx3, after3 := invokeCtx()
	third := make(chan struct{})
	go func() {
		_, _ = d.MatterInvoke(ctx3, 0x00, bdxRequest("logs3.txt"))
		close(third)
	}()
	_, done3, _ := opener.receive(t, false)
	<-third
	after3.Run()
	if err := <-done3; err != nil {
		t.Fatal(err)
	}
}

// TestDiagLogsBDX_Denied: a receiver that refuses the SendInit
// (BDLP:219-227), a collection that cannot start (DLC:152-157 through
// BDLP:65), a collector that fails and a session without an exchange are
// Denied, and the collection ends.
func TestDiagLogsBDX_Denied(t *testing.T) {
	t.Parallel()
	logs := bytes.Repeat([]byte{'x'}, 3000)

	c := &dlCollector{logs: logs, ended: make(chan error, 1)}
	opener := newDLOpener()
	d := core.NewDiagnosticLogs()
	d.AttachCollector(c)
	d.AttachExchangeOpener(opener)
	ctx, _ := invokeCtx()
	answered := make(chan core.RetrieveLogsResponse, 1)
	go func() {
		resp, _ := d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt"))
		answered <- resp.(core.RetrieveLogsResponse)
	}()
	_, _, _ = opener.receive(t, true)
	if r := <-answered; r.Status != core.LogStatusDenied {
		t.Fatalf("refused SendInit: %+v", r)
	}
	if err := <-c.ended; err == nil {
		t.Fatal("EndLogCollection without the failure")
	}

	for name, coll := range map[string]*dlCollector{
		"start fails": {logs: logs, startErr: errors.New("no log store")},
		"size fails":  {logs: logs, sizeErr: errors.New("no log store")},
	} {
		d := core.NewDiagnosticLogs()
		d.AttachCollector(coll)
		d.AttachExchangeOpener(newDLOpener())
		if r := mustResp(t, d, ctx, bdxRequest("logs.txt")); r.Status != core.LogStatusDenied {
			t.Errorf("%s: %+v", name, r)
		}
	}
	noExchange := newDLOpener()
	noExchange.err = errors.New("session gone")
	d = core.NewDiagnosticLogs()
	d.AttachCollector(&dlCollector{logs: logs})
	d.AttachExchangeOpener(noExchange)
	if r := mustResp(t, d, ctx, bdxRequest("logs.txt")); r.Status != core.LogStatusDenied {
		t.Errorf("no exchange: %+v", r)
	}
	// Each failure released the transfer slot: not Busy afterwards.
	if r := mustResp(t, d, ctx, bdxRequest("logs.txt")); r.Status == core.LogStatusBusy {
		t.Error("a failed transfer left the server Busy")
	}
}
