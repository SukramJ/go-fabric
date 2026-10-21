// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestWithServerMountsAClusterTheTypeDoesNotList: a host server for a
// cluster the device type does not list is mounted after the type's own
// clusters, in the order of the WithServer calls — matter.js's
// MutableEndpoint.with extends the type's behaviors with any behavior
// (MutableEndpoint.ts:95-110). Every other option for such a cluster, and
// a server for a cluster the assembler mounts, stays refused.
func TestWithServerMountsAClusterTheTypeDoesNotList(t *testing.T) {
	t.Parallel()
	fixed := &hostServer{id: 0x0040}    // FixedLabel: not an OnOffLight requirement
	eco := &hostServer{id: 0x0750}      // EcosystemInformation: BridgedNode's, not the light's
	levelCtl := &hostServer{id: 0x0008} // LevelControl: an optional requirement
	d, err := NewOnOffLight(testOptions("l"), withServer(eco), withServer(levelCtl), withServer(fixed))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serverIDs(d.Servers()), []uint32{0x0006, 0x0008, 0x0750, 0x0040}; !slices.Equal(got, want) {
		t.Fatalf("servers %#04x, want %#04x (the type's clusters, then the unlisted ones in call order)", got, want)
	}
	if d.Server(0x0040) != fixed || d.Server(0x0750) != eco {
		t.Error("the host's servers for unlisted clusters are not the ones mounted")
	}
	// A second WithServer for the same unlisted cluster replaces the first
	// and keeps its place.
	other := &hostServer{id: 0x0750}
	d, err = NewOnOffLight(testOptions("l"), withServer(eco), withServer(fixed), withServer(other))
	if err != nil {
		t.Fatal(err)
	}
	if got := serverIDs(d.Servers()); !slices.Equal(got, []uint32{0x0006, 0x0750, 0x0040}) || d.Server(0x0750) != other {
		t.Errorf("servers %#04x after a replacing WithServer", got)
	}
	for name, with := range map[string][]Option{
		"opt-in":      {withServer(fixed), withOptional(0x0040)},
		"initial":     {withServer(fixed), withInitial(0x0040, map[uint32]any{0: true})},
		"no server":   {withOptional(0x0040)},
		"assembler's": {withServer(&hostServer{id: 0x0039})},
	} {
		_, err := NewOnOffLight(testOptions("l"), with...)
		if err == nil || !strings.Contains(err.Error(), "0x00") {
			t.Errorf("%s: %v, want a refusal naming the cluster", name, err)
		}
	}
}

// notifier is a host model's change notifier.
type notifier struct{ cbs []func() }

func (n *notifier) OnMatterValueChanged(cb func()) func() {
	n.cbs = append(n.cbs, cb)
	return func() {}
}

func (n *notifier) fire() {
	for _, cb := range n.cbs {
		cb()
	}
}

// TestWithChangeNotifier: the device's endpoint source is a
// contract.ChangeNotifier exactly when the host gave one, and it fires
// when the host's notifier fires; without it the source is the device
// itself, as before.
func TestWithChangeNotifier(t *testing.T) {
	t.Parallel()
	plain, err := NewOnOffLight(testOptions("plain"))
	if err != nil {
		t.Fatal(err)
	}
	if src := plain.Spec().Source; src != contract.EndpointSource(plain.Device) {
		t.Errorf("source without a notifier is %T, want the device", src)
	} else if _, ok := src.(contract.ChangeNotifier); ok {
		t.Error("a device without a notifier is a ChangeNotifier")
	}

	n := &notifier{}
	d, err := NewOnOffLight(testOptions("n"), WithChangeNotifier(n))
	if err != nil {
		t.Fatal(err)
	}
	spec := d.Spec()
	cn, ok := spec.Source.(contract.ChangeNotifier)
	if !ok {
		t.Fatalf("source %T is no ChangeNotifier", spec.Source)
	}
	fired := 0
	cn.OnMatterValueChanged(func() { fired++ })
	n.fire()
	if fired != 1 {
		t.Errorf("fired %d times, want 1", fired)
	}
	if spec.Source.MatterDeviceType() != OnOffLightID || len(spec.Source.MatterClusterServers()) != 1 {
		t.Errorf("source serves type 0x%04X, %d servers; want the device's", spec.Source.MatterDeviceType(), len(spec.Source.MatterClusterServers()))
	}
	if d.Spec().Source != spec.Source {
		t.Error("the source changes between Spec calls")
	}
	// A part keeps its own source.
	p, err := NewPowerSource(Options{StableKey: testOptions("battery").StableKey}, withServer(&hostServer{id: 0x002F}))
	if err != nil {
		t.Fatal(err)
	}
	alarm, err := NewSmokeCoAlarm(testOptions("alarm"), withServer(&hostServer{id: 0x005C}), WithChangeNotifier(n))
	if err != nil {
		t.Fatal(err)
	}
	if err := alarm.AddPart(p.Device); err != nil {
		t.Fatal(err)
	}
	if _, ok := alarm.Spec().Parts[0].Source.(contract.ChangeNotifier); ok {
		t.Error("the part took the owner's notifier")
	}
}
