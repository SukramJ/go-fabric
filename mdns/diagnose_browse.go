// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import "strconv"

// CodeNoOperationalNames is the code of the browse-side finding a
// controller reports ([DiagnoseOperationalBrowse]): it waits for a
// commissioned node's operational record and its DNS-SD cache holds none
// of its nodes' records — the controller's browse gets no answer on the
// network it runs on (a multicast filter, another VLAN, a container
// without the host's network). A Warning; its Service is empty.
const CodeNoOperationalNames = "no_operational_names"

// DiagnoseOperationalBrowse is the controller's browse-side check: with
// commissioned nodes of which waiting wait for device discovery and
// discovered have an operational record in the DNS-SD cache, it reports
// [CodeNoOperationalNames] when at least one waits and none is
// discovered, and nothing otherwise. There is no timer and no grace: the
// waiting state is the signal.
func DiagnoseOperationalBrowse(commissioned, waiting, discovered int) []Finding {
	if waiting == 0 || discovered > 0 {
		return nil
	}
	return []Finding{{
		Severity: SeverityWarning,
		Code:     CodeNoOperationalNames,
		Message: "None of the " + strconv.Itoa(commissioned) + " commissioned nodes has an operational record in the " +
			"DNS-SD cache; the controller's browse gets no answer on this network.",
	}}
}
