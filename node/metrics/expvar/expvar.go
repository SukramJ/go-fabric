// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package expvarmetrics implements the node's metrics port
// ([contract.Metrics], node.Config.Metrics) on the standard library's
// expvar: each name is an [expvar.Int] published under prefix + name, so
// a daemon that serves net/http's /debug/vars (expvar's handler) shows
// the named set without code of its own and without a dependency beyond
// the standard library (ADR 0025 principle 12).
//
// The named set (contract.MetricNames) is published at zero by [New], so
// a dashboard sees every name before its first event; a name outside the
// set is published on its first use.
package expvarmetrics

import (
	"expvar"
	"sync"

	"github.com/SukramJ/go-fabric/contract"
)

// Metrics publishes the node's counters and gauges on expvar. It is safe
// for concurrent use.
type Metrics struct {
	prefix string
	// mu guards vars.
	mu   sync.Mutex
	vars map[string]*expvar.Int
}

var _ contract.Metrics = (*Metrics)(nil)

// New returns the port publishing under prefix ("fabric." gives
// "fabric.sessions.open"). expvar's names are process-wide: a second New
// with the same prefix shares the first one's variables rather than
// panicking on the duplicate.
func New(prefix string) *Metrics {
	m := &Metrics{prefix: prefix, vars: map[string]*expvar.Int{}}
	for _, name := range contract.MetricNames() {
		m.intVar(name)
	}
	return m
}

// Counter implements [contract.Metrics]: adds delta to prefix + name.
func (m *Metrics) Counter(name string, delta int64) { m.intVar(name).Add(delta) }

// Gauge implements [contract.Metrics]: sets prefix + name to value.
func (m *Metrics) Gauge(name string, value int64) { m.intVar(name).Set(value) }

// Value returns the current value of name — what /debug/vars shows for
// prefix + name.
func (m *Metrics) Value(name string) int64 { return m.intVar(name).Value() }

// intVar returns the variable of name, publishing it on first use. A
// variable of the same full name another New published is shared; one of
// another type under that name is shadowed by a private variable, since
// expvar cannot publish twice.
func (m *Metrics) intVar(name string) *expvar.Int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.vars[name]; ok {
		return v
	}
	full := m.prefix + name
	var v *expvar.Int
	switch existing := expvar.Get(full).(type) {
	case nil:
		v = expvar.NewInt(full)
	case *expvar.Int:
		v = existing
	default:
		v = new(expvar.Int)
	}
	m.vars[name] = v
	return v
}
