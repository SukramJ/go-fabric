// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/dcl"
)

// TestDCLFlag: --dcl off (the default) builds no client — nothing reaches
// the ledger — prod and testnet name the CSA ledgers, anything else is
// refused by the flag check.
func TestDCLFlag(t *testing.T) {
	t.Parallel()
	for mode, want := range map[string]string{"off": "", "": "", "prod": dcl.ProductionURL, "testnet": dcl.TestNetURL} {
		c, err := dclClient(mode)
		switch {
		case err != nil:
			t.Errorf("%q: %v", mode, err)
		case want == "" && c != nil:
			t.Errorf("%q built a client", mode)
		case want != "" && (c == nil || c.BaseURL != want):
			t.Errorf("%q: %+v", mode, c)
		}
	}
	if _, _, err := checkFlags(flags{passcode: 20202021, discriminator: 3840, iterations: 1000, dcl: "mainnet"}); err == nil || !strings.Contains(err.Error(), "--dcl") {
		t.Fatalf("--dcl mainnet: %v", err)
	}
	if _, _, err := checkFlags(flags{passcode: 20202021, discriminator: 3840, iterations: 1000, dcl: "off"}); err != nil {
		t.Fatalf("--dcl off: %v", err)
	}
}

// logLines records the log's messages.
type logLines struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logLines) Enabled(context.Context, slog.Level) bool { return true }
func (l *logLines) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.msgs = append(l.msgs, r.Message)
	l.mu.Unlock()
	return nil
}
func (l *logLines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logLines) WithGroup(string) slog.Handler      { return l }

func (l *logLines) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.msgs) == 0 {
		return ""
	}
	return l.msgs[len(l.msgs)-1]
}

// TestLedgerCheckedImages: the provider's offer is looked up in the
// ledger — listed with the image's checksum, listed with another, not
// listed — and offered whatever the answer.
func TestLedgerCheckedImages(t *testing.T) {
	t.Parallel()
	images, err := newTestImages()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(images.image)
	checksum := base64.StdEncoding.EncodeToString(sum[:])
	var mu sync.Mutex
	answer := checksum
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		c := answer
		mu.Unlock()
		if r.URL.Path != fmt.Sprintf("/dcl/model/versions/%d/%d/%d", testVendorID, testProductID, testProviderImageVersion) || c == "absent" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":5,"message":"not found","details":[]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"modelVersion": map[string]any{
			"vid": testVendorID, "pid": testProductID, "softwareVersion": testProviderImageVersion, "softwareVersionString": "2",
			"softwareVersionValid": true, "otaUrl": "https://ota.invalid/2", "otaFileSize": strconv.Itoa(len(images.image)),
			"otaChecksum": c, "otaChecksumType": 1, "maxApplicableSoftwareVersion": 1, "schemaVersion": 0,
		}})
	}))
	defer srv.Close()
	log := &logLines{}
	src := &ledgerCheckedImages{
		OTAImageSource: images, versions: &dcl.OTAVersions{Client: &dcl.Client{BaseURL: srv.URL, HTTPClient: srv.Client()}},
		image: func(string) []byte { return images.image }, logger: slog.New(log),
	}
	req := provdef.QueryImageRequest{VendorId: testVendorID, ProductId: testProductID, SoftwareVersion: 1}
	for _, tc := range []struct{ answer, want string }{
		{checksum, "dcl.ota.in_ledger"},
		{"", "dcl.ota.in_ledger"},
		{"AAAA", "dcl.ota.ledger_mismatch"},
		{"absent", "dcl.ota.not_in_ledger"},
	} {
		mu.Lock()
		answer = tc.answer
		mu.Unlock()
		img, ok, err := src.QueryImage(context.Background(), mattercore.OTAPeer{}, req)
		if err != nil || !ok || img.SoftwareVersion != testProviderImageVersion {
			t.Fatalf("%q: offer %+v %v %v", tc.answer, img, ok, err)
		}
		if got := log.last(); got != tc.want {
			t.Errorf("%q: logged %q, want %q", tc.answer, got, tc.want)
		}
	}
	// A request for another product is not offered and not looked up.
	before := log.last()
	if _, ok, _ := src.QueryImage(context.Background(), mattercore.OTAPeer{}, provdef.QueryImageRequest{VendorId: 1, ProductId: 2}); ok || log.last() != before {
		t.Fatal("another product was offered or looked up")
	}
	// An unreachable ledger is a warning, the image still offered.
	src.versions = &dcl.OTAVersions{Client: &dcl.Client{BaseURL: "http://127.0.0.1:1"}}
	if _, ok, _ := src.QueryImage(context.Background(), mattercore.OTAPeer{}, req); !ok || log.last() != "dcl.ota.lookup_failed" {
		t.Fatalf("unreachable ledger: %v %q", ok, log.last())
	}
}
