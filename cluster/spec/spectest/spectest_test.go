// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/tlv"
)

// recorder collects what a check reports instead of failing the test.
type recorder struct {
	testing.TB
	errs []string
}

func (*recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// run runs check against a recorder and returns what it reported.
func run(t *testing.T, check func(r *recorder)) []string {
	t.Helper()
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		check(r)
	}()
	<-done
	return r.errs
}

func wantReport(t *testing.T, what string, errs []string, substr string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return
		}
	}
	t.Errorf("%s: no report containing %q in %q", what, substr, errs)
}

// clone copies a definition deeply enough to mutate its element slices.
func clone(d *spec.Cluster) *spec.Cluster {
	c := *d
	c.Features = append([]spec.Feature(nil), d.Features...)
	c.Attributes = append([]spec.Attribute(nil), d.Attributes...)
	c.Commands = append([]spec.Command(nil), d.Commands...)
	c.Events = append([]spec.Event(nil), d.Events...)
	return &c
}

func TestCheckDefinition(t *testing.T) {
	t.Parallel()
	if errs := run(t, func(tb *recorder) { spectest.CheckDefinition(tb, rvcrunmode.Definition) }); len(errs) != 0 {
		t.Fatalf("the generated definition fails: %q", errs)
	}
	mutations := []struct {
		name   string
		mutate func(*spec.Cluster)
		report string
	}{
		{"revision", func(c *spec.Cluster) { c.Revision++ }, "identity"},
		{"feature count", func(c *spec.Cluster) { c.Features = c.Features[:1] }, "features, snapshot"},
		{"feature bit", func(c *spec.Cluster) { c.Features[1].Bit = 3 }, "bit 3"},
		{"feature title", func(c *spec.Cluster) { c.Features[1].Title = "X" }, "feature DIRECTMODECH"},
		{"attribute missing", func(c *spec.Cluster) { c.Attributes = c.Attributes[1:] }, "attribute 0x0000"},
		{"attribute type", func(c *spec.Cluster) { c.Attributes[1].Type.Name = "uint16" }, "type"},
		{"attribute quality", func(c *spec.Cluster) { c.Attributes[1].Quality.Fixed = true }, "quality fixed"},
		{"attribute conformance", func(c *spec.Cluster) { c.Attributes[1].Conformance.Text = "O" }, "conformance"},
		{"attribute access", func(c *spec.Cluster) { c.Attributes[1].Access.Write = spec.PrivilegeManage }, "access"},
		{"extra attribute", func(c *spec.Cluster) { c.Attributes = append(c.Attributes, spec.Attribute{ID: 99}) }, "attributes, snapshot"},
		{"command missing", func(c *spec.Cluster) { c.Commands = c.Commands[1:] }, "command 0x00"},
		{"command response", func(c *spec.Cluster) { c.Commands[0].Response = "x" }, "response"},
		{"command decoder", func(c *spec.Cluster) { c.Commands[0].Decode = nil }, "decoder"},
		{"extra event", func(c *spec.Cluster) { c.Events = append(c.Events, spec.Event{ID: 1}) }, "events, snapshot"},
	}
	for _, m := range mutations {
		d := clone(rvcrunmode.Definition)
		m.mutate(d)
		wantReport(t, m.name, run(t, func(tb *recorder) { spectest.CheckDefinition(tb, d) }), m.report)
	}
}

func TestCheckDefinitionEvents(t *testing.T) {
	t.Parallel()
	// A definition of a cluster with events: OperationalState (0x0060).
	d := &spec.Cluster{ID: 0x0060, Name: "OperationalState", Revision: 4}
	errs := run(t, func(tb *recorder) { spectest.CheckDefinition(tb, d) })
	wantReport(t, "missing events", errs, "event 0x00 OperationalError missing")
}

// fake is a server on a spec.Instance whose answers a test can spoil.
type fake struct {
	*spec.Instance
	featureMap  any
	attrs       []uint32
	generated   []uint32
	writeErr    error
	writePriv   uint8
	invokePriv  uint8
	events      []uint32
	spoilWrites bool
}

func (f *fake) MatterRead(id uint32) (any, bool) {
	if id == spec.AttrFeatureMap && f.featureMap != nil {
		return f.featureMap, true
	}
	return f.ReadGlobal(id)
}

