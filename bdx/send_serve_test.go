// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// recordingEnd wraps a pipe end and records the message types it sends
// and receives, in order.
type recordingEnd struct {
	*pipeEnd
	mu   sync.Mutex
	sent []bdx.MessageType
}

func (r *recordingEnd) Send(ctx context.Context, protocolID uint16, opcode uint8, payload []byte) error {
	r.mu.Lock()
	if protocolID == bdx.ProtocolID {
		r.sent = append(r.sent, bdx.MessageType(opcode))
	} else {
		r.sent = append(r.sent, 0xFF) // a StatusReport
	}
	r.mu.Unlock()
	return r.pipeEnd.Send(ctx, protocolID, opcode, payload)
}

func (r *recordingEnd) types() []bdx.MessageType {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bdx.MessageType(nil), r.sent...)
}

// serveFirst runs bdx.Serve on end for the first message the peer sends,
// as the bridge's responder does with the message that opened an exchange.
func serveFirst(ctx context.Context, end bdx.Exchange, timeout time.Duration, opts bdx.ServeOptions) (bdx.Result, error) {
	first, err := end.Receive(ctx)
	if err != nil {
		return bdx.Result{}, err
	}
	init, err := bdx.DecodeInit(first.Payload)
	if err != nil {
		return bdx.Result{}, err
	}
	opts.InitType, opts.Init = bdx.MessageType(first.Opcode), init
	return bdx.Serve(ctx, bdx.NewMessenger(end, timeout), opts)
}

func blobOpener(b []byte) func(context.Context) (bdx.Blob, error) {
	return func(context.Context) (bdx.Blob, error) { return bytes.NewReader(b), nil }
}

