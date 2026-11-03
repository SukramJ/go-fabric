// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	scenesdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
)

// Parity with matter.js
// packages/node/test/behavior/cluster/ValidatedElementsTest.ts at
// 862ad75f. matter.js builds a behavior class from a schema and reads
// which elements it implements (makeCluster, makeBehaviorType :30-160);
// the Go counterpart is a synthetic spec.Cluster, the optional elements a
// server declares (spec.Options) and the command handlers it registers
// (spec.ServerConfig). An element whose conformance names another
// element is present when the server declares it.

// veCluster is makeCluster (:30-104): commands, attributes and events of
// the given conformance, features FT (bit 0) and OT (bit 1).
func veCluster(t *testing.T, commands, attributes map[string]veElement) *spec.Cluster {
	t.Helper()
	def := &spec.Cluster{
		ID: 0xFFF1_FC01, Name: "TestCluster", Revision: 1,
		Features: []spec.Feature{
			{Name: "FT", Title: "Feature", Bit: 0, Conformance: op(spec.ConfOptional)},
			{Name: "OT", Title: "Other", Bit: 1, Conformance: op(spec.ConfOptional)},
		},
	}
	for _, n := range sortedKeys(attributes) {
		e := attributes[n]
		def.Attributes = append(def.Attributes, spec.Attribute{
			ID: e.id, Name: n, Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: specConformance(t, e.conformance),
			Access: spec.Access{RW: "R", Read: spec.PrivilegeView},
		})
	}
	for _, n := range sortedKeys(commands) {
		e := commands[n]
		dir := spec.Request
		if e.response {
			dir = spec.Response
		}
		c := spec.Command{ID: e.id, Name: n, Direction: dir, Conformance: specConformance(t, e.conformance), Access: spec.Access{Write: spec.PrivilegeOperate}}
		if !e.response {
			c.Response = "status"
		}
		def.Commands = append(def.Commands, c)
	}
	return def
}

type veElement struct {
	id          uint32
	conformance string
	response    bool
}

func sortedKeys(m map[string]veElement) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b string) int { return int(m[a].id) - int(m[b].id) })
	return out
}

