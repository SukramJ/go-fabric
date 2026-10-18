// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

// A port of matter.js packages/protocol/test/bdx/BdxTest.ts at 862ad75f:
// whole transfers between an initiator (Send / Receive, matter.js
// BdxClient.asSender / asReceiver) and a [bdx.Responder] serving the "ota"
// scope (BdxProtocol.onNewExchange), every message both sides sent
// recorded before an optional manipulation, as bdx-helpers.ts:85-204
// (bdxTransfer) does. The matter.js PersistedFileDesignator of the client
// is the Data / Sink of the Go initiator; its transferFileDesignator is
// TransferFileDesignator.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// bdxTestPayload is the mock exchange's payload size: ProtocolMocks'
// channel of 1024 bytes (bdx-helpers.ts:238) less MATTER_MESSAGE_OVERHEAD,
// 54 bytes (MessageExchange.ts:167, :449-453) — blocks of 966 bytes.
const bdxTestPayload = 970

// bdxRandom is crypto.randomBytes, seeded so the test is deterministic.
func bdxRandom(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{42}).Read(b)
	return b
}

// bdxRecord renders one message a side sent, every field matter.js's
// deep.equals compares.
func bdxRecord(protocolID uint16, opcode uint8, payload []byte) (text string, msg any) {
	if protocolID != bdx.ProtocolID {
		sr, err := bdx.DecodeStatusReport(payload)
		if err != nil {
			return "StatusReport undecodable", nil
		}
		return fmt.Sprintf("StatusReport general=%d status=%s", sr.GeneralStatus, sr.ProtocolStatus), sr
	}
	t := bdx.MessageType(opcode)
	v, err := bdx.Decode(t, payload)
	if err != nil {
		return t.String() + " undecodable", nil
	}
	switch m := v.(type) {
	case bdx.Init:
		return fmt.Sprintf("%s %s max=%d fd=%s len=%s off=%s meta=%s", t, bdxTC(m.TransferControl), m.MaxBlockSize,
			m.FileDesignator, bdxOpt(m.MaxLength), bdxOpt(m.StartOffset), bdxMeta(m.MetaData)), m
	case bdx.SendAccept:
		return fmt.Sprintf("%s %s max=%d meta=%s", t, bdxTC(m.TransferControl), m.MaxBlockSize, bdxMeta(m.MetaData)), m
	case bdx.ReceiveAccept:
		return fmt.Sprintf("%s %s max=%d len=%s meta=%s", t, bdxTC(m.TransferControl), m.MaxBlockSize, bdxOpt(m.Length), bdxMeta(m.MetaData)), m
	case bdx.Block:
		return fmt.Sprintf("%s cnt=%d len=%d", t, m.BlockCounter, len(m.Data)), m
	case bdx.CounterOnly:
		return fmt.Sprintf("%s cnt=%d", t, m.BlockCounter), m
	case bdx.BlockQueryWithSkip:
		return fmt.Sprintf("%s cnt=%d skip=%d", t, m.BlockCounter, m.BytesToSkip), m
	}
	return t.String(), v
}

func bdxTC(tc bdx.TransferControl) string {
	drive := ""
	if tc.SenderDrive {
		drive += "S"
	}
	if tc.ReceiverDrive {
		drive += "R"
	}
	return fmt.Sprintf("v=%d drive=%s async=%t", tc.Version, drive, tc.AsynchronousTransfer)
}

func bdxOpt(v *uint64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatUint(*v, 10)
}

func bdxMeta(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}

// The renderings of the messages the matter.js cases compare; "-" is
// undefined, drive S/R/SR the senderDrive / receiverDrive bits.
func wantInit(t bdx.MessageType, drive, fd, length, off string) string {
	return fmt.Sprintf("%s v=0 drive=%s async=false max=966 fd=%s len=%s off=%s meta=-", t, drive, fd, length, off)
}

func wantSendAccept(drive string) string {
	return fmt.Sprintf("SendAccept v=0 drive=%s async=false max=966 meta=-", drive)
}

func wantReceiveAccept(drive string) string {
	return fmt.Sprintf("ReceiveAccept v=0 drive=%s async=false max=966 len=- meta=-", drive)
}

func wantBlock(t bdx.MessageType, cnt, n int) string {
	return fmt.Sprintf("%s cnt=%d len=%d", t, cnt, n)
}