func (f *fake) MatterWrite(_ context.Context, id uint32, v any) error {
	if f.spoilWrites {
		return f.writeErr
	}
	_, err := f.ValidateWrite(id, v, nil)
	return err
}

func (*fake) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

func (f *fake) MatterAttributes() []uint32 {
	if f.attrs != nil {
		return f.attrs
	}
	return f.Instance.MatterAttributes()
}

func (f *fake) MatterGeneratedCommands() []uint32 {
	if f.generated != nil {
		return f.generated
	}
	return f.Instance.MatterGeneratedCommands()
}

func (f *fake) MatterEvents() []uint32 {
	if f.events != nil {
		return f.events
	}
	return f.Instance.MatterEvents()
}

func (f *fake) MinWritePrivilege(id uint32) uint8 {
	if f.writePriv != 0 {
		return f.writePriv
	}
	return f.Instance.MinWritePrivilege(id)
}

func (f *fake) MinInvokePrivilege(id uint32) uint8 {
	if f.invokePriv != 0 {
		return f.invokePriv
	}
	return f.Instance.MinInvokePrivilege(id)
}

var serverDef = &spec.Cluster{
	ID: 0xFFF3, Name: "Server", Revision: 2,
	Features: []spec.Feature{{Name: "F", Title: "Feat", Bit: 0, Conformance: spec.Conformance{Op: spec.ConfOptional}}},
	Attributes: []spec.Attribute{
		{ID: 0, Name: "RO", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Op: spec.ConfMandatory}, Access: spec.Access{RW: "R"}},
		{ID: 1, Name: "RW", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Op: spec.ConfMandatory}, Access: spec.Access{RW: "RW", Write: spec.PrivilegeManage}},
		{ID: 2, Name: "Never", Conformance: spec.Conformance{Op: spec.ConfDisallowed}},
	},
	Commands: []spec.Command{
		{ID: 0, Name: "Do", Direction: spec.Request, Response: "DoResponse", Conformance: spec.Conformance{Op: spec.ConfMandatory}},
		{ID: 1, Name: "DoResponse", Direction: spec.Response, Conformance: spec.Conformance{Op: spec.ConfMandatory}},
	},
	Events: []spec.Event{{ID: 0, Name: "Happened", Conformance: spec.Conformance{Op: spec.ConfMandatory}}},
}

func TestCheckServer(t *testing.T) {
	t.Parallel()
	newFake := func() *fake {
		inst, err := spec.New(serverDef, spec.Options{Features: 1})
		if err != nil {
			t.Fatal(err)
		}
		return &fake{Instance: inst}
	}
	if errs := run(t, func(tb *recorder) { spectest.CheckServer(tb, newFake(), serverDef, 1) }); len(errs) != 0 {
		t.Fatalf("a conformant server fails: %q", errs)
	}
	spoils := []struct {
		name  string
		spoil func(*fake)
		want  string
	}{
		{"FeatureMap", func(f *fake) { f.featureMap = uint32(0) }, "FeatureMap"},
		{"missing attribute", func(f *fake) { f.attrs = []uint32{1} }, "mandatory attribute RO missing"},
		{"disallowed attribute", func(f *fake) { f.attrs = []uint32{0, 1, 2} }, "attribute Never listed but disallowed"},
		{"undefined attribute", func(f *fake) { f.attrs = []uint32{0, 1, 7} }, "undefined attribute"},
		{"read-only write", func(f *fake) { f.spoilWrites = true }, "UNSUPPORTED_WRITE"},
		{"write privilege", func(f *fake) { f.writePriv = 3 }, "write privilege"},
		{"generated list", func(f *fake) { f.generated = []uint32{} }, "GeneratedCommandList"},
		{"invoke privilege", func(f *fake) { f.invokePriv = 5 }, "invoke privilege"},
		{"events", func(f *fake) { f.events = []uint32{} }, "mandatory event Happened missing"},
	}
	for _, s := range spoils {
		f := newFake()
		s.spoil(f)
		wantReport(t, s.name, run(t, func(tb *recorder) { spectest.CheckServer(tb, f, serverDef, 1) }), s.want)
	}
	other := &spec.Cluster{ID: 0xFFF4, Name: "Other", Revision: 9}
	errs := run(t, func(tb *recorder) { spectest.CheckServer(tb, newFake(), other, 0) })
	wantReport(t, "cluster id", errs, "cluster id")
	wantReport(t, "revision", errs, "ClusterRevision")
}

