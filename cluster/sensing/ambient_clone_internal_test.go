// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	def "thingfabric.dev/go-fabric/cluster/spec/ambientcontextsensing"
)

// TestObjectCountConfigIsTheServers holds R2-P6-api-1461: the server keeps
// its own copy of the ObjectCountConfig the host hands it and hands out
// copies — a host that changes its config variable after construction, or
// the copy the getter returned, changes neither the getter's value nor
// the wire's, and moves no data version.
func TestObjectCountConfigIsTheServers(t *testing.T) {
	threshold := uint16(5)
	cfg := ObjectCountConfig{
		CountingObject:       &SemanticTag{MfgCode: spec.NullOf[uint16](), NamespaceId: NamespaceIdentifiedObject},
		ObjectCountThreshold: &threshold,
	}
	c, err := NewAmbientContext(AmbientContextConfig{
		Features:          FeatureObjectCounting | FeatureObjectIdentification,
		ObjectCountConfig: &cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	v0 := c.MatterDataVersion()
	threshold = 0
	cfg.CountingObject.Tag = 9
	got := c.ObjectCountConfig()
	if *got.ObjectCountThreshold != 5 || got.CountingObject.Tag != 0 {
		t.Errorf("after the host changed its config: threshold %d, tag %d, want 5 and 0", *got.ObjectCountThreshold, got.CountingObject.Tag)
	}
	*got.ObjectCountThreshold = 77
	got.CountingObject.Tag = 8
	read, _ := c.MatterRead(def.AttrObjectCountConfig)
	occ, ok := read.(ObjectCountConfig)
	if !ok || *occ.ObjectCountThreshold != 5 || occ.CountingObject.Tag != 0 {
		t.Errorf("after an edit of the getter's copy the wire reads %+v, want threshold 5, tag 0", read)
	}
	if again := c.ObjectCountConfig(); *again.ObjectCountThreshold != 5 {
		t.Errorf("after an edit of the getter's copy the getter reads %d, want 5", *again.ObjectCountThreshold)
	}
	if v := c.MatterDataVersion(); v != v0 {
		t.Errorf("data version moved %d → %d", v0, v)
	}
}

// TestAmbientStateCloneIsDeep is the class guard of R2-P6-api-1461
// (decision 142) for the server's state: a clone shares no list, pointer
// or nested value with the state it copies, so a mutation that works on
// the copy and is not committed leaves the state as it was.
func TestAmbientStateCloneIsDeep(t *testing.T) {
	label := spec.ValueOf("tag")
	threshold, start := uint16(3), uint32(10)
	sensed := []SemanticTag{{Tag: 1, Label: &label}}
	orig := acState{
		supported:  []SemanticTag{{Tag: 1, Label: &label}},
		detections: []detection{{info: Detection{AmbientContextSensed: &sensed}}},
		predicted:  []PredictedActivity{{StartTimestamp: &start, AmbientContextType: &sensed}},
		occ:        ObjectCountConfig{CountingObject: &SemanticTag{Tag: 1}, ObjectCountThreshold: &threshold},
	}
	c := orig.clone()
	*c.supported[0].Label = spec.ValueOf("x")
	(*c.detections[0].info.AmbientContextSensed)[0].Tag = 9
	*c.predicted[0].StartTimestamp = 99
	(*c.predicted[0].AmbientContextType)[0].Tag = 9
	c.occ.CountingObject.Tag = 9
	*c.occ.ObjectCountThreshold = 99
	if label != spec.ValueOf("tag") || sensed[0].Tag != 1 || start != 10 || orig.occ.CountingObject.Tag != 1 || threshold != 3 {
		t.Errorf("a change to the clone reached the state: label %v, sensed %d, start %d, counting %d, threshold %d",
			label, sensed[0].Tag, start, orig.occ.CountingObject.Tag, threshold)
	}
}
