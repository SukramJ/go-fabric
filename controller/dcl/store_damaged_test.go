// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestDamagedStoreIsReadAsEmpty (review II, R2-P6-store-1910) is the
// standing guard of the class "a short or damaged store file locks the
// service out": both loaders of a Store — the trust store's and the
// vendor service's — read a blob that does not parse as nothing stored,
// log it, and refetch from the ledger.
func TestDamagedStoreIsReadAsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pki := newTestPKI(t)
	f := newFakeDCL(t)
	f.addRoot(pki.paa)
	f.json("/dcl/vendorinfo/vendors", 200, map[string]any{"vendorInfo": []any{map[string]any{"vendorID": 0xFFF1, "vendorName": "Test"}}})
	for _, damaged := range [][]byte{[]byte(`{"certificates":[{"subjectKeyId":"AB`), []byte("\x00\x00\x00"), []byte(`[{"vendorID":`)} {
		path := filepath.Join(t.TempDir(), "store.json")
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatal(err)
		}
		log := &logRecorder{}
		s, err := OpenCertificateStore(ctx, CertificateOptions{Client: f.client(), Store: FileStore{Path: path}, Clock: newManualClock(), Logger: slog.New(log)})
		if err != nil {
			t.Fatalf("trust store over %q: %v", damaged, err)
		}
		if _, ok := s.PAA(pki.paa.skid()); !ok {
			t.Fatalf("trust store over %q: not refetched", damaged)
		}
		s.Close()
		if !log.hasMessage("dcl.certificates.store_unreadable") {
			t.Fatalf("trust store over %q: not logged", damaged)
		}
		vpath := filepath.Join(t.TempDir(), "vendors.json")
		if err := os.WriteFile(vpath, damaged, 0o600); err != nil {
			t.Fatal(err)
		}
		v, err := NewVendorInfoService(ctx, VendorInfoOptions{Client: f.client(), Store: FileStore{Path: vpath}, Clock: newManualClock(), UpdateInterval: -1})
		if err != nil {
			t.Fatalf("vendor store over %q: %v", damaged, err)
		}
		v.Close()
	}
}

// TestFileStoreRoundTrip: FileStore writes through a synced temporary file
// and a rename; what Save stored, Load returns, and no temporary file is
// left beside it.
func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := FileStore{Path: filepath.Join(dir, "store.json")}
	for _, data := range []string{"first", "second, longer"} {
		if err := fs.Save(context.Background(), []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := fs.Load(context.Background())
		if err != nil || string(got) != data {
			t.Fatalf("Load = %q, %v; want %q", got, err, data)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("%d files beside the store, want only it", len(entries))
	}
}
