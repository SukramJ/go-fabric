// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

// How much of a refused image download reaches the store or memory.
// dcl.OTAVersions.Download reads at most one byte past its limit — the
// announced size, else MaxImageSize — so an answer longer than either is
// cut there; an image whose checksum or length the checks refuse has been
// read in full first. These tests serve such answers from a TLS server and
// count, for a streamed backend, the bytes written to the file and whether
// it was committed or aborted, and for both the bytes the process
// allocated during the call. The streamed counts and the store's state are
// held to the bound; the allocation counts are logged.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/bdx/otaimage"
	"thingfabric.dev/go-fabric/dcl"
)

// volumeBlobs is a streamed backend that records, per writer, how many
// bytes were written and whether the file was committed or aborted.
type volumeBlobs struct {
	MemoryBlobs
	mu       sync.Mutex
	written  []int64
	commits  int
	aborts   int
	readback int
}

func (v *volumeBlobs) CreateBlob(_ context.Context, name string) (BlobWriter, error) {
	v.mu.Lock()
	v.written = append(v.written, 0)
	i := len(v.written) - 1
	v.mu.Unlock()
	return &volumeWriter{v: v, i: i, name: name}, nil
}

func (v *volumeBlobs) OpenBlob(ctx context.Context, name string) (bdx.Blob, error) {
	b, err := v.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	v.readback++
	v.mu.Unlock()
	return bytes.NewReader(b), nil
}

type volumeWriter struct {
	v    *volumeBlobs
	i    int
	name string
	buf  bytes.Buffer
}

func (w *volumeWriter) Write(p []byte) (int, error) {
	w.v.mu.Lock()
	w.v.written[w.i] += int64(len(p))
	w.v.mu.Unlock()
	return w.buf.Write(p)
}

func (w *volumeWriter) Commit() error {
	w.v.mu.Lock()
	w.v.commits++
	w.v.mu.Unlock()
	return w.v.Put(context.Background(), w.name, w.buf.Bytes())
}

func (w *volumeWriter) Abort() error {
	w.v.mu.Lock()
	w.v.aborts++
	w.v.mu.Unlock()
	w.buf = bytes.Buffer{}
	return nil
}

func (v *volumeBlobs) totals() (written int64, commits, aborts, readback int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, n := range v.written {
		written += n
	}
	return written, v.commits, v.aborts, v.readback
}

// volumeServer answers every GET with prefix and then, when endless, with
// 0x5A bytes until the client goes away (at most a hard stop of 1 GiB). It
// counts the requests and the body bytes it handed to the connection.
type volumeServer struct {
	srv      *httptest.Server
	requests atomic.Int64
	sent     atomic.Int64
}

