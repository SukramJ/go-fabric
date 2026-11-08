// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"maps"
	"strings"
)

// NameState is one cached name as a [Names.Watch] observer sees it: a
// snapshot, safe to keep.
type NameState struct {
	// QName is the name as first seen, without the trailing dot.
	QName string
	// Records are its records in installation order.
	Records []Record
	// Parameters are its TXT parameters.
	Parameters Parameters
	// Discovered is false once the name's last record went; it is the
	// last state the observer is given for that name until the name is
	// discovered again.
	Discovered bool
}

// SRVTarget is the target of the name's SRV record, "" without one.
func (s NameState) SRVTarget() string {
	for i := range s.Records {
		if s.Records[i].Type == TypeSRV {
			return s.Records[i].SRV.Target
		}
	}
	return ""
}

// Watch calls fn with the state of every cached name whose qname ends
// with one of suffixes, compared case-insensitively: when the name is
// discovered (a name already discovered at once), after each coalesced
// change of its records, and with Discovered false when its last record
// went — until the returned stop is called. fn runs on the cache's
// goroutine, one call at a time; it must not call back into the cache,
// whose methods wait for that goroutine. Watch admits no record: what is
// watched must already enter the cache (the ingress filter, a browse).
// Mirrors the observers matter.js's border-router registry keeps on the
// cache: one on names.discovered and one per instance name
// (packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:133-135,
// :235-237, :269-304 at db021da0).
func (n *Names) Watch(suffixes []string, fn func(NameState)) (stop func()) {
	lower := make([]string, len(suffixes))
	for i, s := range suffixes {
		lower[i] = strings.ToLower(s)
	}
	w := &watch{n: n, suffixes: lower, fn: fn, names: map[*Name]*nameObserver{}}
	if err := n.loop.call(w.start); err != nil {
		return func() {}
	}
	return func() { _ = n.loop.call(w.stop) }
}

// watch is one [Names.Watch]; its state is the loop's.
type watch struct {
	n        *Names
	suffixes []string
	fn       func(NameState)
	disc     *discoveredObserver
	names    map[*Name]*nameObserver
	stopped  bool
}

func (w *watch) start() {
	w.disc = w.n.onDiscovered(w.discovered)
	for _, nm := range w.n.discoveredNames() {
		w.discovered(nm)
	}
}

func (w *watch) stop() {
	if w.stopped {
		return
	}
	w.stopped = true
	w.n.offDiscovered(w.disc)
	for nm, o := range w.names {
		nm.off(o)
	}
	clear(w.names)
}

func (w *watch) matches(qname string) bool {
	lower := strings.ToLower(qname)
	for _, s := range w.suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

// discovered attaches an observer to a newly discovered name that
// matches and reports its state.
func (w *watch) discovered(nm *Name) {
	if w.stopped || !w.matches(nm.qname) {
		return
	}
	if _, ok := w.names[nm]; ok {
		return
	}
	o := &nameObserver{}
	o.fn = func(Changes) { w.changed(nm, o) }
	w.names[nm] = o
	nm.on(o)
	if !nm.notified {
		// A name the packet in progress discovered has its change batch
		// queued, which the observer now hears; report the others here.
		w.fn(w.state(nm))
	}
}

// changed reports a watched name's change; a name that lost its last
// record is reported once more and released.
func (w *watch) changed(nm *Name, o *nameObserver) {
	if w.stopped || w.names[nm] != o {
		return
	}
	st := w.state(nm)
	if !st.Discovered {
		delete(w.names, nm)
		nm.off(o)
	}
	w.fn(st)
}

func (w *watch) state(nm *Name) NameState {
	st := NameState{QName: nm.qname, Parameters: maps.Clone(nm.parameters()), Discovered: nm.isDiscovered()}
	for _, r := range nm.records.values() {
		st.Records = append(st.Records, *r)
	}
	return st
}
