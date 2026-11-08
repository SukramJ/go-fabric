// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// MDNS.Interfaces composes the responder's filter: empty is the default
// container filter itself, so the records are the ones without the field;
// non-empty keeps every interface not named out, and a named container
// bridge stays filtered (owner decision on issue 450;
// BD-matterjs-mdns-interface-allowlist).
func TestMDNSInterfacesFilter(t *testing.T) {
	names := []string{"eth0", "eth1", "br30", "docker0", "hassio", "veth12ab", "wlan0", "lo"}
	cfg := mdnsTestConfig(t)
	r := newMDNSTestNode(t, cfg).advertiser.(*mdns.Responder)
	for _, name := range names {
		if got, want := r.InterfaceFilter(name), mdns.DefaultInterfaceFilter(name); got != want {
			t.Errorf("empty Interfaces: filter(%q) = %v, want the default filter's %v", name, got, want)
		}
	}

	cfg = mdnsTestConfig(t)
	cfg.Network.MDNS.Interfaces = []string{"br30", "docker0"}
	r = newMDNSTestNode(t, cfg).advertiser.(*mdns.Responder)
	for _, name := range names {
		if got, want := r.InterfaceFilter(name), name != "br30"; got != want {
			t.Errorf("Interfaces [br30 docker0]: filter(%q) = %v, want %v", name, got, want)
		}
	}
}

// Validate refuses an empty or a duplicate interface name and accepts a
// name the host does not have.
func TestMDNSInterfacesValidate(t *testing.T) {
	for _, row := range []struct {
		names []string
		want  string
	}{
		{[]string{"eth0", ""}, "Interfaces[1] is empty"},
		{[]string{" "}, "Interfaces[0] is empty"},
		{[]string{"eth0", "br30", "eth0"}, `names "eth0" twice`},
		{[]string{"gofabric-none0"}, ""},
	} {
		cfg := mdnsTestConfig(t)
		cfg.Network.MDNS.Interfaces = row.names
		err := cfg.Validate()
		if row.want == "" {
			if err != nil {
				t.Errorf("%q: %v", row.names, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), row.want) {
			t.Errorf("%q: %v, want %q", row.names, err, row.want)
		}
	}
}

// DiagnoseMDNS names an interface of the allow-list the host does not
// have — the operator's typo — as an error when nothing else is named.
func TestDiagnoseMDNSNamesUnknownInterface(t *testing.T) {
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Interfaces = []string{"gofabric-none0"}
	n := newMDNSTestNode(t, cfg)
	var found *MDNSFinding
	for _, f := range n.DiagnoseMDNS() {
		if f.Code == "interface_not_found" {
			found = &f
		}
	}
	if found == nil || found.Severity != mdns.SeverityError || !strings.Contains(found.Message, "`gofabric-none0`") {
		t.Fatalf("DiagnoseMDNS = %+v, want an interface_not_found error naming gofabric-none0", n.DiagnoseMDNS())
	}

	cfg = mdnsTestConfig(t)
	n = newMDNSTestNode(t, cfg)
	for _, f := range n.DiagnoseMDNS() {
		if strings.HasPrefix(f.Code, "interface_") {
			t.Fatalf("empty Interfaces: finding %+v", f)
		}
	}
}
