// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/bdx"
)

// streamOnlyBlobs is a backend that opts in to [BlobStreamer] on its own
// type and refuses every whole-slice Put or Get of an image, so a download
// that buffers the image fails (R2-P5-port-14). A record (.meta) and a
// provenance record (.origin) are small and still go through Put and Get. Its files live in the embedded
// MemoryBlobs; it counts its writers' Commits and Aborts, and runs onVerify
// on the first OpenBlob of a name after its Commit.
type streamOnlyBlobs struct {
	MemoryBlobs
	mu              sync.Mutex
	commits, aborts int
	committed       map[string]bool
	onVerify        func(name string)
}

var errWholeSlice = errors.New("whole-slice image access")

// record reports whether name is a small record that goes through Put
// and Get: an image's .meta or .origin.
func record(name string) bool {
	return strings.HasSuffix(name, metaSuffix) || strings.HasSuffix(name, originSuffix)
}

func (s *streamOnlyBlobs) Put(ctx context.Context, name string, data []byte) error {
	if !record(name) {
		return errWholeSlice
	}
	return s.MemoryBlobs.Put(ctx, name, data)
}

func (s *streamOnlyBlobs) Get(ctx context.Context, name string) ([]byte, error) {
	if !record(name) {
		return nil, errWholeSlice
	}
	return s.MemoryBlobs.Get(ctx, name)
}

func (s *streamOnlyBlobs) CreateBlob(_ context.Context, name string) (BlobWriter, error) {
	return &countingWriter{s: s, name: name}, nil
}

func (s *streamOnlyBlobs) OpenBlob(ctx context.Context, name string) (bdx.Blob, error) {
	s.mu.Lock()
	verify := s.committed[name] && s.onVerify != nil
	delete(s.committed, name)
	s.mu.Unlock()
	if verify {
		s.onVerify(name)
	}
	b, err := s.MemoryBlobs.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// countingWriter buffers one file and stores it at Commit.
type countingWriter struct {
	s    *streamOnlyBlobs
	name string
	buf  bytes.Buffer
	done bool
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.done {
		return 0, errors.New("write after Commit or Abort")
	}
	return w.buf.Write(p)
}

func (w *countingWriter) Commit() error {
	if w.done {
		return errors.New("Commit after Commit or Abort")
	}
	w.done = true
	w.s.mu.Lock()
	w.s.commits++
	if w.s.committed == nil {
		w.s.committed = map[string]bool{}
	}
	w.s.committed[w.name] = true
	w.s.mu.Unlock()
	return w.s.MemoryBlobs.Put(context.Background(), w.name, w.buf.Bytes())
}

func (w *countingWriter) Abort() error {
	w.s.mu.Lock()
	w.s.aborts++
	w.s.mu.Unlock()
	w.done = true
	return nil
}

func (s *streamOnlyBlobs) counts() (commits, aborts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commits, s.aborts
}

// plainBlobs is a backend without the streaming side: only [Blobs].
type plainBlobs struct{ Blobs }

func listed(t *testing.T, b Blobs) []string {
	t.Helper()
	names, err := b.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// TestImagesStreamedDownload: through a BlobStreamer the image is written
// as it arrives, read back for its checks and served from the backend in
// place — the whole-slice Put and Get are never used; the stored image
// is found, reused and opened as one downloaded through memory is.
func TestImagesStreamedDownload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFetchMock(t)
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	info := ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 7)
	b := &streamOnlyBlobs{}
	imgs := onlineImages(b)
	name := mustDownload(t, imgs, info, false)
	if c, a := b.counts(); c != 1 || a != 0 {
		t.Fatalf("%d commits, %d aborts; want 1 and 0", c, a)
	}
	if got := listed(t, b); !slices.Equal(got, []string{name, name + metaSuffix, name + originSuffix}) {
		t.Fatalf("stored %v", got)
	}
	found := imgs.Find(ctx, FindOptions{})
	if len(found) != 1 || found[0].Filename != name || found[0].Size != len(img) || found[0].SpecificationVersion != 7 {
		t.Fatalf("Find %+v", found)
	}
	blob, err := imgs.Open(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.NewSectionReader(blob, 0, blob.Size()))
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("served %d bytes (%v), want the image's %d", len(got), err, len(img))
	}
	// A second download finds the stored image valid, read in place.
	mustDownload(t, imgs, info, false)
	if n := len(f.callLog()); n != 1 {
		t.Fatalf("%d fetches, want the first only", n)
	}
}