// broken encodes a payload its decoder rejects or reads differently.
type broken struct{ v uint8 }

func (b broken) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(b.v))
	_ = enc.EndContainer()
}

func (*broken) ResponseCommand() (clusterID, commandID uint32) { return 0, 0 }

func TestRoundTrips(t *testing.T) {
	t.Parallel()
	good := rvcrunmode.ChangeToModeRequest{NewMode: 2}
	if errs := run(t, func(tb *recorder) {
		spectest.RoundTrip(tb, good, spec.DecodeStruct[rvcrunmode.ChangeToModeRequest])
		spectest.RoundTripCommand(tb, rvcrunmode.Definition, rvcrunmode.CmdChangeToMode, spec.Request, good)
		spectest.RoundTripCommand(tb, rvcrunmode.Definition, rvcrunmode.CmdChangeToModeResponse, spec.Response,
			rvcrunmode.ChangeToModeResponse{Status: rvcrunmode.ModeChangeStatusStuck, StatusText: "s"})
		spectest.RoundTripAttribute(tb, rvcrunmode.Definition, rvcrunmode.AttrSupportedModes, spec.List[rvcrunmode.ModeOptionStruct]{
			{Label: "a", Mode: 1, ModeTags: []rvcrunmode.ModeTagStruct{{Value: rvcrunmode.ModeTagIdle}}},
			{Label: "b", Mode: 2, ModeTags: []rvcrunmode.ModeTagStruct{{Value: rvcrunmode.ModeTagCleaning}}},
		})
	}); len(errs) != 0 {
		t.Fatalf("good round trips fail: %q", errs)
	}
	cases := []struct {
		name  string
		check func(*recorder)
		want  string
	}{
		{"decode error", func(tb *recorder) {
			spectest.RoundTrip(tb, broken{v: 255}, func(n spec.Node) (broken, error) {
				_, err := spec.DecodeUintIn[uint8](8, 0, 1)(n.Children[0])
				return broken{}, err
			})
		}, "decode"},
		{"mismatch", func(tb *recorder) {
			spectest.RoundTrip(tb, broken{v: 1}, func(spec.Node) (broken, error) { return broken{v: 2}, nil })
		}, "round trip"},
		{"unknown command", func(tb *recorder) {
			spectest.RoundTripCommand(tb, rvcrunmode.Definition, 9, spec.Request, good)
		}, "no command"},
		{"command decode error", func(tb *recorder) {
			spectest.RoundTripCommand(tb, rvcrunmode.Definition, rvcrunmode.CmdChangeToMode, spec.Request, broken{v: 1})
		}, "round trip"},
		{"not a response", func(tb *recorder) {
			spectest.RoundTripCommand(tb, rvcrunmode.Definition, rvcrunmode.CmdChangeToModeResponse, spec.Response,
				rvcrunmode.ChangeToModeRequest{})
		}, "no response payload"},
		{"wrong response", func(tb *recorder) {
			spectest.RoundTripCommand(tb, rvcrunmode.Definition, rvcrunmode.CmdChangeToModeResponse, spec.Response, &broken{})
		}, "names response"},
		{"unknown event", func(tb *recorder) { spectest.RoundTripEvent(tb, rvcrunmode.Definition, 0, good) }, "no event"},
		{"unknown attribute", func(tb *recorder) { spectest.RoundTripAttribute(tb, rvcrunmode.Definition, 99, good) }, "no attribute"},
		{"no decoder", func(tb *recorder) {
			spectest.RoundTripAttribute(tb, rvcrunmode.Definition, rvcrunmode.AttrCurrentMode, good)
		}, "no decoder"},
		{"undecodable bytes", func(tb *recorder) { spectest.Decode(tb, []byte{0x15}) }, "decode"},
	}
	for _, c := range cases {
		wantReport(t, c.name, run(t, c.check), c.want)
	}
	if *spectest.Ptr(3) != 3 {
		t.Error("Ptr")
	}
}
