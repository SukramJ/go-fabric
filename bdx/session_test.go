// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// pipeEnd is one end of an in-memory exchange.
type pipeEnd struct {
	in      chan bdx.Message
	out     chan bdx.Message
	maxSize int
}

func newPipe(maxPayload int) (*pipeEnd, *pipeEnd) {
	a2b := make(chan bdx.Message, 64)
	b2a := make(chan bdx.Message, 64)
	return &pipeEnd{in: b2a, out: a2b, maxSize: maxPayload}, &pipeEnd{in: a2b, out: b2a, maxSize: maxPayload}
}

func (p *pipeEnd) Send(_ context.Context, protocolID uint16, opcode uint8, payload []byte) error {
	p.out <- bdx.Message{ProtocolID: protocolID, Opcode: opcode, Payload: append([]byte(nil), payload...)}
	return nil
}

func (p *pipeEnd) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}

func (p *pipeEnd) MaxPayloadSize() int { return p.maxSize }
func (p *pipeEnd) Close() error        { return nil }

// memSink is a Sink in memory.
type memSink struct {
	buf       bytes.Buffer
	closed    bool
	committed bool
}

func (s *memSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *memSink) Close(err error) error {
	s.closed = true
	s.committed = err == nil
	return nil
}

// sender is a scripted BDX sender responder: it answers the ReceiveInit
// with accept, then serves data in blocks of the negotiated size.
type sender struct {
	t      *testing.T
	end    *pipeEnd
	accept bdx.ReceiveAccept
	data   []byte
	// tamper, when set, changes the n-th block before it is sent.
	tamper func(n int, t bdx.MessageType, b bdx.Block) (bdx.MessageType, bdx.Block)
	// gotInit receives the ReceiveInit.
	gotInit chan bdx.Init
	// gotStatus receives the StatusReport the receiver sent, if any.
	gotStatus chan bdx.StatusReport
	// gotAckEOF receives the BlockAckEof counter.
	gotAckEOF chan uint32
}

func newSender(t *testing.T, end *pipeEnd, accept bdx.ReceiveAccept, data []byte) *sender {
	return &sender{
		t: t, end: end, accept: accept, data: data,
		gotInit: make(chan bdx.Init, 1), gotStatus: make(chan bdx.StatusReport, 1), gotAckEOF: make(chan uint32, 1),
	}
}

func (s *sender) next(ctx context.Context) (bdx.Message, bool) {
	m, err := s.end.Receive(ctx)
	if err != nil {
		return m, false
	}
	if m.ProtocolID == bdx.SecureChannelProtocolID && m.Opcode == bdx.OpcodeStatusReport {
		sr, _ := bdx.DecodeStatusReport(m.Payload)
		s.gotStatus <- sr
		return m, false
	}
	return m, true
}

func (s *sender) serve(ctx context.Context) {
	m, ok := s.next(ctx)
	if !ok {
		return
	}
	init, err := bdx.DecodeInit(m.Payload)
	if err != nil || bdx.MessageType(m.Opcode) != bdx.MessageReceiveInit {
		s.t.Errorf("sender: first message %#x: %v", m.Opcode, err)
		return
	}
	s.gotInit <- init
	payload, err := s.accept.Encode()
	if err != nil {
		s.t.Errorf("sender: accept: %v", err)
		return
	}
	_ = s.end.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageReceiveAccept), payload)

	size := int(s.accept.MaxBlockSize)
	senderDrives := s.accept.TransferControl.SenderDrive
	for n, off := 0, 0; ; n++ {
		if !senderDrives {
			q, ok := s.next(ctx)
			if !ok {
				return
			}
			if bdx.MessageType(q.Opcode) != bdx.MessageBlockQuery {
				s.t.Errorf("sender: expected BlockQuery, got %#x", q.Opcode)
				return
			}
		}
		end := min(off+size, len(s.data))
		t := bdx.MessageBlock
		if end == len(s.data) {
			t = bdx.MessageBlockEOF
		}
		b := bdx.Block{BlockCounter: uint32(n), Data: s.data[off:end]} //nolint:gosec // test sizes
		if s.tamper != nil {
			t, b = s.tamper(n, t, b)
		}
		_ = s.end.Send(ctx, bdx.ProtocolID, uint8(t), b.Encode())
		off = end
		if t == bdx.MessageBlockEOF {
			r, ok := s.next(ctx)
			if !ok {
				return
			}
			if bdx.MessageType(r.Opcode) != bdx.MessageBlockAckEOF {
				s.t.Errorf("sender: expected BlockAckEof, got %#x", r.Opcode)
				return
			}
			c, _ := bdx.DecodeCounterOnly(r.Payload)
			s.gotAckEOF <- c.BlockCounter
			return
		}
		if senderDrives {
			a, ok := s.next(ctx)
			if !ok {
				return
			}
			c, _ := bdx.DecodeCounterOnly(a.Payload)
			if bdx.MessageType(a.Opcode) != bdx.MessageBlockAck || c.BlockCounter != uint32(n) { //nolint:gosec // test sizes
				s.t.Errorf("sender: expected BlockAck %d, got %#x %d", n, a.Opcode, c.BlockCounter)
				return
			}
		}
	}
}

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i ^ 0x42)
	}
	return b
}