func equalTypes(a, b []bdx.MessageType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSend_SenderDrive_DrivenSendingFlow: Send as the initiating sender,
// the peer serving the SendInit as the following receiver — SendInit,
// SendAccept, Block / BlockAck per block, BlockEof, BlockAckEof
// (flow/DrivenSendingFlow.ts:15-42 against flow/FollowingSendingFlow.ts).
// The SendInit carries the offered definite length
// (bdx-session-initiator.ts:126-153).
func TestSend_SenderDrive_DrivenSendingFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	ra, rb := &recordingEnd{pipeEnd: a}, &recordingEnd{pipeEnd: b}
	data := testData(2500)
	sink := &memSink{}
	served := make(chan error, 1)
	var finished uint64
	go func() {
		_, err := serveFirst(ctx, rb, 0, bdx.ServeOptions{
			TransferFileDesignator: bdx.FileDesignator("log/logs.txt"), Sink: sink,
			Finished: func(total uint64) { finished = total },
		})
		served <- err
	}()
	length := uint64(len(data))
	var progress []uint64
	res, err := bdx.Send(ctx, bdx.NewMessenger(ra, 0), bdx.SendOptions{
		TransferFileDesignator: bdx.FileDesignator("log/logs.txt"), Data: bytes.NewReader(data), Length: &length,
		PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive}, MaxBlockSize: 1024,
		Progress: func(n uint64, _ *uint64) { progress = append(progress, n) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatalf("receiver: %v", err)
	}
	if res.Transferred != 2500 || res.Mode != bdx.SenderDrive || res.BlockSize != 1024 || res.Length == nil || *res.Length != 2500 {
		t.Fatalf("result %+v", res)
	}
	if !bytes.Equal(sink.buf.Bytes(), data) || !sink.committed || finished != 2500 {
		t.Fatalf("received %d bytes, committed %v, finished %d", sink.buf.Len(), sink.committed, finished)
	}
	// Flow.processTransfer: 0, after each chunk but the last, after the last.
	if want := []uint64{0, 1024, 2048, 2500}; !equalUint64(progress, want) {
		t.Fatalf("progress %v, want %v", progress, want)
	}
	if want := []bdx.MessageType{bdx.MessageSendInit, bdx.MessageBlock, bdx.MessageBlock, bdx.MessageBlockEOF}; !equalTypes(ra.types(), want) {
		t.Fatalf("sender sent %v, want %v", ra.types(), want)
	}
	if want := []bdx.MessageType{bdx.MessageSendAccept, bdx.MessageBlockAck, bdx.MessageBlockAck, bdx.MessageBlockAckEOF}; !equalTypes(rb.types(), want) {
		t.Fatalf("receiver sent %v, want %v", rb.types(), want)
	}
}

func equalUint64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestServe_ReceiveInit_FollowingReceivingFlow: the requestor's
// receiver-driven download (Receive, ReceiverDrive) against Serve as the
// following sender — ReceiveInit, ReceiveAccept, BlockQuery / Block per
// block, BlockEof, BlockAckEof (flow/FollowingReceivingFlow.ts:21-55).
func TestServe_ReceiveInit_FollowingReceivingFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	ra, rb := &recordingEnd{pipeEnd: a}, &recordingEnd{pipeEnd: b}
	data := testData(3000)
	type served struct {
		res bdx.Result
		err error
	}
	done := make(chan served, 1)
	var negotiated bdx.Result
	go func() {
		res, err := serveFirst(ctx, rb, 0, bdx.ServeOptions{
			TransferFileDesignator: bdx.FileDesignator("ota/fff1-8001-v2.ota"), Open: blobOpener(data),
			PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive},
			Negotiated:           func(r bdx.Result) { negotiated = r },
		})
		done <- served{res, err}
	}()
	sink := &memSink{}
	res, err := bdx.Receive(ctx, bdx.NewMessenger(ra, 0), bdx.ReceiveOptions{
		TransferFileDesignator: bdx.FileDesignator("ota/fff1-8001-v2.ota"), Sink: sink,
		PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, MaxBlockSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := <-done
	if s.err != nil {
		t.Fatalf("sender: %v", s.err)
	}
	if !bytes.Equal(sink.buf.Bytes(), data) || res.Transferred != 3000 || s.res.Transferred != 3000 {
		t.Fatalf("received %d bytes (%+v), sender %+v", sink.buf.Len(), res, s.res)
	}
	// No definite length in the ReceiveInit, so none in the accept; the
	// sender's progress size is the blob's (OutboundFlow.ts:37-38).
	if res.Length != nil || negotiated.Length == nil || *negotiated.Length != 3000 || negotiated.Mode != bdx.ReceiverDrive {
		t.Fatalf("receiver length %v, sender negotiated %+v", res.Length, negotiated)
	}
	if want := []bdx.MessageType{bdx.MessageReceiveInit, bdx.MessageBlockQuery, bdx.MessageBlockQuery, bdx.MessageBlockQuery, bdx.MessageBlockAckEOF}; !equalTypes(ra.types(), want) {
		t.Fatalf("receiver sent %v, want %v", ra.types(), want)
	}
	if want := []bdx.MessageType{bdx.MessageReceiveAccept, bdx.MessageBlock, bdx.MessageBlock, bdx.MessageBlockEOF}; !equalTypes(rb.types(), want) {
		t.Fatalf("sender sent %v, want %v", rb.types(), want)
	}
}

// TestServe_ReceiveInit_SenderDrive: a ReceiveInit offering sender drive,
// served as the driving sender (DrivenSendingFlow) with the requested
// definite length; the receiver's BlockAck after each Block.
func TestServe_ReceiveInit_SenderDrive(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	data := testData(2048)
	done := make(chan error, 1)
	go func() {
		_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(data)})
		done <- err
	}()
	sink := &memSink{}
	res, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{
		TransferFileDesignator: bdx.FileDesignator("ota/f"), Sink: sink, MaxBlockSize: 1024,
	})
	if err != nil || <-done != nil {
		t.Fatalf("receive %v", err)
	}
	// An exact multiple of the block size: the last full block is not
	// known to be the last, so an empty BlockEof follows
	// (OutboundFlow.ts:73-81).
	if res.Mode != bdx.SenderDrive || !bytes.Equal(sink.buf.Bytes(), data) {
		t.Fatalf("result %+v, %d bytes", res, sink.buf.Len())
	}
}

// TestServe_SendInit_DrivingReceivingFlow: Send preferring receiver drive
// against Serve as the driving receiver — BlockQuery / Block per block
// (flow/DrivingReceivingFlow.ts against FollowingReceivingFlow on the
// sender's side).
func TestServe_SendInit_DrivingReceivingFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	data := testData(1500)
	sink := &memSink{}
	done := make(chan error, 1)
	go func() {
		_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{
			TransferFileDesignator: bdx.FileDesignator("log/x"), Sink: sink,
			PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive},
		})
		done <- err
	}()
	res, err := bdx.Send(ctx, bdx.NewMessenger(a, 0), bdx.SendOptions{TransferFileDesignator: bdx.FileDesignator("log/x"), Data: bytes.NewReader(data)})
	if err != nil || <-done != nil {
		t.Fatalf("send %v", err)
	}
	if res.Mode != bdx.ReceiverDrive || !bytes.Equal(sink.buf.Bytes(), data) || res.Transferred != 1500 {
		t.Fatalf("result %+v, %d bytes", res, sink.buf.Len())
	}
}

