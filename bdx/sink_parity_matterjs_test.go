// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

// A port of matter.js packages/protocol/test/bdx/PersistedFileDesignatorTest.ts
// at 862ad75f: a stored file named once and written again and again answers
// its latest content, and refuses to open once deleted. The Go form of
// PersistedFileDesignator (doc.go) is a path written through a [bdx.FileSink]
// per transfer, as the OTA requestor's download does, and read back as a
// [bdx.Blob]; it holds no opened Blob that could outlive a write.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/bdx"
)

// persistedFile is the designator "ota-1-2" over a directory (the
// MemoryBlobStorageDriver of the matter.js test).
type persistedFile struct{ path string }

func newPersistedFile(t *testing.T) persistedFile {
	t.Helper()
	return persistedFile{path: filepath.Join(t.TempDir(), "ota-1-2")}
}

// writeFromStream writes text as one transfer and commits it.
func (p persistedFile) writeFromStream(t *testing.T, text string) {
	t.Helper()
	s := bdx.NewFileSink(p.path)
	if _, err := s.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
}

type fileBlob struct {
	*os.File
	size int64
}

func (b fileBlob) Size() int64 { return b.size }

// openBlob opens the stored file as a Blob; a missing one wraps
// fs.ErrNotExist (matter.js "must point to an existing blob").
func (p persistedFile) openBlob(t *testing.T) (bdx.Blob, error) {
	t.Helper()
	f, err := os.Open(p.path)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = f.Close() })
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return fileBlob{File: f, size: fi.Size()}, nil
}

func (p persistedFile) text(t *testing.T) string {
	t.Helper()
	b, err := p.openBlob(t)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(io.NewSectionReader(b, 0, b.Size()))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (p persistedFile) delete(t *testing.T) {
	t.Helper()
	if err := os.Remove(p.path); err != nil {
		t.Fatal(err)
	}
}

func (p persistedFile) exists() bool {
	_, err := os.Stat(p.path)
	return err == nil
}

func TestPersistedFileDesignator(t *testing.T) {
	t.Run("reads back what was written", func(t *testing.T) {
		file := newPersistedFile(t)
		file.writeFromStream(t, "first")
		if got := file.text(t); got != "first" {
			t.Fatalf("read %q, want first", got)
		}
	})

	t.Run("answers a second write rather than the blob it opened for the first", func(t *testing.T) {
		file := newPersistedFile(t)
		file.writeFromStream(t, "first")
		if got := file.text(t); got != "first" {
			t.Fatalf("read %q, want first", got)
		}
		file.writeFromStream(t, "second")
		if got := file.text(t); got != "second" {
			t.Fatalf("read %q, want second", got)
		}
	})

	t.Run("answers a write that follows a delete", func(t *testing.T) {
		file := newPersistedFile(t)
		file.writeFromStream(t, "first")
		if _, err := file.openBlob(t); err != nil {
			t.Fatal(err)
		}
		file.delete(t)
		if file.exists() {
			t.Fatal("exists after delete")
		}
		file.writeFromStream(t, "second")
		if got := file.text(t); got != "second" {
			t.Fatalf("read %q, want second", got)
		}
	})

	t.Run("refuses to open a blob it deleted", func(t *testing.T) {
		file := newPersistedFile(t)
		file.writeFromStream(t, "first")
		if _, err := file.openBlob(t); err != nil {
			t.Fatal(err)
		}
		file.delete(t)
		if _, err := file.openBlob(t); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("open after delete: %v, want fs.ErrNotExist", err)
		}
	})
}