func u64(v uint64) *uint64 { return &v }

func receiverAccept(length *uint64) bdx.ReceiveAccept {
	return bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, Length: length}
}

// TestReceive_ReceiverDrive_DrivingReceivingFlow mirrors matter.js
// flow/DrivingReceivingFlow.ts: BlockQuery per block, BlockAckEof with the
// last counter; the ReceiveInit offers what OtaSoftwareUpdateRequestorServer
// #handleBdxDownload asks (:971-977: ReceiverDrive only, 1024 bytes).
func TestReceive_ReceiverDrive_DrivingReceivingFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	data := testData(3000)
	s := newSender(t, b, receiverAccept(u64(3000)), data)
	go s.serve(ctx)

	sink := &memSink{}
	var progress []uint64
	res, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{
		TransferFileDesignator: bdx.FileDesignator("fff1-8001-v2.ota"),
		Sink:                   sink,
		PreferredDriverModes:   []bdx.DriverMode{bdx.ReceiverDrive},
		MaxBlockSize:           1024,
		Progress:               func(n uint64, _ *uint64) { progress = append(progress, n) },
	})
	if err != nil {
		t.Fatal(err)
	}
	init := <-s.gotInit
	if init.TransferControl.SenderDrive || !init.TransferControl.ReceiverDrive || init.MaxBlockSize != 1024 || string(init.FileDesignator) != "fff1-8001-v2.ota" {
		t.Fatalf("ReceiveInit %+v", init)
	}
	if got := <-s.gotAckEOF; got != 2 {
		t.Fatalf("BlockAckEof counter %d, want 2", got)
	}
	if !sink.committed || !bytes.Equal(sink.buf.Bytes(), data) {
		t.Fatalf("sink committed=%v len=%d", sink.committed, sink.buf.Len())
	}
	if res.Transferred != 3000 || res.Mode != bdx.ReceiverDrive || res.BlockSize != 1024 {
		t.Fatalf("result %+v", res)
	}
	// Flow.ts:121-147: 0 after negotiation, after each non-final block,
	// and once more at the end.
	want := []uint64{0, 1024, 2048, 3000}
	if len(progress) != len(want) {
		t.Fatalf("progress %v, want %v", progress, want)
	}
	for i := range want {
		if progress[i] != want[i] {
			t.Fatalf("progress %v, want %v", progress, want)
		}
	}
}

// TestReceive_SenderDrive_FollowingSendingFlow mirrors
// flow/FollowingSendingFlow.ts: a BlockAck after every block but the last.
func TestReceive_SenderDrive_FollowingSendingFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	data := testData(2500)
	acc := bdx.ReceiveAccept{TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 1000}
	s := newSender(t, b, acc, data)
	go s.serve(ctx)
	sink := &memSink{}
	res, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	init := <-s.gotInit
	// BdxSessionConfiguration.DefaultConfig offers both modes.
	if !init.TransferControl.SenderDrive || !init.TransferControl.ReceiverDrive || init.MaxBlockSize != 1174 {
		t.Fatalf("ReceiveInit %+v", init)
	}
	if <-s.gotAckEOF != 2 || !sink.committed || !bytes.Equal(sink.buf.Bytes(), data) || res.Mode != bdx.SenderDrive {
		t.Fatalf("transfer: committed=%v res=%+v", sink.committed, res)
	}
}

// TestReceive_BlockSizeCappedToTransport mirrors
// bdx-session-initiator.ts:19-27 + :155-165: the offered block size is the
// payload size minus the block counter, lowered to a configured maximum,
// and an accepted larger size is capped to the transport.
func TestReceive_BlockSizeCappedToTransport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(504)
	s := newSender(t, b, bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 500}, testData(100))
	go s.serve(ctx)
	res, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}, MaxBlockSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if init := <-s.gotInit; init.MaxBlockSize != 500 {
		t.Fatalf("offered %d, want 500 (504 - 4)", init.MaxBlockSize)
	}
	if res.BlockSize != 500 {
		t.Fatalf("negotiated %d", res.BlockSize)
	}
}