// TestServe_BlockQueryWithSkip: a driving receiver acknowledges a block
// and then skips (BlockAck, BlockQueryWithSkip): the sender skips the
// bytes and counts them as transferred (FollowingReceivingFlow.ts:25-41,
// BdxMessenger.readBlockQuery :212-236).
func TestServe_BlockQueryWithSkip(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	data := testData(300)
	done := make(chan error, 1)
	var total uint64
	go func() {
		_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{
			TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(data),
			PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive},
			Finished:             func(n uint64) { total = n },
		})
		done <- err
	}()
	m := bdx.NewMessenger(a, time.Second)
	if _, err := m.SendReceiveInit(ctx, bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 100, FileDesignator: []byte("ota/f")}); err != nil {
		t.Fatal(err)
	}
	_ = m.SendBlockQuery(ctx, 0)
	if _, blk, err := m.ReadBlock(ctx); err != nil || !bytes.Equal(blk.Data, data[:100]) {
		t.Fatalf("block 0: %v", err)
	}
	_ = m.SendBlockAck(ctx, 0)
	_ = m.SendBlockQueryWithSkip(ctx, bdx.BlockQueryWithSkip{BlockCounter: 1, BytesToSkip: 50})
	typ, blk, err := m.ReadBlock(ctx)
	if err != nil || typ != bdx.MessageBlock || !bytes.Equal(blk.Data, data[150:250]) {
		t.Fatalf("block 1 after the skip: %s %v", typ, err)
	}
	_ = m.SendBlockQuery(ctx, 2)
	typ, blk, err = m.ReadBlock(ctx)
	if err != nil || typ != bdx.MessageBlockEOF || !bytes.Equal(blk.Data, data[250:]) {
		t.Fatalf("last block: %s %v", typ, err)
	}
	_ = m.SendBlockAckEOF(ctx, 2)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if total != 300 {
		t.Fatalf("transferred %d, want 300 (the skip counts)", total)
	}
}

// TestServe_BlockSizeNegotiation: the accepted block size is the Init's,
// capped to the transport (bdx-session-initiator.ts:20-27) and to the
// configured maximum (:200-203).
func TestServe_BlockSizeNegotiation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		transport int
		offered   uint16
		maxBlock  uint16
		want      uint16
	}{
		{"offered", 1178, 1024, 0, 1024},
		{"configured maximum", 1178, 1024, 500, 500},
		{"transport", 300, 1024, 0, 296},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a, b := newPipe(tc.transport)
			go func() {
				_, _ = serveFirst(ctx, b, 0, bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(10)), MaxBlockSize: tc.maxBlock})
			}()
			acc, err := bdx.NewMessenger(a, time.Second).SendReceiveInit(ctx, bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: tc.offered, FileDesignator: []byte("ota/f")})
			if err != nil || acc.MaxBlockSize != tc.want {
				t.Fatalf("accepted %d, want %d (%v)", acc.MaxBlockSize, tc.want, err)
			}
		})
	}
}

// expectStatus reads the StatusReport the other side sent.
func expectStatus(t *testing.T, end *pipeEnd, code bdx.StatusCode) {
	t.Helper()
	for {
		select {
		case m := <-end.in:
			if m.ProtocolID != bdx.SecureChannelProtocolID {
				continue
			}
			sr, err := bdx.DecodeStatusReport(m.Payload)
			if err != nil || sr.GeneralStatus != bdx.GeneralFailure || sr.ProtocolStatus != code {
				t.Fatalf("StatusReport %+v, %v, want %s", sr, err, code)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatalf("no StatusReport %s", code)
		}
	}
}

// TestServe_Refusals: what the responder refuses before it accepts, each
// with a StatusReport (bdx-session-initiator.ts:59-98, :186-196).
func TestServe_Refusals(t *testing.T) {
	t.Parallel()
	notThere := func(context.Context) (bdx.Blob, error) { return nil, fs.ErrNotExist }
	for _, tc := range []struct {
		name string
		init bdx.Init
		opts bdx.ServeOptions
		code bdx.StatusCode
	}{
		{
			"designator mismatch",
			bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, FileDesignator: []byte("ota/other")},
			bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(10))},
			bdx.StatusFileDesignatorUnknown,
		},
		{
			"no such file",
			bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, FileDesignator: []byte("ota/f")},
			bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: notThere},
			bdx.StatusFileDesignatorUnknown,
		},
		{
			"mode",
			bdx.Init{TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 64, FileDesignator: []byte("ota/f")},
			bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(10)), PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}},
			bdx.StatusTransferMethodNotSupported,
		},
		{
			"length beyond the file",
			bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, MaxLength: u64(11), FileDesignator: []byte("ota/f")},
			bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(10))},
			bdx.StatusLengthTooLarge,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a, b := newPipe(1178)
			opts := tc.opts
			opts.InitType, opts.Init = bdx.MessageReceiveInit, tc.init
			_, err := bdx.Serve(ctx, bdx.NewMessenger(b, 0), opts)
			expectBdxError(t, err, tc.code)
			expectStatus(t, a, tc.code)
		})
	}
}