func wantCounter(t bdx.MessageType, cnt int) string { return fmt.Sprintf("%s cnt=%d", t, cnt) }

func wantStatus(code bdx.StatusCode) string {
	return fmt.Sprintf("StatusReport general=1 status=%s", code)
}

type bdxRec struct {
	text string
	msg  any
}

// tapEnd records what its side sends — before manipulate rewrites it,
// as bdx-helpers.ts:134-154 records before the manipulators run.
type tapEnd struct {
	*pipeEnd
	manipulate func(opcode uint8, payload []byte) (uint8, []byte)

	mu   sync.Mutex
	recs []bdxRec
}

func (e *tapEnd) Send(ctx context.Context, protocolID uint16, opcode uint8, payload []byte) error {
	text, msg := bdxRecord(protocolID, opcode, payload)
	e.mu.Lock()
	e.recs = append(e.recs, bdxRec{text: text, msg: msg})
	e.mu.Unlock()
	if e.manipulate != nil && protocolID == bdx.ProtocolID {
		opcode, payload = e.manipulate(opcode, payload)
	}
	return e.pipeEnd.Send(ctx, protocolID, opcode, payload)
}

func (e *tapEnd) records() []bdxRec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]bdxRec(nil), e.recs...)
}

// memBlobs is the server's ScopedStorage over a MemoryBlobStorageDriver: a
// [bdx.Storage] and [bdx.SinkStorage] that stores a file only once its
// transfer committed.
type memBlobs struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func (s *memBlobs) Open(_ context.Context, name string) (bdx.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(b), nil
}

func (s *memBlobs) Create(_ context.Context, name string) (bdx.Sink, error) {
	return &memBlobSink{store: s, name: name}, nil
}

func (s *memBlobs) get(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[name]
	return b, ok
}

type memBlobSink struct {
	store *memBlobs
	name  string
	buf   bytes.Buffer
}

func (k *memBlobSink) Write(p []byte) (int, error) { return k.buf.Write(p) }

func (k *memBlobSink) Close(err error) error {
	if err == nil {
		k.store.mu.Lock()
		k.store.blobs[k.name] = bytes.Clone(k.buf.Bytes())
		k.store.mu.Unlock()
	}
	return nil
}

// bdxCase is one bdxTransfer: the client's role and options, the server's
// limits and stored files, the manipulators and the responder observer.
type bdxCase struct {
	sender       bool // BdxClient.asSender, else asReceiver
	data         []byte
	fd           string // the transfer file designator; "ota/data" when empty
	clientModes  []bdx.DriverMode
	serverModes  []bdx.DriverMode
	maxBlockSize uint16
	// length and startOffset are senderMaxLength / senderStartOffset; the
	// Go sender reads a stream the caller cuts to them.
	length, startOffset *uint64
	serverBlobs         map[string][]byte
	clientManipulate    func(opcode uint8, payload []byte) (uint8, []byte)
	serverManipulate    func(opcode uint8, payload []byte) (uint8, []byte)
	observe             func(*bdx.ServerSession)
}

type bdxRun struct {
	client, server []bdxRec
	clientErr      error
	clientSink     *memSink // what a receiving client stored
	serverStore    *memBlobs
}