func expectBdxError(t *testing.T, err error, code bdx.StatusCode) *bdx.Error {
	t.Helper()
	be, ok := bdx.AsError(err)
	if !ok || be.Code != code {
		t.Fatalf("err = %v, want BDX error %s", err, code)
	}
	return be
}

// TestReceive_BadBlockCounter mirrors Flow.validateCounter (Flow.ts:103-111):
// BadBlockCounter, reported to the peer (BdxSession.ts:188-191), the sink
// discarded.
func TestReceive_BadBlockCounter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	s := newSender(t, b, receiverAccept(nil), testData(3000))
	s.tamper = func(n int, t bdx.MessageType, blk bdx.Block) (bdx.MessageType, bdx.Block) {
		if n == 1 {
			blk.BlockCounter = 5
		}
		return t, blk
	}
	go s.serve(ctx)
	sink := &memSink{}
	_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: sink, PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}})
	be := expectBdxError(t, err, bdx.StatusBadBlockCounter)
	if be.BytesTransferred != 1024 {
		t.Fatalf("bytes transferred %d", be.BytesTransferred)
	}
	if sr := <-s.gotStatus; sr.GeneralStatus != bdx.GeneralFailure || sr.ProtocolStatus != bdx.StatusBadBlockCounter {
		t.Fatalf("StatusReport %+v", sr)
	}
	if !sink.closed || sink.committed {
		t.Fatal("sink not discarded")
	}
}

// TestReceive_LengthMismatch mirrors InboundFlow.writeDataChunk
// (InboundFlow.ts:86-94): BlockEof with bytes left is LengthTooShort, with
// more than announced LengthTooLarge.
func TestReceive_LengthMismatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		announced uint64
		code      bdx.StatusCode
	}{{4000, bdx.StatusLengthTooShort}, {2000, bdx.StatusLengthTooLarge}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a, b := newPipe(1178)
		s := newSender(t, b, receiverAccept(u64(tc.announced)), testData(3000))
		go s.serve(ctx)
		_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}})
		be := expectBdxError(t, err, tc.code)
		if be.TotalBytesLength == nil || *be.TotalBytesLength != tc.announced {
			t.Fatalf("total %v", be.TotalBytesLength)
		}
		cancel()
	}
}

// TestReceive_EmptyBlock mirrors BdxMessenger.readBlock (:198-205).
func TestReceive_EmptyBlock(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	s := newSender(t, b, receiverAccept(nil), testData(3000))
	s.tamper = func(_ int, t bdx.MessageType, blk bdx.Block) (bdx.MessageType, bdx.Block) {
		blk.Data = nil
		return t, blk
	}
	go s.serve(ctx)
	_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}})
	expectBdxError(t, err, bdx.StatusBadMessageContent)
}

// TestReceive_AsynchronousRefused mirrors collectAcceptParameters
// (bdx-session-initiator.ts:233-239).
func TestReceive_AsynchronousRefused(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	s := newSender(t, b, bdx.ReceiveAccept{TransferControl: bdx.TransferControl{SenderDrive: true, AsynchronousTransfer: true}, MaxBlockSize: 100}, testData(10))
	go s.serve(ctx)
	_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}})
	expectBdxError(t, err, bdx.StatusTransferMethodNotSupported)
	if sr := <-s.gotStatus; sr.ProtocolStatus != bdx.StatusTransferMethodNotSupported {
		t.Fatalf("StatusReport %+v", sr)
	}
}

// TestReceive_PeerStatusReport mirrors throwIfErrorStatusReport
// (BdxMessenger.ts:274-296): the peer's failure ends the transfer with a
// StatusResponseError and no StatusReport back.
func TestReceive_PeerStatusReport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	go func() {
		if _, err := b.Receive(ctx); err != nil {
			return
		}
		_ = b.Send(ctx, bdx.SecureChannelProtocolID, bdx.OpcodeStatusReport, bdx.EncodeStatusReport(bdx.GeneralFailure, bdx.StatusFileDesignatorUnknown))
	}()
	sink := &memSink{}
	_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: sink})
	var sre *bdx.StatusResponseError
	if !errors.As(err, &sre) || sre.ProtocolStatus != bdx.StatusFileDesignatorUnknown || sre.GeneralStatus != bdx.GeneralFailure {
		t.Fatalf("err = %v", err)
	}
	select {
	case m := <-b.in:
		t.Fatalf("receiver answered a peer failure with %+v", m)
	default:
	}
	if !sink.closed || sink.committed {
		t.Fatal("sink not discarded")
	}
}

