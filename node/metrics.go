// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import "github.com/SukramJ/go-fabric/contract"

// Metrics is the port [Config].Metrics takes ([contract.Metrics]): the
// host's counters and gauges. A host's implementation imports contract
// only; node/metrics/expvar is the stdlib implementation the module
// ships. Nil records nothing.
type Metrics = contract.Metrics

// The named metric set the node reports to [Config].Metrics — the
// contract's names, here so a host reads them where it configures the
// port. docs/guides/observability.md tables them with their sources.
const (
	// MetricSessionsOpen is a gauge: the entries of the secure-session
	// table (PASE and CASE), set whenever a session is established or
	// closed.
	MetricSessionsOpen = contract.MetricSessionsOpen
	// MetricSubscriptionsActive is a gauge: the subscriptions the node
	// serves, set whenever one is established, restored or closed.
	MetricSubscriptionsActive = contract.MetricSubscriptionsActive
	// MetricIMReceived is a counter: one Interaction Model message
	// received (GeneralDiagnostics DeviceLoadStatus
	// TotalInteractionModelMessagesReceived).
	MetricIMReceived = contract.MetricIMReceived
	// MetricIMSent is a counter: one Interaction Model message sent
	// (DeviceLoadStatus TotalInteractionModelMessagesSent).
	MetricIMSent = contract.MetricIMSent
	// MetricPASEFailures is a counter: one failed PASE pairing attempt.
	MetricPASEFailures = contract.MetricPASEFailures
	// MetricPASELockouts is a counter: one PASE lockout engaged after the
	// failure cap.
	MetricPASELockouts = contract.MetricPASELockouts
	// MetricAggregatorReassemblies is a counter: one completed reassembly
	// of the bridged-device topology ([Aggregator]).
	MetricAggregatorReassemblies = contract.MetricAggregatorReassemblies
	// MetricMRPRetransmits is a counter: one reliable message sent again,
	// by the device side or the node's client exchanges.
	MetricMRPRetransmits = contract.MetricMRPRetransmits
	// MetricPanicsRecovered is a counter: one panic recovered on a
	// datagram or connection path of the node's sockets.
	MetricPanicsRecovered = contract.MetricPanicsRecovered
)

// count adds one to the counter name of the configured port.
func (n *Node) count(name string) {
	if n.cfg.Metrics != nil {
		n.cfg.Metrics.Counter(name, 1)
	}
}

// panicRecovered is the hook of the node's own sockets (the client and the
// group sender): one recovered panic.
func (n *Node) panicRecovered() { n.count(MetricPanicsRecovered) }

// retransmitted is the client exchanges' hook: one retransmission.
func (n *Node) retransmitted() { n.count(MetricMRPRetransmits) }
