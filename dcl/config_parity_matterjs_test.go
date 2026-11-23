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
