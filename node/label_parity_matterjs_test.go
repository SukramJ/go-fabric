// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The ports of matter.js's label server tests, by meaning, on fabrictest:
// packages/node/test/behaviors/user-label/UserLabelServerTest.ts at
// db021da0 (TestUserLabelServerParity) and
// packages/node/test/behaviors/fixed-label/FixedLabelServerTest.ts at
// db021da0 (TestFixedLabelServerParity). Both drive a commissioned
// device's OnOffLight over the wire — REPLACE_ALL, REPLACE_ALL followed by
// ADD, hand-made TLV — so they live here and not beside the servers in
// cluster/core: the list-append and BUSY rules are the Interaction Model's
// and the endpoint dispatcher's, not the server's.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// userLabels is labels (UserLabelServerTest.ts:30-32 at db021da0).
func userLabels(count int) []userlabeldef.LabelStruct {
	out := make([]userlabeldef.LabelStruct, count)
	for i := range out {
		out[i] = userlabeldef.LabelStruct{Label: fmt.Sprintf("label%d", i), Value: fmt.Sprintf("value%d", i)}
	}
	return out
}

// labelPair is labelDevice (UserLabelServerTest.ts:43-80 at db021da0): a
// commissioned device whose OnOffLight carries the cluster under test, and
// the ways to write and read it as a controller does.
type labelPair struct {
	*mockPair
	ep uint16
}

// newLabelPair commissions a device serving a light with decl. The light
// is added on every start of the node (Topology.Prepare), so a test that
// replaces lp.light while the device is down restarts it with a new light
// of the same key — matter.js's site.addNode with a new
// lightWithLabels() on the same storage (UserLabelServerTest.ts:346-353).
func newLabelPair(t *testing.T, decl device.Declaration) *labelPair {
	t.Helper()
	site := quietSite(t)
	lp := &labelPair{mockPair: &mockPair{site: site}}
	lp.light = lp.newLight(t, decl)
	lp.pair = site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Prepare: func(n *node.Node) {
		if err := n.Aggregator().Add(context.Background(), lp.light); err != nil {
			t.Error(err)
		}
	}}})
	ep, ok := lp.pair.Device.EndpointOf("ep1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	lp.ep, lp.lightEP = ep, ep
	return lp
}

func (lp *labelPair) newLight(t *testing.T, decl device.Declaration) *device.OnOffLight {
	t.Helper()
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("ep1"), Name: "ep1"}, decl)
	if err != nil {
		t.Fatal(err)
	}
	return light
}

// userLabelPair is labelDevice with lightWithLabels(maxLabels)
// (UserLabelServerTest.ts:34-40 at db021da0); maxLabels 0 is the server's
// default.
func userLabelPair(t *testing.T, maxLabels int) *labelPair {
	t.Helper()
	return newLabelPair(t, userLabelDecl(t, maxLabels))
}

func userLabelDecl(t *testing.T, maxLabels int) userlabeldef.Declare {
	t.Helper()
	decl := userlabeldef.Declare{}
	if maxLabels != 0 {
		srv, err := core.NewUserLabel(core.UserLabelConfig{MaxLabels: maxLabels})
		if err != nil {
			t.Fatal(err)
		}
		decl.HostServer = srv
	}
	return decl
}

