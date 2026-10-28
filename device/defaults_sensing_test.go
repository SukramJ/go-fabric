// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	acsdef "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
)

// acsType is ContactSensor with AmbientContextSensing as an optional
// server: no device type at the pin lists the cluster, so the kit's
// default is exercised on a type that does.
func acsType() *Type {
	t := ContactSensorType
	t.Servers = append(slices.Clone(t.Servers), ClusterRequirement{
		ID: acsdef.ClusterID, Name: acsdef.Name, Conformance: "O", Definition: acsdef.Definition,
	})
	return &t
}

// acsHA is a HumanActivity selection with a supported tag and a persisted
// HoldTime.
func acsHA() acsdef.Declare {
	hold := uint16(30)
	sup := []acsdef.Semtag{{NamespaceId: acsdef.NamespaceIdentifiedHumanActivity, Tag: 1}}
	sup[0].MfgCode.Null = true
	return acsdef.Declare{Features: acsdef.FeatureHumanActivity, Initial: acsdef.Patch{HoldTime: &hold, AmbientContextTypeSupported: &sup}}
}

// sensingCases are the TestSelfHeldDefaults cases of cluster/sensing.
func sensingCases() []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
} {
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{acsdef.ClusterID, func() (*Device, error) { return New(acsType(), testOptions("acs"), acsHA()) }, (*sensing.AmbientContext)(nil)},
	}
}

// TestAmbientContextKit: the kit's server takes Initial as its
// configuration and reaches its Host through the typed server; a
// selection without a detection feature is refused (the model's O.a+); on a type that does not list the cluster the host mounts the
// server as its HostServer.
func TestAmbientContextKit(t *testing.T) {
	d, err := New(acsType(), testOptions("acs"), acsHA())
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := d.Server(acsdef.ClusterID).(*sensing.AmbientContext)
	if srv == nil || srv.HoldTime() != 30 {
		t.Fatalf("server %T, HoldTime from Initial", d.Server(acsdef.ClusterID))
	}
	host := acsdef.Wrap(srv).Host()
	tags := []acsdef.Semtag{{MfgCode: srv.ObjectCountConfig().CountingObject.MfgCode, NamespaceId: acsdef.NamespaceIdentifiedHumanActivity, Tag: 1}}
	if err := host.AddDetection(acsdef.AmbientContextTypeStruct{AmbientContextSensed: &tags}); err != nil {
		t.Fatal(err)
	}
	if !acsdef.Wrap(srv).Get().HumanActivityDetected {
		t.Error("not detected")
	}

	if _, err := New(acsType(), testOptions("acs-pred"), acsdef.Declare{Features: acsdef.FeaturePredictedActivity}); err == nil {
		t.Error("PRED alone accepted")
	}

	own, err := sensing.NewAmbientContext(sensing.AmbientContextConfig{Features: acsdef.FeatureSoundIdentification})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(own.Close)
	d, err = New(&ContactSensorType, testOptions("acs-host"), acsdef.Declare{HostServer: own})
	if err != nil {
		t.Fatal(err)
	}
	if d.Server(acsdef.ClusterID) != own {
		t.Error("the HostServer not mounted")
	}
}
