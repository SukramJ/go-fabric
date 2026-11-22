// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
)

// closingBlob is a blob with a Close, counting its calls — a file-backed
// blob's handle.
type closingBlob struct {
	*bytes.Reader
	closes *atomic.Int32
}

func (b closingBlob) Close() error {
	b.closes.Add(1)
	return nil
}

func closingOpener(data []byte, closes, opened *atomic.Int32) func(context.Context) (bdx.Blob, error) {
	return func(context.Context) (bdx.Blob, error) {
		opened.Add(1)
		return closingBlob{Reader: bytes.NewReader(data), closes: closes}, nil
	}
}

// TestServe_ClosesTheBlob: a served blob that implements io.Closer is
// closed exactly once when the transfer ends — completed, refused after
// the open, or aborted by the peer — and a transfer refused before the
// open opens and closes nothing.
func TestServe_ClosesTheBlob(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, opened, closes *atomic.Int32, wantOpened int32) {
		t.Helper()
		if opened.Load() != wantOpened || closes.Load() != wantOpened {
			t.Fatalf("opened %d, closed %d; want %d and %d", opened.Load(), closes.Load(), wantOpened, wantOpened)
		}
	}

	t.Run("completed", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var opened, closes atomic.Int32
		a, b := newPipe(1178)
		data := testData(2500)
		done := make(chan error, 1)
		go func() {
			_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: closingOpener(data, &closes, &opened)})
			done <- err
		}()
		sink := &memSink{}
		if _, err := bdx.Receive(ctx, bdx.NewMessenger(a, 0), bdx.ReceiveOptions{
			TransferFileDesignator: bdx.FileDesignator("ota/f"), Sink: sink, MaxBlockSize: 1024,
		}); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("serve: %v", err)
		}
		if !bytes.Equal(sink.buf.Bytes(), data) {
			t.Fatalf("received %d bytes", sink.buf.Len())
		}
		check(t, &opened, &closes, 1)
	})

	for _, tc := range []struct {
		name       string
		init       bdx.Init
		modes      []bdx.DriverMode
		code       bdx.StatusCode
		wantOpened int32
	}{
		{"refused before the open", bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, FileDesignator: []byte("ota/other")}, nil, bdx.StatusFileDesignatorUnknown, 0},
		{"refused after the open: length", bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 64, MaxLength: u64(11), FileDesignator: []byte("ota/f")}, nil, bdx.StatusLengthTooLarge, 1},
		{"refused after the open: mode", bdx.Init{TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 64, FileDesignator: []byte("ota/f")}, []bdx.DriverMode{bdx.ReceiverDrive}, bdx.StatusTransferMethodNotSupported, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var opened, closes atomic.Int32
			_, b := newPipe(1178)
			_, err := bdx.Serve(ctx, bdx.NewMessenger(b, 0), bdx.ServeOptions{
				InitType: bdx.MessageReceiveInit, Init: tc.init, TransferFileDesignator: bdx.FileDesignator("ota/f"),
				Open: closingOpener(testData(10), &closes, &opened), PreferredDriverModes: tc.modes,
			})
			expectBdxError(t, err, tc.code)
			check(t, &opened, &closes, tc.wantOpened)
		})
	}

	t.Run("aborted by the peer", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var opened, closes atomic.Int32
		a, b := newPipe(1178)
		done := make(chan error, 1)
		go func() {
			_, err := serveFirst(ctx, b, 0, bdx.ServeOptions{
				TransferFileDesignator: bdx.FileDesignator("ota/f"), Open: closingOpener(testData(50), &closes, &opened),
				PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive},
			})
			done <- err
		}()
		m := bdx.NewMessenger(a, time.Second)
		if _, err := m.SendReceiveInit(ctx, bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/f")}); err != nil {
			t.Fatal(err)
		}
		_ = m.SendError(ctx, bdx.StatusLengthTooLarge)
		if err := <-done; err == nil {
			t.Fatal("an aborted transfer reported success")
		}
		check(t, &opened, &closes, 1)
	})
}
