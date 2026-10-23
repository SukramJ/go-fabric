// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"strings"
	"testing"

	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/device"
)

// TestFixedLabel: the labels a host states are the server's LabelList,
// on a device whose type does not list FixedLabel; an over-long label is
// refused.
func TestFixedLabel(t *testing.T) {
	t.Parallel()
	labels := fixedlabeldef.LabelStruct{Label: "room", Value: "demo"}
	decl, err := device.FixedLabel(labels)
	if err != nil {
		t.Fatal(err)
	}
	d, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("l"), Name: "l"}, decl)
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Server(fixedlabeldef.ClusterID)
	if srv == nil {
		t.Fatal("no FixedLabel server")
	}
	if got := fixedlabeldef.Wrap(srv).Get().LabelList; len(got) != 1 || got[0] != labels {
		t.Errorf("LabelList %+v", got)
	}
	if _, err := device.FixedLabel(fixedlabeldef.LabelStruct{Label: strings.Repeat("x", 40), Value: "v"}); err == nil {
		t.Error("an over-long label was accepted")
	}
}