// TestImagesStreamedDownloadRefused: a streamed image that fails a check
// leaves no file and no record, and one whose download fails is aborted
// before it is committed — with the errors of the buffered path.
func TestImagesStreamedDownloadRefused(t *testing.T) {
	t.Parallel()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	for _, tc := range []struct {
		name    string
		edit    func(f *fetchMock, info UpdateInfo) UpdateInfo
		want    string
		commits int
		aborts  int
	}{
		{"the ledger's checksum", func(_ *fetchMock, info UpdateInfo) UpdateInfo {
			l := *info.ledger
			l.OTAChecksum, l.OTAChecksumType = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", 1
			info.ledger = &l
			return info
		}, "checksum", 1, 0},
		{"the ledger's size", func(_ *fetchMock, info UpdateInfo) UpdateInfo {
			l := *info.ledger
			l.OTAFileSize++
			info.ledger = &l
			return info
		}, "size", 1, 0},
		{"the header's version", func(_ *fetchMock, info UpdateInfo) UpdateInfo {
			info.SoftwareVersion, info.ledger.SoftwareVersion = 4, 4
			return info
		}, "version", 1, 0},
		{"a failed fetch", func(f *fetchMock, info UpdateInfo) UpdateInfo {
			info.ledger.OTAURL = f.url("/missing.bin")
			return info
		}, "", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFetchMock(t)
			info := tc.edit(f, ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 7))
			for _, backend := range []string{"streamed", "buffered"} {
				b := &streamOnlyBlobs{}
				var blobs Blobs = b
				if backend == "buffered" {
					blobs = plainBlobs{&MemoryBlobs{}}
				}
				_, err := onlineImages(blobs).DownloadUpdate(context.Background(), info, false)
				if !errors.Is(err, ErrOTA) || errors.Is(err, errWholeSlice) || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
					t.Fatalf("%s: DownloadUpdate = %v, want an ErrOTA naming %q", backend, err, tc.want)
				}
				if got := listed(t, blobs); len(got) != 0 {
					t.Fatalf("%s: stored %v after a refused download", backend, got)
				}
				if backend == "streamed" {
					if c, a := b.counts(); c != tc.commits || a != tc.aborts {
						t.Fatalf("%d commits, %d aborts; want %d and %d", c, a, tc.commits, tc.aborts)
					}
				}
			}
		})
	}
}

// TestImagesStreamedImageHiddenUntilVerified: between the Commit and the
// end of its checks a streamed image is neither found nor opened.
func TestImagesStreamedImageHiddenUntilVerified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFetchMock(t)
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	info := ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
	b := &streamOnlyBlobs{}
	imgs := onlineImages(b)
	ran := false
	b.onVerify = func(name string) {
		ran = true
		if found := imgs.Find(ctx, FindOptions{}); len(found) != 0 {
			t.Errorf("Find during the checks: %+v", found)
		}
		if _, err := imgs.Open(ctx, name); err == nil {
			t.Error("Open during the checks served the image")
		}
	}
	name := mustDownload(t, imgs, info, false)
	if !ran {
		t.Fatal("the checks never read the image back")
	}
	if found := imgs.Find(ctx, FindOptions{}); len(found) != 1 || found[0].Filename != name {
		t.Fatalf("Find after the checks: %+v", found)
	}
}

// TestMemoryBlobsDoesNotStream: MemoryBlobs, and a type that embeds it
// to override Get and Put, keep the buffered path — the capability is
// opt-in on a backend's own type.
func TestMemoryBlobsDoesNotStream(t *testing.T) {
	t.Parallel()
	for _, b := range []Blobs{&MemoryBlobs{}, &flakyBlobs{}} {
		if _, ok := b.(BlobStreamer); ok {
			t.Fatalf("%T is a BlobStreamer", b)
		}
	}
}

// TestImagesBufferedDownloadWithoutStreamer: a backend without the
// streaming side keeps the buffered path — Put and Get.
func TestImagesBufferedDownloadWithoutStreamer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFetchMock(t)
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	info := ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
	mem := &MemoryBlobs{}
	imgs := onlineImages(plainBlobs{mem})
	name := mustDownload(t, imgs, info, false)
	if b, err := mem.Get(ctx, name); err != nil || !bytes.Equal(b, img) {
		t.Fatalf("stored %d bytes (%v)", len(b), err)
	}
	blob, err := imgs.Open(ctx, name)
	if err != nil || blob.Size() != int64(len(img)) {
		t.Fatalf("Open = %v, %v", blob, err)
	}
}

// TestImagesStreamedDownloadUnderTheChecksumRule: under the default
// configuration — a ledger image needs the ledger's checksum — a streamed
// download of a record with its checksum records the image's provenance
// (.origin, through Put like .meta), so the stored image opens and is
// offered from the store afterwards, as one downloaded through memory is.
func TestImagesStreamedDownloadUnderTheChecksumRule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFetchMock(t)
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	info := withChecksum(ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 7), img)
	b := &streamOnlyBlobs{}
	imgs := NewImages(ImagesConfig{Blobs: b, Ledger: hostLedger(0), Logger: quietLog()})
	name := mustDownload(t, imgs, info, false)
	if c, a := b.counts(); c != 1 || a != 0 {
		t.Fatalf("%d commits, %d aborts; want 1 and 0", c, a)
	}
	if got := listed(t, b); !slices.Equal(got, []string{name, name + metaSuffix, name + originSuffix}) {
		t.Fatalf("stored %v; want the image, its record and its provenance", got)
	}
	blob, err := imgs.Open(ctx, name)
	if err != nil {
		t.Fatalf("Open of the streamed image: %v", err)
	}
	got, err := io.ReadAll(io.NewSectionReader(blob, 0, blob.Size()))
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("served %d bytes (%v), want the image's %d", len(got), err, len(img))
	}
	if u, ok := imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{IncludeStored: true}); !ok || u.SoftwareVersion != 3 {
		t.Fatalf("CheckForUpdate with IncludeStored = %+v, %v; want the stored image offered", u, ok)
	}
}
