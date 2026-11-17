// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCertificateUpdateListsBounded (review II, R2-P6-bound-1911): of a
// root list or a GitHub directory longer than the bound, an update fetches
// the bound's number of entries and logs the cut.
func TestCertificateUpdateListsBounded(t *testing.T) {
	t.Parallel()
	t.Run("ledger roots", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		refs := make([]any, maxRootsPerUpdate+100)
		for i := range refs {
			refs[i] = map[string]any{"subject": fmt.Sprintf("S%d", i), "subjectKeyId": fmt.Sprintf("%040X", i)}
		}
		f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": refs, "schemaVersion": 0}})
		log := &logRecorder{}
		openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock(), Logger: slog.New(log)})
		if n := f.hitsPrefix("/dcl/pki/certificates/"); n != maxRootsPerUpdate {
			t.Fatalf("%d roots fetched, want %d", n, maxRootsPerUpdate)
		}
		if !log.hasMessage("dcl.certificates.too_many_roots") {
			t.Fatal("the cut is not logged")
		}
	})
	t.Run("GitHub files", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		gh := newFakeDCL(t)
		f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
		entry := func(name, typ, path string) map[string]any {
			return map[string]any{"path": name, "type": typ, "url": gh.url(path)}
		}
		tree := func(path string, entries ...map[string]any) {
			gh.json(path, 200, map[string]any{"sha": "x", "tree": entries})
		}
		tree("/repos/project-chip/connectedhomeip/git/trees/master", entry("credentials", "tree", "/t/credentials"))
		tree("/t/credentials", entry("development", "tree", "/t/dev"))
		tree("/t/dev", entry("paa-root-certs", "tree", "/t/paa"), entry("cd-certs", "tree", "/t/cd"))
		files := make([]map[string]any, maxGitHubFilesPerUpdate+100)
		for i := range files {
			files[i] = entry(fmt.Sprintf("f%d.der", i), "blob", fmt.Sprintf("/b/%d", i))
		}
		tree("/t/paa", files...)
		tree("/t/cd")
		log := &logRecorder{}
		openStore(t, CertificateOptions{
			Client: f.client(), TestClient: f.client(), FetchTestCertificates: true, FetchGitHubCertificates: true,
			GitHub: GitHubConfig{APIURL: gh.srv.URL, Timeout: 5 * time.Second}, Clock: newManualClock(), Logger: slog.New(log),
		})
		if n := gh.hitsPrefix("/b/"); n != maxGitHubFilesPerUpdate {
			t.Fatalf("%d files fetched, want %d", n, maxGitHubFilesPerUpdate)
		}
		if !log.hasMessage("dcl.certificates.too_many_github_files") {
			t.Fatal("the cut is not logged")
		}
	})
}

// hitsPrefix is the number of requests whose URI starts with prefix.
func (f *fakeDCL) hitsPrefix(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for uri, c := range f.hits {
		if strings.HasPrefix(uri, prefix) {
			n += c
		}
	}
	return n
}

// TestCertificateWaitersHonourTheirContext (review II, R2-P6-bound-1911):
// while an update holds the store, a GetOrFetchCertificate, a re-fetch and
// a second Update whose context ends return then, not when the update
// does.
func TestCertificateWaitersHonourTheirContext(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock(), UpdateInterval: -1})
	release := make(chan struct{})
	entered := make(chan struct{})
	f.route("/dcl/pki/root-certificates", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"approvedRootCertificates":{"certs":[],"schemaVersion":0}}`))
	})
	running := make(chan bool, 1)
	go func() { running <- s.Update(context.Background(), false) }()
	<-entered
	defer func() { close(release); <-running }()

	wait := func(name string, call func(ctx context.Context)) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		done := make(chan struct{})
		go func() { call(ctx); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second): // below the request timeout that would end the update
			t.Fatalf("%s waits for the running update past its context", name)
		}
	}
	wait("GetOrFetchCertificate", func(ctx context.Context) {
		if _, ok := s.GetOrFetchCertificate(ctx, "AB", GetOrFetchCertificateOptions{}); ok {
			t.Error("found a certificate the store does not hold")
		}
	})
	wait("the re-fetch", func(ctx context.Context) { s.tryRefetchRootCertificate(ctx, "AB", true, true) })
	wait("Update", func(ctx context.Context) {
		if s.Update(ctx, false) {
			t.Error("an update that never ran reported success")
		}
	})
}
