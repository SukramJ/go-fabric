// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
)

// extraElements is an OnOff server that also accepts command 0x77 and
// lists event 0x55, neither of which OnOff defines.
type extraElements struct{ *spec.Server }

func (e extraElements) MatterAcceptedCommands() []uint32 {
	return append(slices.Clone(e.Server.MatterAcceptedCommands()), 0x77)
}

func (e extraElements) MatterEvents() []uint32 {
	return append(slices.Clone(e.Server.MatterEvents()), 0x55)
}

// recordingTB records the errors a check reports instead of failing.
type recordingTB struct {
	testing.TB
	errs []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// TestCheckServerFlagsUndefinedCommandsAndEvents holds CheckServer to its
// doc: an accepted command the definition does not define as a request
// and a listed event it does not define are reported. Before, only
// attributes were held to the definition, so a server listing either
// passed (review II, R2-P6-test-1363).
func TestCheckServerFlagsUndefinedCommandsAndEvents(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	var base, extra recordingTB
	base.TB, extra.TB = t, t
	checkServer(&base, srv, onoffdef.Definition, 0, srv.MatterWrite, false)
	var withExtra contract.ClusterServer = extraElements{srv}
	checkServer(&extra, withExtra, onoffdef.Definition, 0, srv.MatterWrite, false)
	added := slices.DeleteFunc(slices.Clone(extra.errs), func(e string) bool { return slices.Contains(base.errs, e) })
	var cmd, ev bool
	for _, e := range added {
		cmd = cmd || strings.Contains(e, "command 0x77 is not a request")
		ev = ev || strings.Contains(e, "event 0x55 is not defined by the definition")
	}
	if !cmd || !ev {
		t.Errorf("CheckServer added %q over the baseline; want an error for command 0x77 and one for event 0x55", added)
	}
}
