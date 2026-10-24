// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// Metrics is the port a host implements to receive the module's
// operational counters and gauges: a Prometheus or OpenTelemetry adapter,
// the stdlib expvar one the module ships
// (github.com/SukramJ/go-fabric/node/metrics/expvar), or a test's fake.
// The module never imports a metrics library (ADR 0025 principle 12).
//
// The names are the Metric… constants of this package; each states what
// one unit is. A name carries no attributes in this release: a per-fabric
// or per-peer split is a later decision, and an implementation that needs
// one today derives it from the logs (docs/guides/observability.md).
//
// Calls arrive on the goroutine that observed the event — the receive
// path, a timer, the MRP pump — so an implementation must be safe for
// concurrent use and must not block: a slow Counter delays the datagram
// that triggered it. A nil Metrics records nothing at the cost of a nil
// check.
type Metrics interface {
	// Counter adds delta (at least one) to the monotonic counter name.
	Counter(name string, delta int64)
	// Gauge sets the gauge name to value, the current level.
	Gauge(name string, value int64)
}

// The named metric set. The names are spec-near — the Matter terms for
// what is counted — not the names of matter.js internals, so a dashboard
// survives a refactor of the code that feeds it.
const (
	// MetricSessionsOpen is a gauge: the entries of the secure-session
	// table (PASE and CASE), set whenever a session is established or
	// closed.
	MetricSessionsOpen = "sessions.open"
	// MetricSubscriptionsActive is a gauge: the subscriptions the node
	// serves, set whenever one is established, restored or closed.
	MetricSubscriptionsActive = "subscriptions.active"
	// MetricIMReceived is a counter: one Interaction Model message
	// received — the count GeneralDiagnostics DeviceLoadStatus reports as
	// TotalInteractionModelMessagesReceived.
	MetricIMReceived = "im.received"
	// MetricIMSent is a counter: one Interaction Model message sent — the
	// count DeviceLoadStatus reports as TotalInteractionModelMessagesSent.
	MetricIMSent = "im.sent"
	// MetricPASEFailures is a counter: one PASE pairing attempt that
	// failed (a wrong passcode, a broken handshake), not a retransmitted
	// step.
	MetricPASEFailures = "pase.failures"
	// MetricPASELockouts is a counter: one PASE lockout engaged after the
	// failure cap was reached; the commissioning window closes with it.
	MetricPASELockouts = "pase.lockouts"
	// MetricAggregatorReassemblies is a counter: one completed
	// reassembly of the bridged-device topology under the Aggregator.
	MetricAggregatorReassemblies = "aggregator.reassemblies"
	// MetricMRPRetransmits is a counter: one reliable message sent again
	// because its acknowledgement did not arrive in time (Matter §4.12).
	MetricMRPRetransmits = "mrp.retransmits"
	// MetricPanicsRecovered is a counter: one panic recovered on the
	// datagram or connection path; the message that caused it was
	// dropped.
	MetricPanicsRecovered = "panics.recovered"
)

// MetricNames are the names of the named set, in the order the guide's
// table lists them.
func MetricNames() []string {
	return []string{
		MetricSessionsOpen, MetricSubscriptionsActive, MetricIMReceived, MetricIMSent,
		MetricPASEFailures, MetricPASELockouts, MetricAggregatorReassemblies,
		MetricMRPRetransmits, MetricPanicsRecovered,
	}
}
