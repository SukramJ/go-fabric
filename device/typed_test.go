// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// TestTypedAccessors: every accessor returns the cluster's typed server
// over the server the device mounts — the generated one, a self-held one,
// a host's of any type — and nil for a cluster it does not mount. The
// device mounts the servers themselves.
func TestTypedAccessors(t *testing.T) {
	t.Parallel()
	d, err := NewDimmableLight(testOptions("typed"))
	if err != nil {
		t.Fatal(err)
	}
	lc := d.LevelControl()
	if _, selfHeld := lc.Underlying().(*levelcontrol.Server); !selfHeld || lc.Underlying() != d.Server(levelcontroldef.ClusterID) {
		t.Errorf("LevelControl over %T", lc.Underlying())
	}
	if _, selfHeld := d.OnOff().Underlying().(*onoff.Server); !selfHeld {
		t.Errorf("OnOff over %T", d.OnOff().Underlying())
	}
	if got := d.OnOff().Get(); got.OnOff {
		t.Error("a new light is on")
	}

	host := struct{ contract.ClusterServer }{d.Server(levelcontroldef.ClusterID)}
	d, err = NewDimmableLight(testOptions("typed-host"), withServer(host))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.LevelControl().Underlying(); got != contract.ClusterServer(host) || d.Server(levelcontroldef.ClusterID) != got {
		t.Errorf("a host's server: %T mounted, %T wrapped", d.Server(levelcontroldef.ClusterID), got)
	}

	ap, err := NewAirPurifier(testOptions("typed-optional"))
	if err != nil {
		t.Fatal(err)
	}
	if ap.OnOff() != nil {
		t.Error("an accessor of an unmounted cluster is not nil")
	}
}
