// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// TestTypedServerMounted: the default generated server and a bare
// *spec.Server the host supplies are both mounted as the cluster's typed
// server, the one the accessor returns and the device lists.
func TestTypedServerMounted(t *testing.T) {
	t.Parallel()
	d, err := NewDimmableLight(testOptions("typed"))
	if err != nil {
		t.Fatal(err)
	}
	lc := d.LevelControl()
	if lc == nil || lc.Server == nil {
		t.Fatal("no typed LevelControl server")
	}
	if got := d.Server(levelcontroldef.ClusterID); got != contract.ClusterServer(lc) {
		t.Errorf("the device lists %T, the accessor returns another", got)
	}

	host, err := spec.NewServer(levelcontroldef.Definition, spec.Options{Features: uint32(levelcontroldef.FeatureOnOff | levelcontroldef.FeatureLighting)},
		spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err = NewDimmableLight(testOptions("typed-host"), WithServer(host))
	if err != nil {
		t.Fatal(err)
	}
	if lc := d.LevelControl(); lc == nil || lc.Server != host {
		t.Errorf("the host's server is not the typed one: %#v", lc)
	}

	// A host server of another type is mounted as given, and the accessor
	// answers nil for it.
	other := struct{ contract.ClusterServer }{host}
	d, err = NewDimmableLight(testOptions("typed-other"), WithServer(other))
	if err != nil {
		t.Fatal(err)
	}
	if d.LevelControl() != nil || d.Server(levelcontroldef.ClusterID) != contract.ClusterServer(other) {
		t.Error("a host server of another type was wrapped")
	}
}