// TestReceive_UnexpectedMessage mirrors BdxMessenger.nextMessage
// (:95-98): a wrong message type is not a BDX error and sends nothing.
func TestReceive_UnexpectedMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	go func() {
		if _, err := b.Receive(ctx); err != nil {
			return
		}
		_ = b.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockAck), bdx.CounterOnly{}.Encode())
	}()
	_, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}})
	if !errors.Is(err, bdx.ErrUnexpectedMessage) {
		t.Fatalf("err = %v", err)
	}
}

// TestReceive_IdleTimeout: the messenger gives up after its message
// timeout (BdxMessenger.ts:37, :89).
func TestReceive_IdleTimeout(t *testing.T) {
	t.Parallel()
	a, _ := newPipe(1178)
	_, err := bdx.Receive(context.Background(), bdx.NewMessenger(a, 20*time.Millisecond), bdx.ReceiveOptions{TransferFileDesignator: bdx.FileDesignator("f"), Sink: &memSink{}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if bdx.TransferIdleTimeout != 5*time.Minute {
		t.Fatal("BDX_TRANSFER_IDLE_TIMEOUT is five minutes")
	}
}

// TestReadBlockQuery_AckThenQuery mirrors BdxMessenger.readBlockQuery
// (:212-236), the sender-side reader kept for the message set.
func TestReadBlockQuery_AckThenQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, b := newPipe(1178)
	_ = b.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockAck), bdx.CounterOnly{BlockCounter: 3}.Encode())
	_ = b.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockQuery), bdx.CounterOnly{BlockCounter: 4}.Encode())
	m := bdx.NewMessenger(a, time.Second)
	if typ, v, err := m.ReadBlockQuery(ctx); err != nil || typ != bdx.MessageBlockQuery || v.(bdx.CounterOnly).BlockCounter != 4 {
		t.Fatalf("%v %v %v", typ, v, err)
	}
	_ = b.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockAck), bdx.CounterOnly{BlockCounter: 3}.Encode())
	_ = b.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockQueryWithSkip), bdx.BlockQueryWithSkip{BlockCounter: 9}.Encode())
	_, _, err := m.ReadBlockQuery(ctx)
	expectBdxError(t, err, bdx.StatusBadBlockCounter)
}

// TestFileDesignator mirrors FileDesignator.ts: the BDX URI form and the
// text form.
func TestFileDesignator(t *testing.T) {
	t.Parallel()
	fd, node, err := bdx.ParseBdxURI("bdx://00000000DEADBEEF/fff1-8001-v2.ota")
	if err != nil || node != 0xDEADBEEF || fd.Text() != "fff1-8001-v2.ota" {
		t.Fatalf("%q %x %v", fd, node, err)
	}
	if got := fd.BdxURI(0xDEADBEEF); got != "bdx://00000000DEADBEEF/fff1-8001-v2.ota" {
		t.Fatal(got)
	}
	for _, bad := range []string{"bdx://deadbeef/x", "bdx://00000000DEADBEEF/", "https://x/y", "bdx://00000000deadbeef/x"} {
		if _, _, err := bdx.ParseBdxURI(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got := bdx.FileDesignator([]byte{0x00, 0xff}).Text(); got != "0x00ff" {
		t.Fatal(got)
	}
}

// TestFileSink: commit renames into place; a failure leaves nothing.
func TestFileSink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "image.ota")
	s := bdx.NewFileSink(path)
	_, _ = s.Write([]byte("abc"))
	if s.Written() != 3 {
		t.Fatal(s.Written())
	}
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "abc" {
		t.Fatalf("%q", b)
	}
	failed := bdx.NewFileSink(filepath.Join(dir, "other.ota"))
	_, _ = failed.Write([]byte("x"))
	_ = failed.Close(errors.New("boom"))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("left behind %v", entries)
	}
	if _, err := failed.Write([]byte("y")); err == nil {
		t.Fatal("write after close")
	}
	empty := bdx.NewFileSink(filepath.Join(dir, "empty.ota"))
	if err := empty.Close(nil); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "empty.ota")); err != nil || fi.Size() != 0 {
		t.Fatal("empty transfer not committed")
	}
}
