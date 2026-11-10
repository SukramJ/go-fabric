// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import "github.com/SukramJ/go-fabric/contract"

// The named metric set the controller reports to [Config].Metrics — the
// contract's names, here so a host reads them where it configures the
// port. docs/guides/observability.md tables them with their sources.
const (
	// MetricSessionsOpen is a gauge: the secure sessions of the
	// controller's peers, set whenever one is established or leaves its
	// peer.
	MetricSessionsOpen = contract.MetricSessionsOpen
	// MetricMRPRetransmits is a counter: one reliable message the
	// controller's exchanges sent again.
	MetricMRPRetransmits = contract.MetricMRPRetransmits
	// MetricPairFailures is a counter: one failed pairing step, the last
	// report of a failed [Controller.Pair].
	MetricPairFailures = contract.MetricPairFailures
	// MetricCASEUnresponsive is a counter: one CASE establishment that
	// retransmitted past its MRP budget unanswered.
	MetricCASEUnresponsive = contract.MetricCASEUnresponsive
	// MetricReachabilityProbes is a counter: one reachability probe run
	// of a peer, whatever its verdict.
	MetricReachabilityProbes = contract.MetricReachabilityProbes
	// MetricICDCheckInsMissed is a counter: one ICD Check-In expected
	// from a registered LIT peer and not received.
	MetricICDCheckInsMissed = contract.MetricICDCheckInsMissed
)

// count adds one to the counter name of the configured port.
func (c *Controller) count(name string) {
	if c.metrics != nil {
		c.metrics.Counter(name, 1)
	}
}

// gauge sets the gauge name of the configured port.
func (c *Controller) gauge(name string, value int64) {
	if c.metrics != nil {
		c.metrics.Gauge(name, value)
	}
}

// retransmitted is the exchanges' hook: one retransmission.
func (c *Controller) retransmitted() { c.count(MetricMRPRetransmits) }

// gaugeSessions sets sessions.open to the sessions of every peer — read
// from the set, so a peer that left takes its sessions with it.
func (c *Controller) gaugeSessions() {
	if c.metrics == nil {
		return
	}
	set := c.peerSet.Load()
	if set == nil {
		return
	}
	var n int64
	for _, p := range set.Peers() {
		n += int64(len(p.Sessions()))
	}
	c.gauge(MetricSessionsOpen, n)
}
