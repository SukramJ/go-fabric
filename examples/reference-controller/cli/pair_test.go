// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPairNetworkOptions: --wifi-ssid / --wifi-credentials and
// --thread-dataset become the commissioner's network options.
func TestPairNetworkOptions(t *testing.T) {
	wifi, thread, err := networkOptions("lab", "secret", "0e080000000000010000")
	if err != nil || wifi == nil || wifi.SSID != "lab" || wifi.Credentials != "secret" || thread == nil || !bytes.Equal(thread.OperationalDataset, []byte{0x0e, 0x08, 0, 0, 0, 0, 0, 1, 0, 0}) {
		t.Fatalf("networkOptions = %+v %+v %v", wifi, thread, err)
	}
	if wifi, thread, err := networkOptions("", "", ""); wifi != nil || thread != nil || err != nil {
		t.Fatalf("no network: %+v %+v %v", wifi, thread, err)
	}
	if _, _, err := networkOptions("", "secret", ""); err == nil || !strings.Contains(err.Error(), "--wifi-ssid") {
		t.Fatalf("credentials without SSID: %v", err)
	}
	if _, _, err := networkOptions("", "", "zz"); err == nil || !strings.Contains(err.Error(), "--thread-dataset") {
		t.Fatalf("bad dataset: %v", err)
	}
	stderr = &bytes.Buffer{}
	if err := run([]string{"--db", t.TempDir() + "/x.db", "pair", "--passcode", "20202021", "--wifi-credentials", "x"}, strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--wifi-ssid") {
		t.Fatalf("pair: %v", err)
	}
}

// TestNetworkSecretSources: a network secret comes from its flag, from the
// file an "@" flag names, or from the environment when the flag is empty,
// so it need not be on the command line (W5-ex-15).
//
//nolint:paralleltest // sets the environment
func TestNetworkSecretSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "psk")
	if err := os.WriteFile(path, []byte("from-a-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(wifiCredentialsEnv, "from-the-env")
	for _, c := range []struct{ value, want string }{
		{"on-the-line", "on-the-line"},
		{"@" + path, "from-a-file"},
		{"", "from-the-env"},
	} {
		if got, err := secret("--wifi-credentials", c.value, wifiCredentialsEnv); err != nil || got != c.want {
			t.Errorf("secret(%q) = %q, %v; want %q", c.value, got, err, c.want)
		}
	}
	if _, err := secret("--thread-dataset", "@"+filepath.Join(t.TempDir(), "missing"), threadDatasetEnv); err == nil || !strings.Contains(err.Error(), "--thread-dataset") {
		t.Errorf("a missing file: %v, want the flag named", err)
	}
}