func newVolumeServer(t *testing.T, prefix []byte, endless bool) *volumeServer {
	t.Helper()
	s := &volumeServer{}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		n, err := w.Write(prefix)
		s.sent.Add(int64(n))
		if err != nil || !endless {
			return
		}
		chunk := bytes.Repeat([]byte{0x5A}, 32<<10)
		for s.sent.Load() < 1<<30 && r.Context().Err() == nil {
			n, err := w.Write(chunk)
			s.sent.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// bigOtaImage is a valid OTA image for vid/pid/version with a payload of
// payload bytes.
func bigOtaImage(t *testing.T, vid, pid uint16, version uint32, payload int) []byte {
	t.Helper()
	b, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: vid, ProductID: pid, SoftwareVersion: version, SoftwareVersionString: "v3.0.0",
		Payload: bytes.Repeat([]byte{0xA5}, payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// allocDuring is the bytes the process allocated while fn ran, and the
// largest rise of the heap objects (live and not yet swept) above their level before fn, sampled every
// millisecond.
func allocDuring(fn func()) (alloc, peak uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	base := sample[0].Value.Uint64()
	var top atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := []metrics.Sample{{Name: sample[0].Name}}
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			metrics.Read(s)
			if v := s[0].Value.Uint64(); v > top.Load() {
				top.Store(v)
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	fn()
	close(stop)
	<-done
	runtime.ReadMemStats(&after)
	if t := top.Load(); t > base {
		peak = t - base
	}
	return after.TotalAlloc - before.TotalAlloc, peak
}

// TestImagesDownloadVolumeAboveTheBound: an answer that does not end is
// cut one byte past the bound — MaxImageSize when the record announces no
// size, the announced size when it does — and a record that announces
// more than MaxImageSize is refused before the request. The streamed
// backend's file is aborted, never committed, after one byte past the
// limit; neither backend keeps anything.
func TestImagesDownloadVolumeAboveTheBound(t *testing.T) {
	const (
		vid, pid        = 0xfff1, 0x8000
		version  uint32 = 3
		bound    int64  = 4 << 20
	)
	img := createOtaImage(t, vid, pid, version)
	for _, tc := range []struct {
		name      string
		announced int
		requests  int64
		limit     int64 // the bytes the download may read; 0 for none
	}{
		{"no size announced", 0, 1, bound},
		{"a size below the bound announced", 1 << 20, 1, 1 << 20},
		{"a size above the bound announced", int(bound) + 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, backend := range []string{"streamed", "buffered"} {
				srv := newVolumeServer(t, img, true)
				info := withChecksum(ledgerUpdate(srv.srv.URL+"/ota.bin", vid, pid, version, tc.announced, 0), img)
				vb := &volumeBlobs{}
				var blobs Blobs = vb
				if backend == "buffered" {
					blobs = plainBlobs{&MemoryBlobs{}}
				}
				imgs := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(bound), Logger: quietLog()})
				var err error
				alloc, peak := allocDuring(func() { _, err = imgs.DownloadUpdate(context.Background(), info, false) })
				if !errors.Is(err, ErrOTA) || !errors.Is(err, dcl.ErrOTAImageTooLarge) {
					t.Fatalf("%s: DownloadUpdate = %v, want ErrOTA wrapping dcl.ErrOTAImageTooLarge", backend, err)
				}
				if got := srv.requests.Load(); got != tc.requests {
					t.Fatalf("%s: %d requests, want %d", backend, got, tc.requests)
				}
				if got := listed(t, blobs); len(got) != 0 {
					t.Fatalf("%s: stored %v after the refusal", backend, got)
				}
				t.Logf("%s: bound %d, announced %d: server handed %d bytes to the connection; the process allocated %d bytes during the call, heap objects rose by up to %d",
					backend, bound, tc.announced, srv.sent.Load(), alloc, peak)
				if backend == "streamed" {
					written, commits, aborts, _ := vb.totals()
					t.Logf("streamed: %d bytes written to the file, %d commits, %d aborts", written, commits, aborts)
					// The file is created before the request, so a refusal
					// without one aborts an empty file.
					want := int64(0)
					if tc.limit > 0 {
						want = tc.limit + 1
					}
					if written != want || commits != 0 || aborts != 1 {
						t.Fatalf("streamed: %d bytes written, %d commits, %d aborts; want %d, 0, 1", written, commits, aborts, want)
					}
				}
				// The allocation counts are logged, not held: they are the
				// whole process's and move with the build (-race allocates
				// several times more).
			}
		})
	}
}

// TestImagesDownloadVolumeRefusedByItsChecks: an image within the bound
// whose whole-file checksum does not match, and one whose answer ends
// before the announced length, are read in full before the refusal. The
// streamed backend writes and commits the bytes received, reads them back
// for the checks and then deletes the file; the buffered backend holds
// them in memory. Nothing stays stored.
func TestImagesDownloadVolumeRefusedByItsChecks(t *testing.T) {
	const (
		vid, pid        = 0xfff1, 0x8000
		version  uint32 = 3
		payload         = 2 << 20
	)
	img := bigOtaImage(t, vid, pid, version, payload)
	other := bigOtaImage(t, vid, pid, version, payload-1)
	for _, tc := range []struct {
		name   string
		served []byte
		record func(url string) UpdateInfo
	}{
		{"checksum of another image", img, func(url string) UpdateInfo {
			return withChecksum(ledgerUpdate(url, vid, pid, version, len(img), 0), other)
		}},
		{"answer shorter than announced", img[:len(img)/2], func(url string) UpdateInfo {
			return withChecksum(ledgerUpdate(url, vid, pid, version, len(img), 0), img)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, backend := range []string{"streamed", "buffered"} {
				srv := newVolumeServer(t, tc.served, false)
				info := tc.record(srv.srv.URL + "/ota.bin")
				vb := &volumeBlobs{}
				var blobs Blobs = vb
				if backend == "buffered" {
					blobs = plainBlobs{&MemoryBlobs{}}
				}
				imgs := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: quietLog()})
				var err error
				alloc, peak := allocDuring(func() { _, err = imgs.DownloadUpdate(context.Background(), info, false) })
				if !errors.Is(err, ErrOTA) || errors.Is(err, dcl.ErrOTAImageTooLarge) {
					t.Fatalf("%s: DownloadUpdate = %v, want ErrOTA from the checks", backend, err)
				}
				if got := listed(t, blobs); len(got) != 0 {
					t.Fatalf("%s: stored %v after the refusal", backend, got)
				}
				t.Logf("%s: image %d bytes, served %d: the process allocated %d bytes during the call, heap objects rose by up to %d; err = %v",
					backend, len(img), len(tc.served), alloc, peak, err)
				if backend == "streamed" {
					written, commits, aborts, readback := vb.totals()
					t.Logf("streamed: %d bytes written to the file, %d commits, %d aborts, %d read-backs", written, commits, aborts, readback)
					if written != int64(len(tc.served)) || commits != 1 || aborts != 0 {
						t.Fatalf("streamed: %d bytes written, %d commits, %d aborts; want %d, 1, 0", written, commits, aborts, len(tc.served))
					}
				}
			}
		})
	}
}