func (lp *labelPair) path(cluster uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: lp.ep, Cluster: cluster, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// write is labelDevice.write (:55-59): the controller's write of value,
// as one REPLACE_ALL with chunking off (replaceAll, :87-89) or as an
// empty REPLACE_ALL followed by one ADD per entry (replaceAllThenAppend,
// :92-94); it returns the status of each write path in order.
func (lp *labelPair) write(t *testing.T, cluster uint32, value spec.Encodable, chunkLists bool) interaction.WriteStatuses {
	t.Helper()
	ctx := parityCtx(t)
	ia, err := fabrictestseam.Connect(ctx, lp.pair)
	if err != nil {
		t.Fatal(err)
	}
	var statuses interaction.WriteStatuses
	if err := lp.site.Run(ctx, func(ctx context.Context) (err error) {
		statuses, err = ia.Write(ctx, []interaction.WriteRequest{{Path: lp.path(cluster), Value: value}}, interaction.WriteOptions{DisableListChunking: !chunkLists})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return statuses
}

func (lp *labelPair) replaceAll(t *testing.T, value []userlabeldef.LabelStruct) interaction.WriteStatuses {
	t.Helper()
	return lp.write(t, userlabeldef.ClusterID, spec.List[userlabeldef.LabelStruct](value), false)
}

func (lp *labelPair) replaceAllThenAppend(t *testing.T, value []userlabeldef.LabelStruct) interaction.WriteStatuses {
	t.Helper()
	return lp.write(t, userlabeldef.ClusterID, spec.List[userlabeldef.LabelStruct](value), true)
}

// rawWrite is rawWrite (:105-117): write paths with hand-made TLV, past
// the controller's own formulation; an element of writes is a REPLACE_ALL
// of a rawLabels list or an ADD of one rawLabel.
func (lp *labelPair) rawWrite(t *testing.T, writes ...spec.Encodable) []im.StatusCode {
	t.Helper()
	return lp.rawWriteTo(t, userlabeldef.ClusterID, writes...)
}

// rawWriteTo is rawWrite to the LabelList of cluster.
func (lp *labelPair) rawWriteTo(t *testing.T, cluster uint32, writes ...spec.Encodable) []im.StatusCode {
	t.Helper()
	req := im.WriteRequest{}
	for _, w := range writes {
		p := lp.path(cluster)
		_, p.ListAppend = w.(rawLabel)
		req.Writes = append(req.Writes, im.AttributeWrite{Path: p, Value: im.AttributeValue{Value: w}})
	}
	answer := rawWrite(t, lp.mockPair, req)
	if answer.status != nil {
		t.Fatalf("StatusResponse %v in place of a WriteResponse", *answer.status)
	}
	out := make([]im.StatusCode, len(answer.resp.Responses))
	for i, r := range answer.resp.Responses {
		out[i] = r.Status.Status
	}
	return out
}

// stored is labelDevice.stored (:52): the labels the device holds.
func (lp *labelPair) stored() []userlabeldef.LabelStruct {
	return lp.light.UserLabel().Get().LabelList
}

// read is labelDevice.read (:62-78): LabelList over the wire.
func (lp *labelPair) read(t *testing.T) []userlabeldef.LabelStruct {
	t.Helper()
	values, err := fabrictest.ReadCluster(parityCtx(t), lp.pair, lp.ep, userlabeldef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := userlabeldef.StateFrom(values)
	return state.LabelList
}

// rawLabel is one LabelStruct with neither length limit nor mandatory
// field: UnconstrainedLabel, LabelWithoutValue and LabelWithoutLabel
// (:97-99); a nil field is left out.
type rawLabel struct{ label, value *string }

func (r rawLabel) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	if r.label != nil {
		enc.PutUTF8(tlv.ContextTag(0), *r.label)
	}
	if r.value != nil {
		enc.PutUTF8(tlv.ContextTag(1), *r.value)
	}
	_ = enc.EndContainer()
}

func entry(label, value string) rawLabel { return rawLabel{label: &label, value: &value} }

// rawLabels is TlvArray(UnconstrainedLabel) (replaceAllWith, :119-122).
type rawLabels []rawLabel

func (r rawLabels) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, e := range r {
		e.EncodeTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

func statusesOf(s interaction.WriteStatuses) []im.StatusCode {
	out := make([]im.StatusCode, len(s))
	for i := range s {
		out[i] = s[i].Status
	}
	return out
}

func appends(s interaction.WriteStatuses) []bool {
	out := make([]bool, len(s))
	for i := range s {
		out[i] = s[i].Path.ListAppend
	}
	return out
}

func expectStatuses(t *testing.T, got, want []im.StatusCode) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("statuses %v, want %v", got, want)
	}
}

func expectLabels(t *testing.T, what string, got, want []userlabeldef.LabelStruct) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s %v, want %v", what, got, want)
	}
}

var (
	success              = []im.StatusCode{im.StatusSuccess}
	keepMe               = []userlabeldef.LabelStruct{{Label: "keep", Value: "me"}}
	seventeen            = strings.Repeat("x", 17)
	labelOnly, valueOnly = "a", "a"
)