func runBdxTransfer(t *testing.T, c bdxCase) bdxRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a, b := newPipe(bdxTestPayload)
	clientEnd := &tapEnd{pipeEnd: a, manipulate: c.clientManipulate}
	serverEnd := &tapEnd{pipeEnd: b, manipulate: c.serverManipulate}
	store := &memBlobs{blobs: map[string][]byte{}}
	for name, blob := range c.serverBlobs {
		store.blobs[name] = blob
	}

	resp := bdx.NewResponder(slog.New(slog.DiscardHandler))
	peer := bdx.Peer{FabricIndex: 1, NodeID: 1, SessionID: 1}
	resp.EnablePeerForScope(peer, "ota", store, bdx.ScopeConfig{PreferredDriverModes: c.serverModes})
	if c.observe != nil {
		resp.OnSessionStarted(func(s *bdx.ServerSession, _ string) { c.observe(s) })
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		first, err := serverEnd.Receive(ctx)
		if err == nil {
			resp.HandleExchange(ctx, peer, first, serverEnd)
		}
	}()

	fd := c.fd
	if fd == "" {
		fd = "ota/data"
	}
	run := bdxRun{serverStore: store}
	m := bdx.NewMessenger(clientEnd, 0)
	if c.sender {
		var start uint64
		if c.startOffset != nil {
			start = *c.startOffset
		}
		n := uint64(len(c.data)) - start
		if c.length != nil {
			n = *c.length
		}
		_, run.clientErr = bdx.Send(ctx, m, bdx.SendOptions{
			TransferFileDesignator: bdx.FileDesignator(fd),
			Data:                   io.NewSectionReader(bytes.NewReader(c.data), int64(start), int64(n)), //nolint:gosec // test sizes
			Length:                 &n, StartOffset: c.startOffset,
			PreferredDriverModes: c.clientModes, MaxBlockSize: c.maxBlockSize,
		})
	} else {
		run.clientSink = &memSink{}
		_, run.clientErr = bdx.Receive(ctx, m, bdx.ReceiveOptions{
			TransferFileDesignator: bdx.FileDesignator(fd), Sink: run.clientSink,
			PreferredDriverModes: c.clientModes, MaxBlockSize: c.maxBlockSize,
		})
	}
	select {
	case <-served:
	case <-ctx.Done():
		t.Fatal("the responder did not finish")
	}
	run.client, run.server = clientEnd.records(), serverEnd.records()
	return run
}

// expectRecords compares the rendered messages; "" is a message the
// matter.js case counts but does not inspect.
func expectRecords(t *testing.T, side string, got []bdxRec, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s sent %d messages %v, want %d", side, len(got), got, len(want))
	}
	for i, w := range want {
		if w != "" && got[i].text != w {
			t.Errorf("%s[%d] = %q\n want %q", side, i, got[i].text, w)
		}
	}
}

func expectStored(t *testing.T, store *memBlobs, name string, want []byte) {
	t.Helper()
	if got, ok := store.get(name); !ok || !bytes.Equal(got, want) {
		t.Fatalf("server stored %d bytes under %q (present %v), want %d", len(got), name, ok, len(want))
	}
}

func expectReceived(t *testing.T, sink *memSink, want []byte) {
	t.Helper()
	if !sink.committed || !bytes.Equal(sink.buf.Bytes(), want) {
		t.Fatalf("client stored %d bytes (committed %v), want %d", sink.buf.Len(), sink.committed, len(want))
	}
}

func expectPeerStatus(t *testing.T, err error, code bdx.StatusCode) {
	t.Helper()
	var sre *bdx.StatusResponseError
	if !errors.As(err, &sre) || sre.ProtocolStatus != code {
		t.Fatalf("client error %v, want a %s StatusReport from the peer", err, code)
	}
}

const (
	sendInit    = bdx.MessageSendInit
	receiveInit = bdx.MessageReceiveInit
	block       = bdx.MessageBlock
	blockEOF    = bdx.MessageBlockEOF
	blockAck    = bdx.MessageBlockAck
	blockAckEOF = bdx.MessageBlockAckEOF
	blockQuery  = bdx.MessageBlockQuery
)

var (
	senderDriveOnly   = []bdx.DriverMode{bdx.SenderDrive}
	receiverDriveOnly = []bdx.DriverMode{bdx.ReceiverDrive}
)

// rewriteInit decodes an *Init payload, applies fn and encodes it again.
func rewriteInit(t *testing.T, kind bdx.MessageType, fn func(*bdx.Init)) func(uint8, []byte) (uint8, []byte) {
	t.Helper()
	return func(opcode uint8, payload []byte) (uint8, []byte) {
		if bdx.MessageType(opcode) != kind {
			return opcode, payload
		}
		init, err := bdx.DecodeInit(payload)
		if err != nil {
			t.Error(err)
			return opcode, payload
		}
		fn(&init)
		out, err := init.Encode()
		if err != nil {
			t.Error(err)
		}
		return opcode, out
	}
}

