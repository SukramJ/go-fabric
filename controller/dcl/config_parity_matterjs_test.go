// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclConfigTest.ts at
// 862ad75f: the production and test-net ledger URLs and the GitHub
// development-certificate source defaults.
//
// matter.js DclConfig is the Go client's BaseURL with the ProductionURL
// and TestNetURL constants (client.go); DclGithubConfig is GitHubConfig
// with DefaultGitHubConfig (github.go).

import "testing"

// TestDclConfig: describe("DclConfig") (DclConfigTest.ts:9-22).
func TestDclConfig(t *testing.T) {
	t.Parallel()
	t.Run("production has correct default URL", func(t *testing.T) {
		t.Parallel()
		if ProductionURL != "https://on.dcl.csa-iot.org" {
			t.Fatal(ProductionURL)
		}
		if got := (&Client{}).baseURL(); got != "https://on.dcl.csa-iot.org" {
			t.Fatalf("zero-value client asks %s", got)
		}
	})
	t.Run("test has correct default URL", func(t *testing.T) {
		t.Parallel()
		if TestNetURL != "https://on.test-net.dcl.csa-iot.org" {
			t.Fatal(TestNetURL)
		}
	})
	t.Run("custom config satisfies interface", func(t *testing.T) {
		t.Parallel()
		if got := NewClient("https://custom.dcl.local").baseURL(); got != "https://custom.dcl.local" {
			t.Fatal(got)
		}
	})
}

// TestDclConfigDclGithubConfig: describe("DclGithubConfig")
// (DclConfigTest.ts:24-59).
func TestDclConfigDclGithubConfig(t *testing.T) {
	t.Parallel()
	d := DefaultGitHubConfig
	for _, tc := range []struct{ name, got, want string }{
		{"defaults has correct owner", d.Owner, "project-chip"},
		{"defaults has correct repo", d.Repo, "connectedhomeip"},
		{"defaults has correct branch", d.Branch, "master"},
		{"defaults has correct certPath", d.CertPath, "credentials/development/paa-root-certs"},
		{"defaults has correct cdSignerCertPath", d.CDSignerCertPath, "credentials/development/cd-certs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
	t.Run("custom config satisfies interface", func(t *testing.T) {
		t.Parallel()
		custom := GitHubConfig{Owner: "my-org", Repo: "my-repo", Branch: "main", CertPath: "certs/paa", CDSignerCertPath: "certs/cd-signers"}
		if custom.Owner != "my-org" || custom.Repo != "my-repo" || custom.Branch != "main" ||
			custom.CertPath != "certs/paa" || custom.CDSignerCertPath != "certs/cd-signers" {
			t.Fatalf("%+v", custom)
		}
	})
}
