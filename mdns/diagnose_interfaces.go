// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"slices"
	"strings"
)

// DiagnoseInterfaces checks an allow-list of host interface names — the
// interfaces a host restricted its advertiser to (node.MDNS.Interfaces) —
// against the host's interfaces now: a name no interface carries, a name
// [DefaultInterfaceFilter] keeps out (a container or virtualisation
// bridge stays filtered when named), and a name whose interface carries
// no address the advertise policy publishes (down, not multicast-capable,
// a loopback or tunnel, or without an address). The operator's typo is the
// likely cause of each, and each is silent otherwise: the advertiser
// publishes nothing on the interface and logs no error. A finding is an
// error when none of the named interfaces carries an advertised address —
// nothing is announced at all — and a warning otherwise. An empty list
// reports nothing.
func DiagnoseInterfaces(allow []string) []Finding {
	if len(allow) == 0 {
		return nil
	}
	return diagnoseInterfaces(allow, hostIfaces())
}

// hostIfaces is the host's interfaces in the testable shape.
func hostIfaces() []hostIface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]hostIface, 0, len(ifaces))
	for _, ifi := range ifaces {
		out = append(out, hostIfaceOf(ifi))
	}
	return out
}

// diagnoseInterfaces is DiagnoseInterfaces over a given interface list.
func diagnoseInterfaces(allow []string, ifaces []hostIface) []Finding {
	var out []Finding
	advertised := false
	for _, name := range allow {
		i := slices.IndexFunc(ifaces, func(h hostIface) bool { return h.name == name })
		switch {
		case i < 0:
			out = append(out, Finding{
				Code: "interface_not_found",
				Message: "The advertiser is restricted to interface `" + name + "`, which this host does not have " +
					"(its interfaces: " + strings.Join(ifaceNames(ifaces), ", ") + "). Nothing is announced on it; " +
					"check the name.",
			})
		case DefaultInterfaceFilter(name):
			out = append(out, Finding{
				Code: "interface_filtered",
				Message: "The advertiser is restricted to interface `" + name + "`, a container or virtualisation " +
					"bridge whose addresses a controller on the LAN cannot reach; the default filter keeps it out " +
					"even when named. Name the host's LAN interface.",
			})
		case len(filterPrimaryHostIPs(ifaces[i:i+1], DefaultInterfaceFilter)) == 0:
			out = append(out, Finding{
				Code: "interface_no_addresses",
				Message: "The advertiser is restricted to interface `" + name + "`, which carries no address the " +
					"advertiser publishes (it is down, not multicast-capable, a loopback or tunnel, or has no " +
					"address). Nothing is announced on it.",
			})
		default:
			advertised = true
		}
	}
	severity := SeverityWarning
	if !advertised {
		severity = SeverityError
	}
	for i := range out {
		out[i].Severity = severity
	}
	return out
}

// ifaceNames is the names of ifaces, in order.
func ifaceNames(ifaces []hostIface) []string {
	out := make([]string, 0, len(ifaces))
	for _, h := range ifaces {
		out = append(out, h.name)
	}
	return out
}