// TestUserLabelServerParity ports UserLabelServerTest.ts at db021da0: the
// UserLabel server (UserLabelServer.ts at db021da0) behind a commissioned
// device, written as a controller writes it.
func TestUserLabelServerParity(t *testing.T) { //nolint:tparallel // each subtest commissions a device on the shared test site; parallel subtests would wait for a site slot this test holds
	t.Parallel()

	// "writes within the limit" (UserLabelServerTest.ts:131-170).
	t.Run("stores a single REPLACE_ALL", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		s := lp.replaceAll(t, userLabels(3))
		expectStatuses(t, statusesOf(s), success)
		if appends(s)[0] {
			t.Fatal("the REPLACE_ALL answered as an ADD")
		}
		expectLabels(t, "stored", lp.stored(), userLabels(3))
		expectLabels(t, "read", lp.read(t), userLabels(3))
	})

	t.Run("stores REPLACE_ALL followed by ADD", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		s := lp.replaceAllThenAppend(t, userLabels(3))
		if got := appends(s); !slices.Equal(got, []bool{false, true, true, true}) {
			t.Fatalf("paths ADD %v, want REPLACE_ALL then three ADDs", got)
		}
		expectStatuses(t, statusesOf(s), slices.Repeat(success, 4))
		expectLabels(t, "stored", lp.stored(), userLabels(3))
		expectLabels(t, "read", lp.read(t), userLabels(3))
	})

	t.Run("replaces earlier labels and accepts an empty list", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		expectStatuses(t, statusesOf(lp.replaceAll(t, userLabels(3))), success)
		expectLabels(t, "stored", lp.stored(), userLabels(3))
		expectStatuses(t, statusesOf(lp.replaceAll(t, nil)), success)
		expectLabels(t, "stored", lp.stored(), nil)
	})

	// LabelStruct Label and Value are "max 16": the generated definition's
	// DecodeStringIn(0, 16), from the snapshot.
	t.Run("accepts labels and values of exactly 16 characters", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		e := []userlabeldef.LabelStruct{{Label: strings.Repeat("l", 16), Value: strings.Repeat("v", 16)}}
		lp.replaceAll(t, e)
		expectLabels(t, "stored", lp.stored(), e)
	})

	// "list length limit" (:172-223): a LabelList longer than maxLabels is
	// RESOURCE_EXHAUSTED (UserLabelServer.ts:24-31 at db021da0), maxLabels
	// 255 by default (:39).
	t.Run("accepts exactly 4 entries when maxLabels is the spec minimum", func(t *testing.T) {
		lp := userLabelPair(t, 4)
		expectStatuses(t, statusesOf(lp.replaceAll(t, userLabels(4))), success)
		expectLabels(t, "stored", lp.stored(), userLabels(4))
	})

	t.Run("refuses a single REPLACE_ALL above maxLabels with RESOURCE_EXHAUSTED and keeps the old list", func(t *testing.T) {
		lp := userLabelPair(t, 4)
		lp.replaceAll(t, userLabels(2))
		expectStatuses(t, statusesOf(lp.replaceAll(t, userLabels(5))), []im.StatusCode{im.StatusResourceExhausted})
		expectLabels(t, "stored", lp.stored(), userLabels(2))
	})

	t.Run("refuses the ADD that crosses maxLabels and keeps the entries before it", func(t *testing.T) {
		lp := userLabelPair(t, 4)
		s := lp.replaceAllThenAppend(t, userLabels(6))
		expectStatuses(t, statusesOf(s), append(slices.Repeat(success, 5), im.StatusResourceExhausted, im.StatusResourceExhausted))
		expectLabels(t, "stored", lp.stored(), userLabels(4))
		expectLabels(t, "read", lp.read(t), userLabels(4))
	})

	t.Run("defaults to a maximum of 255 entries", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		s := statusesOf(lp.replaceAllThenAppend(t, userLabels(256)))
		if len(s) != 257 {
			t.Fatalf("%d statuses, want 257", len(s))
		}
		if n := len(slices.DeleteFunc(slices.Clone(s), func(c im.StatusCode) bool { return c != im.StatusResourceExhausted })); n != 1 || s[256] != im.StatusResourceExhausted {
			t.Fatalf("%d RESOURCE_EXHAUSTED, the last %v; want only the last", n, s[256])
		}
		if n := len(lp.stored()); n != 255 {
			t.Fatalf("stored %d labels, want 255", n)
		}
	})

	// "LabelStruct constraints" (:225-294): a 17-character label or value
	// is CONSTRAINT_ERROR in a REPLACE_ALL and in an ADD.
	for _, c := range []struct {
		name  string
		entry rawLabel
	}{
		{"label", entry(seventeen, "v")},
		{"value", entry("l", seventeen)},
	} {
		t.Run("rejects a "+c.name+" of 17 characters in a REPLACE_ALL", func(t *testing.T) {
			lp := userLabelPair(t, 0)
			lp.replaceAll(t, keepMe)
			expectStatuses(t, lp.rawWrite(t, rawLabels{c.entry}), []im.StatusCode{im.StatusConstraintError})
			expectLabels(t, "stored", lp.stored(), keepMe)
		})

		t.Run("rejects a "+c.name+" of 17 characters in an ADD", func(t *testing.T) {
			lp := userLabelPair(t, 0)
			got := lp.rawWrite(t, rawLabels{entry("keep", "me")}, c.entry)
			expectStatuses(t, got, []im.StatusCode{im.StatusSuccess, im.StatusConstraintError})
			expectLabels(t, "stored", lp.stored(), keepMe)
		})
	}

	// Value is mandatory: matter.js refuses an entry without it, where chip
	// decodes it as empty (:255-256).
	t.Run("rejects an entry without value in a REPLACE_ALL", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		expectStatuses(t, lp.rawWrite(t, rawLabels{{label: &labelOnly}}), []im.StatusCode{im.StatusConstraintError})
		expectLabels(t, "stored", lp.stored(), nil)
	})

	t.Run("rejects an entry without value in an ADD", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		got := lp.rawWrite(t, rawLabels{}, rawLabel{label: &labelOnly})
		expectStatuses(t, got, []im.StatusCode{im.StatusSuccess, im.StatusConstraintError})
		expectLabels(t, "stored", lp.stored(), nil)
	})

	t.Run("rejects an entry without label like one without value", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		withoutLabel := lp.rawWrite(t, rawLabels{{value: &valueOnly}})
		withoutValue := lp.rawWrite(t, rawLabels{{label: &labelOnly}})
		expectStatuses(t, withoutLabel, withoutValue)
		expectStatuses(t, withoutLabel, []im.StatusCode{im.StatusConstraintError})
		expectLabels(t, "stored", lp.stored(), nil)
	})

	// "write rules of the interaction model" (:296-331).
	t.Run("answers a lone ADD to a non-empty list with BUSY and keeps the list", func(t *testing.T) {
		// matter.js answers an ADD not preceded by a write path to the same
		// attribute in the same interaction BUSY, as chip does
		// (packages/node/src/node/server/InteractionServer.ts:511-531 at
		// db021da0, previousProcessedAttributePath :456-458); im/write.go
		// keeps the same marker (issue 438, findings T3-P3-1 closed).
		lp := userLabelPair(t, 0)
		lp.replaceAll(t, userLabels(2))
		third := userLabels(3)[2]
		expectStatuses(t, lp.rawWrite(t, entry(third.Label, third.Value)), []im.StatusCode{im.StatusBusy})
		expectLabels(t, "stored", lp.stored(), userLabels(2))
	})

	// LabelList is "RW VM": writing needs Manage (:309).
	t.Run("refuses a writer with only Operate privilege with UNSUPPORTED_ACCESS", func(t *testing.T) {
		lp := userLabelPair(t, 0)
		if err := lp.pair.Grant(parityCtx(t), store.PrivilegeOperate); err != nil {
			t.Fatal(err)
		}
		expectStatuses(t, statusesOf(lp.replaceAll(t, userLabels(2))), []im.StatusCode{im.StatusUnsupportedAccess})
		expectLabels(t, "stored", lp.stored(), nil)
	})

	// "persistence" (:333-357): LabelList is nonvolatile. As matter.js
	// does, the restarted node on the same store serves a new light of the
	// same key, whose list must come back from the store.
	for _, c := range []struct {
		name  string
		chunk bool
	}{{"a single REPLACE_ALL", false}, {"REPLACE_ALL followed by ADD", true}} {
		t.Run("keeps labels written with "+c.name+" across a restart", func(t *testing.T) {
			lp := userLabelPair(t, 0)
			expectStatuses(t, statusesOf(lp.write(t, userlabeldef.ClusterID, spec.List[userlabeldef.LabelStruct](userLabels(3)), c.chunk)), slices.Repeat(success, map[bool]int{false: 1, true: 4}[c.chunk]))
			ctx := parityCtx(t)
			if err := lp.pair.Device.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			lp.light = lp.newLight(t, userLabelDecl(t, 0))
			if err := lp.pair.Device.Start(ctx); err != nil {
				t.Fatal(err)
			}
			expectLabels(t, "restored", lp.stored(), userLabels(3))
		})
	}
}

