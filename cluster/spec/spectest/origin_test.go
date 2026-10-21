// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// notifying is a server that reports its attribute changes.
type notifying struct {
	*fake
	cluster.AttributeChanges
}

func newNotifying(tb testing.TB) contract.ClusterServer {
	tb.Helper()
	inst, err := spec.New(serverDef, spec.Options{Features: 1})
	if err != nil {
		tb.Fatal(err)
	}
	return &notifying{fake: &fake{Instance: inst}}
}

// notify returns an Act that reports a change of attribute 0 from origin.
func notify(origins ...contract.Origin) func(tb testing.TB, srv contract.ClusterServer) {
	return func(tb testing.TB, srv contract.ClusterServer) {
		tb.Helper()
		n, ok := srv.(*notifying)
		if !ok {
			tb.Fatalf("%T is not the notifying server", srv)
		}
		for _, o := range origins {
			n.NotifyFrom(o, 0)
		}
	}
}

// TestCheckOriginsAcceptsAServerThatReportsTheWantedOrigin: a change from
// the wanted origin, and none where none is wanted, is a pass.
func TestCheckOriginsAcceptsAServerThatReportsTheWantedOrigin(t *testing.T) {
	t.Parallel()

	errs := run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newNotifying,
			spectest.OriginCase{Name: "host", Want: contract.OriginHostSet, Act: notify(contract.OriginHostSet, contract.OriginHostSet)},
			spectest.OriginCase{Name: "none", Act: notify()},
		)
	})
	if len(errs) != 0 {
		t.Errorf("a conformant audit fails: %q", errs)
	}
}

// TestCheckOriginsReportsWhatDiffers: a server that reports no change
// notifier, a change where none is wanted, none where one is wanted and a
// change from another origin are each reported.
func TestCheckOriginsReportsWhatDiffers(t *testing.T) {
	t.Parallel()

	wantReport(t, "no notifier", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, func(tb testing.TB) contract.ClusterServer {
			tb.Helper()
			inst, err := spec.New(serverDef, spec.Options{Features: 1})
			if err != nil {
				tb.Fatal(err)
			}
			return &fake{Instance: inst}
		}, spectest.OriginCase{Name: "x", Act: notify()})
	}), "reports no attribute change")

	wantReport(t, "an unwanted change", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newNotifying,
			spectest.OriginCase{Name: "refused write", Act: notify(contract.OriginHostSet)})
	}), "refused write: changes")

	wantReport(t, "a missing change", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newNotifying,
			spectest.OriginCase{Name: "silent set", Want: contract.OriginHostSet, Act: notify()})
	}), "silent set: no change reported")

	wantReport(t, "another origin", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newNotifying,
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: notify(contract.OriginCommand, contract.OriginHostSet)})
	}), "command: change [0] from")
}

// TestCheckDefinitionReportsAnEventPriority: an event whose priority is
// not the snapshot's is reported.
func TestCheckDefinitionReportsAnEventPriority(t *testing.T) {
	t.Parallel()

	d := clone(opdef.Definition)
	d.Events[0].Priority = spec.PriorityDebug
	wantReport(t, "priority", run(t, func(tb *recorder) { spectest.CheckDefinition(tb, d) }), "priority")
}

// TestRoundTripHelpersReportAMissingElement: the command, event and
// attribute helpers stop on an element the definition does not have, and
// the encoders stop on bytes that are not TLV.
func TestRoundTripHelpersReportAMissingElement(t *testing.T) {
	t.Parallel()

	good := broken{v: 1}
	wantReport(t, "command", run(t, func(tb *recorder) {
		spectest.RoundTripCommand(tb, opdef.Definition, 0x7F, spec.Request, good)
	}), "no command 0x7F")
	wantReport(t, "event", run(t, func(tb *recorder) {
		spectest.RoundTripEvent(tb, opdef.Definition, 0x7F, good)
	}), "no event 0x7F")
	wantReport(t, "attribute", run(t, func(tb *recorder) {
		spectest.RoundTripAttribute(tb, opdef.Definition, 0x7F7F, good)
	}), "no attribute 0x7F7F")
	wantReport(t, "decode", run(t, func(tb *recorder) {
		spectest.Decode(tb, []byte{0xFF})
	}), "decode")
}