func TestBdxSuccessfulTransfers(t *testing.T) {
	t.Run("2048bytes (3 packages)", func(t *testing.T) {
		t.Run("Using SendInit as Sender-Driver (no limits)", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data})
			expectRecords(t, "client", r.client, []string{
				wantInit(sendInit, "SR", "ota/data", "2048", "-"),
				wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 116),
			})
			expectRecords(t, "server", r.server, []string{
				wantSendAccept("S"),
				wantCounter(blockAck, 0), wantCounter(blockAck, 1), wantCounter(blockAckEOF, 2),
			})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using SendInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, clientModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{
				wantInit(sendInit, "R", "ota/data", "2048", "-"),
				wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 116),
			})
			expectRecords(t, "server", r.server, []string{
				wantSendAccept("R"),
				wantCounter(blockQuery, 0), wantCounter(blockQuery, 1), wantCounter(blockQuery, 2), wantCounter(blockAckEOF, 2),
			})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using SendInit as Receiver-Driver (by Responder and custom transfer designator)", func(t *testing.T) {
			// The client's file "data2" goes out under the transfer designator "ota/data".
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, serverModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "SR", "ota/data", "2048", "-"), "", "", ""})
			expectRecords(t, "server", r.server, []string{wantSendAccept("R"), "", "", "", ""})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using ReceiveInit as Sender-Driver (no limits and custom transfer File Designator)", func(t *testing.T) {
			// The server's "data2" is stored by the client as "data".
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{data: data, fd: "ota/data2", serverBlobs: map[string][]byte{"data2": data}})
			expectRecords(t, "client", r.client, []string{
				wantInit(receiveInit, "SR", "ota/data2", "-", "-"),
				wantCounter(blockAck, 0), wantCounter(blockAck, 1), wantCounter(blockAckEOF, 2),
			})
			expectRecords(t, "server", r.server, []string{
				wantReceiveAccept("S"),
				wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 116),
			})
			expectReceived(t, r.clientSink, data)
		})

		t.Run("Caps a peer-proposed maxBlockSize larger than the transport payload size", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{
				data: data, serverBlobs: map[string][]byte{"data": data},
				clientManipulate: rewriteInit(t, receiveInit, func(in *bdx.Init) { in.MaxBlockSize = 4096 }),
			})
			if a, ok := r.server[0].msg.(bdx.ReceiveAccept); !ok || a.MaxBlockSize != 966 {
				t.Fatalf("server[0] = %s, want a ReceiveAccept of 966", r.server[0].text)
			}
			if b, ok := r.server[1].msg.(bdx.Block); !ok || len(b.Data) != 966 {
				t.Fatalf("server[1] = %s, want a Block of 966", r.server[1].text)
			}
			expectReceived(t, r.clientSink, data)
			if last := r.client[len(r.client)-1].text; last != wantCounter(blockAckEOF, 2) {
				t.Fatalf("client's last message %s, want a BlockAckEof", last)
			}
		})

		t.Run("Caps a peer-accepted maxBlockSize larger than the transport payload size", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{
				sender: true, data: data,
				serverManipulate: func(opcode uint8, payload []byte) (uint8, []byte) {
					if bdx.MessageType(opcode) != bdx.MessageSendAccept {
						return opcode, payload
					}
					accept, err := bdx.DecodeSendAccept(payload)
					if err != nil {
						t.Error(err)
						return opcode, payload
					}
					accept.MaxBlockSize = 4096
					out, _ := accept.Encode()
					return opcode, out
				},
			})
			if b, ok := r.client[1].msg.(bdx.Block); !ok || len(b.Data) != 966 {
				t.Fatalf("client[1] = %s, want a Block of 966", r.client[1].text)
			}
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using ReceiveInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{data: data, clientModes: receiverDriveOnly, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{
				wantInit(receiveInit, "R", "ota/data", "-", "-"),
				wantCounter(blockQuery, 0), wantCounter(blockQuery, 1), wantCounter(blockQuery, 2), wantCounter(blockAckEOF, 2),
			})
			expectRecords(t, "server", r.server, []string{
				wantReceiveAccept("R"),
				wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 116),
			})
			expectReceived(t, r.clientSink, data)
		})

		t.Run("Using ReceiveInit as Receiver-Driver (by Responder)", func(t *testing.T) {
			data := bdxRandom(2048)
			r := runBdxTransfer(t, bdxCase{data: data, serverModes: receiverDriveOnly, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{wantInit(receiveInit, "SR", "ota/data", "-", "-"), "", "", "", ""})
			expectRecords(t, "server", r.server, []string{wantReceiveAccept("R"), "", "", ""})
			expectReceived(t, r.clientSink, data)
		})
	})

	t.Run("1932bytes (exactly 2 packages)", func(t *testing.T) {
		t.Run("Using SendInit as Sender-Driver (no limits)", func(t *testing.T) {
			data := bdxRandom(1932)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data})
			expectRecords(t, "client", r.client, []string{
				wantInit(sendInit, "SR", "ota/data", "1932", "-"), wantBlock(block, 0, 966), wantBlock(blockEOF, 1, 966),
			})
			expectRecords(t, "server", r.server, []string{wantSendAccept("S"), wantCounter(blockAck, 0), wantCounter(blockAckEOF, 1)})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using SendInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(1932)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, clientModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{
				wantInit(sendInit, "R", "ota/data", "1932", "-"), wantBlock(block, 0, 966), wantBlock(blockEOF, 1, 966),
			})
			expectRecords(t, "server", r.server, []string{
				wantSendAccept("R"), wantCounter(blockQuery, 0), wantCounter(blockQuery, 1), wantCounter(blockAckEOF, 1),
			})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using ReceiveInit as Sender-Driver (no limits)", func(t *testing.T) {
			// No length is negotiated, so the server learns the end only
			// after the second full block: an empty BlockEof.
			data := bdxRandom(1932)
			r := runBdxTransfer(t, bdxCase{data: data, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{
				wantInit(receiveInit, "SR", "ota/data", "-", "-"),
				wantCounter(blockAck, 0), wantCounter(blockAck, 1), wantCounter(blockAckEOF, 2),
			})
			expectRecords(t, "server", r.server, []string{
				wantReceiveAccept("S"), wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 0),
			})
			expectReceived(t, r.clientSink, data)
		})

		t.Run("Using ReceiveInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(1932)
			r := runBdxTransfer(t, bdxCase{data: data, clientModes: receiverDriveOnly, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{
				wantInit(receiveInit, "R", "ota/data", "-", "-"),
				wantCounter(blockQuery, 0), wantCounter(blockQuery, 1), wantCounter(blockQuery, 2), wantCounter(blockAckEOF, 2),
			})
			expectRecords(t, "server", r.server, []string{
				wantReceiveAccept("R"), wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantBlock(blockEOF, 2, 0),
			})
			expectReceived(t, r.clientSink, data)
		})
	})

	t.Run("256bytes (1 package)", func(t *testing.T) {
		t.Run("Using SendInit as Sender-Driver (no limits)", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "SR", "ota/data", "256", "-"), wantBlock(blockEOF, 0, 256)})
			expectRecords(t, "server", r.server, []string{wantSendAccept("S"), wantCounter(blockAckEOF, 0)})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using SendInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, clientModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "R", "ota/data", "256", "-"), wantBlock(blockEOF, 0, 256)})
			expectRecords(t, "server", r.server, []string{wantSendAccept("R"), wantCounter(blockQuery, 0), wantCounter(blockAckEOF, 0)})
			expectStored(t, r.serverStore, "data", data)
		})

		t.Run("Using ReceiveInit as Sender-Driver (no limits)", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{data: data, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{wantInit(receiveInit, "SR", "ota/data", "-", "-"), wantCounter(blockAckEOF, 0)})
			expectRecords(t, "server", r.server, []string{wantReceiveAccept("S"), wantBlock(blockEOF, 0, 256)})
			expectReceived(t, r.clientSink, data)
		})

		t.Run("Using ReceiveInit as Receiver-Driver (by Initiator)", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{data: data, clientModes: receiverDriveOnly, serverBlobs: map[string][]byte{"data": data}})
			expectRecords(t, "client", r.client, []string{
				wantInit(receiveInit, "R", "ota/data", "-", "-"), wantCounter(blockQuery, 0), wantCounter(blockAckEOF, 0),
			})
			expectRecords(t, "server", r.server, []string{wantReceiveAccept("R"), wantBlock(blockEOF, 0, 256)})
			expectReceived(t, r.clientSink, data)
		})
	})

	t.Run("partial transfers", func(t *testing.T) {
		t.Run("Send first 100 bytes as sender drive", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, length: u64(100)})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "SR", "ota/data", "100", "-"), wantBlock(blockEOF, 0, 100)})
			expectRecords(t, "server", r.server, []string{"", ""})
			expectStored(t, r.serverStore, "data", data[:100])
		})

		t.Run("Send first 100 bytes as Receiver-Driver", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, length: u64(100), clientModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "R", "ota/data", "100", "-"), wantBlock(blockEOF, 0, 100)})
			expectRecords(t, "server", r.server, []string{wantSendAccept("R"), wantCounter(blockQuery, 0), wantCounter(blockAckEOF, 0)})
			expectStored(t, r.serverStore, "data", data[:100])
		})

		t.Run("Send 100 bytes with startOffset 50 as sender drive", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, length: u64(100), startOffset: u64(50)})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "SR", "ota/data", "100", "50"), wantBlock(blockEOF, 0, 100)})
			expectRecords(t, "server", r.server, []string{"", ""})
			expectStored(t, r.serverStore, "data", data[50:150])
		})

		t.Run("Send first 100 bytes with startOffset 50 as Receiver-Driver", func(t *testing.T) {
			data := bdxRandom(256)
			r := runBdxTransfer(t, bdxCase{sender: true, data: data, length: u64(100), startOffset: u64(50), clientModes: receiverDriveOnly})
			expectRecords(t, "client", r.client, []string{wantInit(sendInit, "R", "ota/data", "100", "50"), wantBlock(blockEOF, 0, 100)})
			expectRecords(t, "server", r.server, []string{wantSendAccept("R"), wantCounter(blockQuery, 0), wantCounter(blockAckEOF, 0)})
			expectStored(t, r.serverStore, "data", data[50:150])
		})
	})
}

