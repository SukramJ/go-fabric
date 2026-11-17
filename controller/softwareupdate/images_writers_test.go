// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
)

// TestImagesOneWriterPerName (review II, R2-P6-conc-1901, carried
// DR5b-c3-07) is the standing guard of the class "two writers of one
// file name": a DownloadUpdate waits in its fetch for bytes that will
// fail their check, and a second writer of the same name — every writer
// the store has, through each backend — runs meanwhile. The second
// writer's verified image must open after both returned: the first
// writer may fail, but not on the second's file. A new writer of a file
// name joins the table.
func TestImagesOneWriterPerName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	sum := sha256.Sum256(img)
	info := ledgerUpdate("https://example.invalid/ota.bin", 0xfff1, 0x8000, 3, len(img), 7)
	info.ledger.OTAChecksum = base64.StdEncoding.EncodeToString(sum[:])
	info.ledger.OTAChecksumType = 1

	backends := map[string]func() Blobs{
		"streamed": func() Blobs { return &streamAndPutBlobs{} },
		"buffered": func() Blobs { return &MemoryBlobs{} },
	}
	writers := map[string]func(imgs *Images) (string, error){
		"DownloadUpdate": func(imgs *Images) (string, error) { return imgs.DownloadUpdate(ctx, info, false) },
		"DownloadUpdate force": func(imgs *Images) (string, error) {
			return imgs.DownloadUpdate(ctx, info, true)
		},
		"Store": func(imgs *Images) (string, error) {
			st, err := imgs.Store(ctx, img, ModeProd, &info)
			return st.Filename, err
		},
	}
	for bname, backend := range backends {
		for wname, second := range writers {
			t.Run(bname+"/"+wname, func(t *testing.T) {
				t.Parallel()
				imgs := NewImages(ImagesConfig{Blobs: backend(), Ledger: hostLedger(0), Logger: quietLog()})
				var calls atomic.Int32
				entered, release := make(chan struct{}), make(chan struct{})
				imgs.download = func(_ context.Context, _ dcl.SoftwareVersion, w io.Writer) error {
					if calls.Add(1) == 1 { // the first writer: other bytes, late
						close(entered)
						<-release
						_, err := w.Write(make([]byte, len(img)))
						return err
					}
					_, err := w.Write(img)
					return err
				}
				waiting := make(chan struct{}, 1)
				imgs.names.waited = func(string) {
					select {
					case waiting <- struct{}{}:
					default:
					}
				}
				firstErr := make(chan error, 1)
				go func() {
					_, err := imgs.DownloadUpdate(ctx, info, false)
					firstErr <- err
				}()
				<-entered
				type result struct {
					name string
					err  error
				}
				secondDone := make(chan result, 1)
				go func() {
					name, err := second(imgs)
					secondDone <- result{name, err}
				}()
				var got *result
				select {
				case <-waiting: // the second writer waits for the first
				case r := <-secondDone: // it did not wait
					got = &r
				}
				close(release)
				if err := <-firstErr; err == nil {
					t.Fatal("the first writer's bad bytes passed their check")
				}
				if got == nil {
					r := <-secondDone
					got = &r
				}
				if got.err != nil {
					t.Fatalf("second writer: %v", got.err)
				}
				b, err := imgs.Open(ctx, got.name)
				if err != nil {
					t.Fatalf("the second writer's image %q does not open after both returned: %v", got.name, err)
				}
				closeBlob(b)
			})
		}
	}
}

// streamAndPutBlobs is a BlobStreamer that also takes and returns whole
// images, as a file backend does: Store writes through Put.
type streamAndPutBlobs struct{ streamOnlyBlobs }

func (s *streamAndPutBlobs) Put(ctx context.Context, name string, data []byte) error {
	return s.MemoryBlobs.Put(ctx, name, data)
}

func (s *streamAndPutBlobs) Get(ctx context.Context, name string) ([]byte, error) {
	return s.MemoryBlobs.Get(ctx, name)
}

// failOriginDelete is a backend whose removal of a provenance record
// fails.
type failOriginDelete struct {
	streamOnlyBlobs
	created atomic.Int32
}

func (f *failOriginDelete) Delete(ctx context.Context, name string) error {
	if strings.HasSuffix(name, originSuffix) {
		return errors.New("disk failure")
	}
	return f.streamOnlyBlobs.Delete(ctx, name)
}

