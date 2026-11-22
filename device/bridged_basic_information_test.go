// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"strings"
	"testing"

	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
)

// TestBridgedBasicInformationDeclaration (review II, R2-P5-apidev-02,
// decision 143): New takes a BridgedDeviceBasicInformation declaration's
// Initial and hands it to the assembler (Spec), copied; everything else the
// declaration carries, an attribute the assembler owns, and a fact stated
// twice (Options.Name beside NodeLabel) are refused.
func TestBridgedBasicInformationDeclaration(t *testing.T) {
	t.Parallel()
	serial, label := "SN-1", "Desk"
	d, err := New(OnOffLightType(), testOptions("lamp-sn"), bdbidef.Declare{Initial: bdbidef.Patch{SerialNumber: &serial}})
	if err != nil {
		t.Fatal(err)
	}
	serial = "changed"
	if got := d.Spec().BridgedBasicInformation.SerialNumber; got == nil || *got != "SN-1" {
		t.Errorf("Spec SerialNumber %v, want the declared SN-1", got)
	}
	opts := testOptions("lamp-label")
	opts.Name = ""
	d, err = New(OnOffLightType(), opts, &bdbidef.Declare{Initial: bdbidef.Patch{NodeLabel: &label}})
	if err != nil {
		t.Fatal(err)
	}
	if s := d.Spec(); s.FriendlyName != "Desk" || s.BridgedBasicInformation.NodeLabel == nil {
		t.Errorf("Spec FriendlyName %q, NodeLabel %v; want the declared label", s.FriendlyName, s.BridgedBasicInformation.NodeLabel)
	}
	reach := true
	named := testOptions("lamp-twice")
	named.Name = "Lamp"
	for name, tc := range map[string]struct {
		opts Options
		decl bdbidef.Declare
		want string
	}{
		"Features":     {testOptions("a"), bdbidef.Declare{Features: 1}, "Initial only"},
		"OnWrite":      {testOptions("b"), bdbidef.Declare{OnWrite: func(context.Context, bdbidef.State, uint32) error { return nil }}, "Initial only"},
		"Reachable":    {testOptions("c"), bdbidef.Declare{Initial: bdbidef.Patch{Reachable: &reach}}, "not the declaration's"},
		"stated twice": {named, bdbidef.Declare{Initial: bdbidef.Patch{NodeLabel: &label}}, "one fact twice"},
	} {
		_, err := New(OnOffLightType(), tc.opts, tc.decl)
		if !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want ErrConfiguration naming %q", name, err, tc.want)
		}
	}
}