// TestBdxPerBlockDebugLogging ports the one case of the group that is not
// about log text: what a responder session reports. The Go Init and
// Result are values, so an observer altering its copy cannot reach the
// session's.
func TestBdxPerBlockDebugLogging(t *testing.T) {
	const blockSize, tailSize = 500, 116 // BdxTest.ts:1307-1308

	t.Run("a responder reports what it received and granted, and an observer cannot alter either", func(t *testing.T) {
		var responder *bdx.ServerSession
		var announced bdx.Init
		runBdxTransfer(t, bdxCase{
			sender: true, data: bdxRandom(2*blockSize + tailSize), maxBlockSize: blockSize,
			// Announced before it negotiates (BdxTest.ts:1374-1379).
			observe: func(s *bdx.ServerSession) {
				responder = s
				announced = s.Init()
			},
		})
		if responder == nil {
			t.Fatal("no session announced")
		}
		if announced.MaxBlockSize != blockSize {
			t.Fatalf("announced maxBlockSize %d, want %d", announced.MaxBlockSize, blockSize)
		}
		announced.MaxBlockSize = 1
		announced.FileDesignator[0] = 'x'
		announced.TransferControl.SenderDrive = false
		if in := responder.Init(); in.MaxBlockSize != blockSize || string(in.FileDesignator) != "ota/data" || !in.TransferControl.SenderDrive {
			t.Fatalf("the session's Init changed with the observer's copy: %+v", in)
		}

		// Result carries no file designator (BdxTest.ts:1388).
		negotiated, ok := responder.Negotiated()
		if !ok || negotiated.Mode != bdx.SenderDrive || negotiated.BlockSize != blockSize || negotiated.StartOffset != 0 {
			t.Fatalf("negotiated %+v (%v)", negotiated, ok)
		}
		negotiated.BlockSize = 1
		if again, _ := responder.Negotiated(); again.BlockSize != blockSize {
			t.Fatalf("the session's parameters changed with the observer's copy: %+v", again)
		}
	})
}