func (f *failOriginDelete) Put(ctx context.Context, name string, data []byte) error {
	return f.MemoryBlobs.Put(ctx, name, data)
}

func (f *failOriginDelete) CreateBlob(ctx context.Context, name string) (BlobWriter, error) {
	f.created.Add(1)
	return f.streamOnlyBlobs.CreateBlob(ctx, name)
}

// TestImagesWriteNeedsTheRecordRemoved (review II, 5b-dcl R9): a writer
// that cannot remove the provenance record of a file name writes nothing
// under it — Store and DownloadUpdate's streamed path both fail with
// ErrOTA before the image is written.
func TestImagesWriteNeedsTheRecordRemoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	sum := sha256.Sum256(img)
	info := ledgerUpdate("https://example.invalid/ota.bin", 0xfff1, 0x8000, 3, len(img), 7)
	info.ledger.OTAChecksum = base64.StdEncoding.EncodeToString(sum[:])
	info.ledger.OTAChecksumType = 1
	name := fileName(0xfff1, 0x8000, ModeProd, 3)

	t.Run("Store", func(t *testing.T) {
		t.Parallel()
		b := &failOriginDelete{}
		imgs := NewImages(ImagesConfig{Blobs: b, Ledger: hostLedger(0), Logger: quietLog()})
		if _, err := imgs.Store(ctx, img, ModeProd, &info); !errors.Is(err, ErrOTA) {
			t.Fatalf("Store = %v, want ErrOTA", err)
		}
		if _, err := b.MemoryBlobs.Get(ctx, name); err == nil {
			t.Fatal("the image was written although its former record stays")
		}
	})
	t.Run("DownloadUpdate", func(t *testing.T) {
		t.Parallel()
		b := &failOriginDelete{}
		imgs := NewImages(ImagesConfig{Blobs: b, Ledger: hostLedger(0), Logger: quietLog()})
		imgs.download = func(_ context.Context, _ dcl.SoftwareVersion, w io.Writer) error {
			_, err := w.Write(img)
			return err
		}
		if _, err := imgs.DownloadUpdate(ctx, info, false); !errors.Is(err, ErrOTA) {
			t.Fatalf("DownloadUpdate = %v, want ErrOTA", err)
		}
		if n := b.created.Load(); n != 0 {
			t.Fatalf("CreateBlob called %d times; nothing may be written", n)
		}
	})
}

// TestImagesWriterWaitHonoursTheContext (review II, R2-P6-conc-1901): a
// second writer of a name waits for the first only as long as its own
// context lasts; it then fails with ErrOTA and the context's error.
func TestImagesWriterWaitHonoursTheContext(t *testing.T) {
	t.Parallel()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	sum := sha256.Sum256(img)
	info := ledgerUpdate("https://example.invalid/ota.bin", 0xfff1, 0x8000, 3, len(img), 7)
	info.ledger.OTAChecksum = base64.StdEncoding.EncodeToString(sum[:])
	info.ledger.OTAChecksumType = 1
	imgs := NewImages(ImagesConfig{Blobs: &streamAndPutBlobs{}, Ledger: hostLedger(0), Logger: quietLog()})
	entered, release := make(chan struct{}), make(chan struct{})
	imgs.download = func(_ context.Context, _ dcl.SoftwareVersion, w io.Writer) error {
		close(entered)
		<-release
		_, err := w.Write(img)
		return err
	}
	first := make(chan error, 1)
	go func() {
		_, err := imgs.DownloadUpdate(context.Background(), info, false)
		first <- err
	}()
	<-entered
	defer func() { close(release); <-first }()
	for name, write := range map[string]func(context.Context) error{
		"DownloadUpdate": func(ctx context.Context) error { _, err := imgs.DownloadUpdate(ctx, info, false); return err },
		"Store":          func(ctx context.Context) error { _, err := imgs.Store(ctx, img, ModeProd, &info); return err },
	} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan error, 1)
		go func() { done <- write(ctx) }()
		watchdog, stop := context.WithTimeout(context.Background(), 10*time.Second)
		select {
		case err := <-done:
			if !errors.Is(err, ErrOTA) || !errors.Is(err, context.Canceled) {
				t.Errorf("%s with a cancelled context: %v, want ErrOTA and context.Canceled", name, err)
			}
		case <-watchdog.Done():
			t.Errorf("%s waits for the other writer past its context", name)
		}
		stop()
	}
}