func TestValidatedElementsParityMatterJS(t *testing.T) {
	t.Parallel()
	const cmdA, cmdB, cmdC, cmdD = 1, 2, 3, 4

	// :217-251: CmdB "CmdA, O" with CmdA disallowed falls back to O.
	fallback := func(t *testing.T) *spec.Cluster {
		return veCluster(t, map[string]veElement{"CmdA": {id: cmdA, conformance: "X"}, "CmdB": {id: cmdB, conformance: "CmdA, O"}}, nil)
	}
	t.Run("optional fallback when dep absent and element implemented", func(t *testing.T) {
		inst, err := spec.New(fallback(t), spec.Options{Commands: []uint32{cmdB}})
		if err != nil || !inst.Accepts(cmdB) {
			t.Fatalf("Accepts(CmdB) = %v, err %v", err == nil && inst.Accepts(cmdB), err)
		}
	})
	t.Run("optional fallback when dep absent and element not implemented", func(t *testing.T) {
		inst, err := spec.New(fallback(t), spec.Options{})
		if err != nil || inst.Accepts(cmdB) {
			t.Fatalf("CmdB present or err %v", err)
		}
	})

	// :253-289: a cyclic pair "CmdB, O" / "CmdA, O".
	cyclic := func(t *testing.T) *spec.Cluster {
		return veCluster(t, map[string]veElement{"CmdA": {id: cmdA, conformance: "CmdB, O"}, "CmdB": {id: cmdB, conformance: "CmdA, O"}}, nil)
	}
	t.Run("cyclic pair both implemented", func(t *testing.T) {
		inst, err := spec.New(cyclic(t), spec.Options{Commands: []uint32{cmdA, cmdB}})
		if err != nil || !inst.Accepts(cmdA) || !inst.Accepts(cmdB) {
			t.Fatalf("pair not both present (err %v)", err)
		}
	})
	t.Run("cyclic pair neither implemented", func(t *testing.T) {
		inst, err := spec.New(cyclic(t), spec.Options{})
		if err != nil || inst.Accepts(cmdA) || inst.Accepts(cmdB) {
			t.Fatalf("pair present (err %v)", err)
		}
	})

	// :329-355: OperationalState's Pause/Resume/Stop/Start, every one
	// implemented.
	t.Run("OperationalState-like scenario", func(t *testing.T) {
		def := veCluster(t, map[string]veElement{
			"Pause": {id: 1, conformance: "Resume, O"}, "Resume": {id: 2, conformance: "Pause, O"},
			"Stop": {id: 3, conformance: "M"}, "Start": {id: 4, conformance: "Stop"},
			"OperationalCommandResponse": {id: 5, conformance: "Pause | Stop | Start | Resume", response: true},
		}, nil)
		handlers := map[uint32]spec.CommandHandler{1: noop, 2: noop, 3: noop, 4: noop}
		srv, err := spec.NewServer(def, spec.Options{Commands: []uint32{1, 2, 4}}, spec.ServerConfig{Handlers: handlers})
		if err != nil {
			t.Fatal(err)
		}
		if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{1, 2, 3, 4}) {
			t.Errorf("AcceptedCommandList %v", got)
		}
	})

	// :357-374: a mandatory command left as Behavior.unimplemented is a
	// warning, not fatal — the Go stub (ServerConfig.Unimplemented), which
	// UnimplementedMandatoryCommands names and the device-type validator
	// reports (ViolationUnimplemented).
	t.Run("errors when mandatory element not implemented", func(t *testing.T) {
		def := veCluster(t, map[string]veElement{"CmdA": {id: cmdA, conformance: "M"}, "CmdB": {id: cmdB, conformance: "M"}}, nil)
		srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Unimplemented: true, Handlers: map[uint32]spec.CommandHandler{cmdA: noop}})
		if err != nil {
			t.Fatalf("a stub is fatal: %v", err)
		}
		if got := srv.UnimplementedMandatoryCommands(); !slices.Equal(got, []uint32{cmdB}) {
			t.Errorf("unimplemented %v, want [CmdB]", got)
		}
	})

	// :430-446: "Z, CmdA" reads as "X, CmdA" and is not fatal.
	t.Run("reads a misplaced obsolete term in an otherwise list as it reads a disallowed one", func(t *testing.T) {
		resultFor := func(flag string) (bool, error) {
			def := veCluster(t, map[string]veElement{"CmdA": {id: cmdA, conformance: "O"}, "CmdB": {id: cmdB, conformance: flag + ", CmdA"}}, nil)
			inst, err := spec.New(def, spec.Options{Commands: []uint32{cmdA, cmdB}})
			if err != nil {
				return false, err
			}
			return inst.Accepts(cmdB), nil
		}
		z, zErr := resultFor("Z")
		x, xErr := resultFor("X")
		if z != x || (zErr == nil) != (xErr == nil) {
			t.Errorf("Z: %v %v, X: %v %v", z, zErr, x, xErr)
		}
		if zErr != nil {
			t.Errorf("Z fatal: %v", zErr)
		}
	})

	// :448-464: AttrB "AttrA" beside a mandatory AttrA.
	t.Run("handles attributes with element references", func(t *testing.T) {
		def := veCluster(t, nil, map[string]veElement{"AttrA": {id: 1, conformance: "M"}, "AttrB": {id: 2, conformance: "AttrA"}})
		inst, err := spec.New(def, spec.Options{Attributes: []uint32{2}})
		if err != nil || !inst.Serves(1) || !inst.Serves(2) {
			t.Fatalf("attributes not both served (err %v)", err)
		}
	})

	// "unimplemented mandatory commands" (:496-592): CmdA M, CmdB FT
	// (supported), CmdC OT, CmdD "CmdA".
	mandatory := func(t *testing.T) *spec.Cluster {
		return veCluster(t, map[string]veElement{
			"CmdA": {id: cmdA, conformance: "M"}, "CmdB": {id: cmdB, conformance: "FT"},
			"CmdC": {id: cmdC, conformance: "OT"}, "CmdD": {id: cmdD, conformance: "CmdA"},
		}, nil)
	}
	ft := spec.Options{Features: 1 << 0, Commands: []uint32{cmdD}}

	// :528-544: a server that leaves its mandatory commands without a
	// handler is refused naming each, not the first alone — Go's strict
	// form is a server without ServerConfig.Unimplemented. matter.js
	// requires CmdD once CmdA is implemented; a Go element whose
	// conformance names another is optional when declared.
	t.Run("is fatal for each mandatory command that throws unimplemented in strict mode", func(t *testing.T) {
		_, err := spec.NewServer(mandatory(t), ft, spec.ServerConfig{})
		if !errors.Is(err, spec.ErrMissingHandler) {
			t.Fatalf("err = %v, want ErrMissingHandler", err)
		}
		for _, name := range []string{"CmdA", "CmdB"} {
			if !strings.Contains(err.Error(), "command "+name+" ") {
				t.Errorf("%q does not name %s", err, name)
			}
		}
		if strings.Contains(err.Error(), "CmdC") {
			t.Errorf("%q names CmdC, whose feature is off", err)
		}
	})

	// :557-566: CmdB, required by the supported feature FT.
	t.Run("is fatal for a conditionally required command without implementation", func(t *testing.T) {
		_, err := spec.NewServer(mandatory(t), ft, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{cmdA: noop}})
		if !errors.Is(err, spec.ErrMissingHandler) || !strings.Contains(err.Error(), "CmdB") {
			t.Fatalf("err = %v, want ErrMissingHandler naming CmdB", err)
		}
	})

	// :580-582: the default Groups, ScenesManagement and GroupKeyManagement
	// servers — here their definitions — are accepted as they stand.
	t.Run("accepts the default ${server.name} in strict mode", func(t *testing.T) {
		for _, def := range []*spec.Cluster{groupsdef.Definition, scenesdef.Definition, gkmdef.Definition} {
			if _, err := spec.New(def, spec.Options{}); err != nil {
				t.Errorf("%s: %v", def.Name, err)
			}
		}
	})

	// :585-592.
	t.Run("accepts a cluster that implements its mandatory commands in strict mode", func(t *testing.T) {
		srv, err := spec.NewServer(mandatory(t), ft, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{cmdA: noop, cmdB: noop, cmdD: noop}})
		if err != nil {
			t.Fatal(err)
		}
		if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{cmdA, cmdB, cmdD}) {
			t.Errorf("AcceptedCommandList %v", got)
		}
	})

	// "element IDs" (:594-668).
	t.Run("refuses illegal attribute, command and event IDs together", func(t *testing.T) { // :596-611
		def := veCluster(t, map[string]veElement{"IllegalCommand": {id: 0xFFF1_0100, conformance: "O"}},
			map[string]veElement{"IllegalAttribute": {id: 0xFFF1_5000, conformance: "O"}})
		def.Events = []spec.Event{{ID: 0xFFF1_0100, Name: "IllegalEvent", Priority: spec.PriorityInfo, Conformance: op(spec.ConfOptional)}}
		_, err := spec.New(def, spec.Options{})
		if !errors.Is(err, spec.ErrIllegalID) {
			t.Fatalf("err = %v, want ErrIllegalID", err)
		}
		for _, want := range []string{
			"TestCluster.IllegalAttribute: spec: illegal element id: invalid attribute ID 0xfff15000",
			"TestCluster.IllegalCommand: spec: illegal element id: invalid command ID 0xfff10100",
			"TestCluster.IllegalEvent: spec: illegal element id: invalid event ID 0xfff10100",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q lacks %q", err, want)
			}
		}
	})
	t.Run("refuses an illegal cluster ID", func(t *testing.T) { // :613-621
		_, err := spec.New(&spec.Cluster{ID: 0xFFF1_0001, Name: "IllegalMei", Revision: 1}, spec.Options{})
		if !errors.Is(err, spec.ErrIllegalID) || !strings.Contains(err.Error(), "IllegalMei.cluster") || !strings.Contains(err.Error(), "invalid cluster ID 0xfff10001") {
			t.Fatalf("err = %v", err)
		}
	})
}