func TestBdxErrorCases(t *testing.T) {
	t.Run("No matching driver modes", func(t *testing.T) {
		r := runBdxTransfer(t, bdxCase{sender: true, data: bdxRandom(256), clientModes: senderDriveOnly, serverModes: receiverDriveOnly})
		expectPeerStatus(t, r.clientErr, bdx.StatusTransferMethodNotSupported)
		if r.server[0].text != wantStatus(bdx.StatusTransferMethodNotSupported) {
			t.Fatalf("server[0] = %s", r.server[0].text)
		}
		if _, ok := r.serverStore.get("data"); ok {
			t.Fatal("the server stored the refused file")
		}
	})

	t.Run("Unknown FileDesignator", func(t *testing.T) {
		r := runBdxTransfer(t, bdxCase{})
		expectPeerStatus(t, r.clientErr, bdx.StatusFileDesignatorUnknown)
		if r.server[0].text != wantStatus(bdx.StatusFileDesignatorUnknown) {
			t.Fatalf("server[0] = %s", r.server[0].text)
		}
	})

	t.Run("Request a file from peer that is larger than the data", func(t *testing.T) {
		data := bdxRandom(100)
		r := runBdxTransfer(t, bdxCase{
			data: data, serverBlobs: map[string][]byte{"data": data},
			clientManipulate: rewriteInit(t, receiveInit, func(in *bdx.Init) { in.MaxLength = u64(300) }),
		})
		expectPeerStatus(t, r.clientErr, bdx.StatusLengthTooLarge)
		if r.clientSink.committed {
			t.Fatal("the client stored the refused file")
		}
	})

	t.Run("Request a file from peer that is larger than the data using startOffset", func(t *testing.T) {
		data := bdxRandom(100)
		r := runBdxTransfer(t, bdxCase{
			data: data, serverBlobs: map[string][]byte{"data": data},
			clientManipulate: rewriteInit(t, receiveInit, func(in *bdx.Init) { in.MaxLength, in.StartOffset = u64(100), u64(50) }),
		})
		expectPeerStatus(t, r.clientErr, bdx.StatusLengthTooLarge)
		if r.clientSink.committed {
			t.Fatal("the client stored the refused file")
		}
	})

	t.Run("Unexpected Blockcounter in request with SenderDriver", func(t *testing.T) {
		r := runBdxTransfer(t, bdxCase{
			sender: true, data: bdxRandom(256),
			clientManipulate: func(opcode uint8, payload []byte) (uint8, []byte) {
				if bdx.MessageType(opcode) != blockEOF {
					return opcode, payload
				}
				b, err := bdx.DecodeBlock(payload)
				if err != nil {
					t.Error(err)
					return opcode, payload
				}
				b.BlockCounter++
				return opcode, b.Encode()
			},
		})
		expectPeerStatus(t, r.clientErr, bdx.StatusBadBlockCounter)
		if len(r.server) < 2 || r.server[1].text != wantStatus(bdx.StatusBadBlockCounter) {
			t.Fatalf("server sent %v, want a BadBlockCounter StatusReport second", r.server)
		}
		if _, ok := r.serverStore.get("data"); ok {
			t.Fatal("the server stored the failed file")
		}
	})

	t.Run("BlockQueryWithSkip skipping bytes but end errors because of too less data", func(t *testing.T) {
		data := bdxRandom(2048)
		r := runBdxTransfer(t, bdxCase{
			sender: true, data: data, clientModes: receiverDriveOnly,
			// The second BlockQuery becomes a BlockQueryWithSkip of 100 bytes.
			serverManipulate: func(opcode uint8, payload []byte) (uint8, []byte) {
				if bdx.MessageType(opcode) != blockQuery {
					return opcode, payload
				}
				q, err := bdx.DecodeCounterOnly(payload)
				if err != nil || q.BlockCounter != 1 {
					return opcode, payload
				}
				return uint8(bdx.MessageBlockQueryWithSkip), bdx.BlockQueryWithSkip{BlockCounter: 1, BytesToSkip: 100}.Encode()
			},
		})
		if be, ok := bdx.AsError(r.clientErr); !ok || be.Code != bdx.StatusLengthTooShort {
			t.Fatalf("client error %v, want a LengthTooShort BDX error", r.clientErr)
		}
		expectRecords(t, "client", r.client, []string{
			wantInit(sendInit, "R", "ota/data", "2048", "-"),
			wantBlock(block, 0, 966), wantBlock(block, 1, 966), wantStatus(bdx.StatusLengthTooShort),
		})
		// Recorded before the manipulation: the original BlockQuery.
		expectRecords(t, "server", r.server, []string{
			wantSendAccept("R"), wantCounter(blockQuery, 0), wantCounter(blockQuery, 1), wantCounter(blockQuery, 2),
		})
		if b := r.client[1].msg.(bdx.Block); !bytes.Equal(b.Data, data[:966]) {
			t.Fatal("block 0 is not the first 966 bytes")
		}
		if b := r.client[2].msg.(bdx.Block); !bytes.Equal(b.Data, data[1066:1066+966]) {
			t.Fatal("block 1 does not start 100 bytes after block 0")
		}
		if _, ok := r.serverStore.get("data"); ok {
			t.Fatal("the server stored the failed file")
		}
	})
}