// TestServe_AbortedTransfers: a BlockQuery after the BlockEof — where the
// sender waits for BlockAckEof — is an unexpected message and ends the
// transfer without a StatusReport (BdxMessenger.ts:95-98); a StatusReport
// from the receiver mid-transfer ends it with the receiver's error
// (:274-296), and both cancel the flow (Flow.ts:143-145).
func TestServe_AbortedTransfers(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, afterFirst func(ctx context.Context, m *bdx.Messenger)) (error, bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a, b := newPipe(1178)
		cancelled := false
		done := make(chan error, 1)
		go func() {
			_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{
				TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(50)),
				PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, Cancelled: func() { cancelled = true },
			})
			done <- err
		}()
		m := bdx.NewMessenger(a, time.Second)
		if _, err := m.SendReceiveInit(ctx, bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/f")}); err != nil {
			t.Fatal(err)
		}
		afterFirst(ctx, m)
		return <-done, cancelled
	}
	err, cancelled := run(t, func(ctx context.Context, m *bdx.Messenger) {
		_ = m.SendBlockQuery(ctx, 0)
		if typ, _, err := m.ReadBlock(ctx); err != nil || typ != bdx.MessageBlockEOF {
			t.Fatalf("expected the whole file as BlockEof: %s %v", typ, err)
		}
		_ = m.SendBlockQuery(ctx, 1)
	})
	if !errors.Is(err, bdx.ErrUnexpectedMessage) || !cancelled {
		t.Fatalf("BlockQuery after BlockEof: %v, cancelled %v", err, cancelled)
	}
	err, cancelled = run(t, func(ctx context.Context, m *bdx.Messenger) {
		_ = m.SendError(ctx, bdx.StatusLengthTooLarge)
	})
	var sre *bdx.StatusResponseError
	if !errors.As(err, &sre) || sre.ProtocolStatus != bdx.StatusLengthTooLarge || !cancelled {
		t.Fatalf("StatusReport: %v, cancelled %v", err, cancelled)
	}
}

// TestServe_IdleTimeout: the responder's messenger gives up after its
// message timeout — the parameter the OTA provider sets to five minutes
// (OtaSoftwareUpdateProviderServer.ts:296, BdxProtocol.ts:139-144).
func TestServe_IdleTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	done := make(chan error, 1)
	go func() {
		_, err := serveFirst(ctx, b, 30*time.Millisecond, bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: blobOpener(testData(5000)), PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}})
		done <- err
	}()
	if _, err := bdx.NewMessenger(a, time.Second).SendReceiveInit(ctx, bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/f")}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the idle timeout", err)
	}
}

// TestStartSend_SplitsAtTheAccept: StartSend returns once the SendAccept
// arrived and before any block went out — the point at which chip's
// DiagnosticLogs server answers its command (BDXDiagnosticLogsProvider.cpp:
// 167-174); an indefinite length is offered as chip offers it (:72-76).
func TestStartSend_SplitsAtTheAccept(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := newPipe(1178)
	rb := &recordingEnd{pipeEnd: b}
	sink := &memSink{}
	done := make(chan error, 1)
	var gotInit bdx.Init
	go func() {
		first, err := rb.Receive(ctx)
		if err != nil {
			done <- err
			return
		}
		gotInit, _ = bdx.DecodeInit(first.Payload)
		_, err = bdx.Serve(ctx, bdx.NewMessenger(rb, 0), bdx.ServeOptions{InitType: bdx.MessageSendInit, Init: gotInit, TransferFileDesignator: bdx.FileDesignator("logs.txt"), Sink: sink})
		done <- err
	}()
	data := testData(1100)
	tr, err := bdx.StartSend(ctx, bdx.NewMessenger(a, 0), bdx.SendOptions{
		TransferFileDesignator: bdx.FileDesignator("logs.txt"), Data: bytes.NewReader(data),
		PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive}, MaxBlockSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotInit.MaxLength != nil || !gotInit.TransferControl.SenderDrive || gotInit.TransferControl.ReceiverDrive || gotInit.MaxBlockSize != 1024 {
		t.Fatalf("SendInit %+v", gotInit)
	}
	if got := rb.types(); !equalTypes(got, []bdx.MessageType{bdx.MessageSendAccept}) {
		t.Fatalf("before Run the receiver sent %v", got)
	}
	if tr.Result().BlockSize != 1024 {
		t.Fatalf("negotiated %+v", tr.Result())
	}
	res, err := tr.Run(ctx)
	if err != nil || <-done != nil || res.Transferred != 1100 || !bytes.Equal(sink.buf.Bytes(), data) {
		t.Fatalf("run %+v %v", res, err)
	}
}