// fixed is FIXED (FixedLabelServerTest.ts:18-21 at db021da0).
var fixed = []fixedlabeldef.LabelStruct{{Label: "room", Value: "bedroom 2"}, {Label: "orientation", Value: "up"}}

// fixedLabelPair is addCommissionedPair with lightWithFixedLabels(FIXED)
// (FixedLabelServerTest.ts:23-29 at db021da0).
func fixedLabelPair(t *testing.T) *labelPair {
	t.Helper()
	return newLabelPair(t, fixedLabelDecl(fixed))
}

func fixedLabelDecl(labels []fixedlabeldef.LabelStruct) fixedlabeldef.Declare {
	initial := slices.Clone(labels)
	return fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &initial}}
}

func (lp *labelPair) fixedStored() []fixedlabeldef.LabelStruct {
	return lp.light.FixedLabel().Get().LabelList
}

// TestFixedLabelServerParity ports FixedLabelServerTest.ts at db021da0: the
// FixedLabel server, which has no logic of its own (FixedLabelServer.ts:14
// at db021da0), behind a commissioned device.
func TestFixedLabelServerParity(t *testing.T) { //nolint:tparallel // each subtest commissions a device on the shared test site; parallel subtests would wait for a site slot this test holds
	t.Parallel()

	// LabelList is "R V" (FixedLabelServerTest.ts:36).
	t.Run("serves the labels of the application to a controller", func(t *testing.T) {
		lp := fixedLabelPair(t)
		values, err := fabrictest.ReadCluster(parityCtx(t), lp.pair, lp.ep, fixedlabeldef.ClusterID)
		if err != nil {
			t.Fatal(err)
		}
		state, _ := fixedlabeldef.StateFrom(values)
		if !slices.Equal(state.LabelList, fixed) {
			t.Fatalf("LabelList %v, want %v", state.LabelList, fixed)
		}
	})

	// "R V" has no write privilege (:58-87): every write path is
	// UNSUPPORTED_WRITE, the REPLACE_ALL and each ADD alike.
	for _, c := range []struct {
		name  string
		chunk bool
		paths int
	}{{"REPLACE_ALL", false, 1}, {"REPLACE_ALL followed by ADD", true, 2}} {
		t.Run("refuses a controller "+c.name+" with UNSUPPORTED_WRITE and keeps the labels", func(t *testing.T) {
			// matter.js answers a write to a non-writable attribute
			// UNSUPPORTED_WRITE before it decodes the value
			// (packages/protocol/src/action/server/AttributeWriteResponse.ts:231-233
			// at db021da0); schema's read-only table carries LabelList, so
			// the dispatcher refuses the ADD before it decodes the item.
			lp := fixedLabelPair(t)
			s := lp.write(t, fixedlabeldef.ClusterID, spec.List[fixedlabeldef.LabelStruct](fixed[:1]), c.chunk)
			expectStatuses(t, statusesOf(s), slices.Repeat([]im.StatusCode{im.StatusUnsupportedWrite}, c.paths))
			if got := lp.fixedStored(); !slices.Equal(got, fixed) {
				t.Fatalf("LabelList %v, want %v", got, fixed)
			}
		})
	}

	// The lone-ADD rule comes before the writability check
	// (InteractionServer.ts:511-531 at db021da0, ahead of
	// AttributeWriteResponse.ts:231-233): an ADD to the read-only list
	// that follows no write of it is BUSY, not UNSUPPORTED_WRITE.
	t.Run("answers a lone ADD to the read-only list with BUSY and keeps the labels", func(t *testing.T) {
		lp := fixedLabelPair(t)
		expectStatuses(t, lp.rawWriteTo(t, fixedlabeldef.ClusterID, entry("room", "kitchen")), []im.StatusCode{im.StatusBusy})
		if got := lp.fixedStored(); !slices.Equal(got, fixed) {
			t.Fatalf("LabelList %v, want %v", got, fixed)
		}
	})

	// Nothing writes the application's labels to storage, so a changed
	// application value wins after a restart (:89-91).
	t.Run("takes the labels from the application after a restart", func(t *testing.T) {
		lp := fixedLabelPair(t)
		ctx := parityCtx(t)
		if err := lp.pair.Device.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		changed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "kitchen"}}
		lp.light = lp.newLight(t, fixedLabelDecl(changed))
		if err := lp.pair.Device.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if got := lp.fixedStored(); !slices.Equal(got, changed) {
			t.Fatalf("LabelList %v, want %v", got, changed)
		}
	})
}
