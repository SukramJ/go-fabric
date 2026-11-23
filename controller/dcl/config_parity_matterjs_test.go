// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"testing"
)

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
