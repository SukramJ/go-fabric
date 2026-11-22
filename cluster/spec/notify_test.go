// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/identify"
	"thingfabric.dev/go-fabric/contract"
)

// TestDeprecatedNotificationsKeepTheirBehaviour: the four notifications
// Server now declares itself (R2-P5-apispec-16) tell exactly the listeners
// they told when promoted from cluster.AttributeChanges, and advance no
// data version.
func TestDeprecatedNotificationsKeepTheirBehaviour(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(identify.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	var changes, reports []contract.Origin
	srv.OnAttributeChange(func(c contract.Change) { changes = append(changes, c.Origin) })
	srv.OnAttributeReport(func(c contract.Change) { reports = append(reports, c.Origin) })
	v := srv.MatterDataVersion()
	srv.Notify(identify.AttrIdentifyType)                                     //nolint:staticcheck // the deprecated method under test
	srv.NotifyFrom(contract.OriginCommand, identify.AttrIdentifyType)         //nolint:staticcheck // the deprecated method under test
	srv.ChangedFrom(contract.OriginHostSet, identify.AttrIdentifyType)        //nolint:staticcheck // the deprecated method under test
	srv.ReportFrom(contract.OriginControllerWrite, identify.AttrIdentifyType) //nolint:staticcheck // the deprecated method under test
	wantChanges := []contract.Origin{contract.OriginUnknown, contract.OriginCommand, contract.OriginHostSet}
	wantReports := []contract.Origin{contract.OriginUnknown, contract.OriginCommand, contract.OriginControllerWrite}
	if !equalOrigins(changes, wantChanges) || !equalOrigins(reports, wantReports) {
		t.Errorf("changes %v reports %v, want %v and %v", changes, reports, wantChanges, wantReports)
	}
	if srv.MatterDataVersion() != v {
		t.Errorf("the notifications advanced the data version %d → %d", v, srv.MatterDataVersion())
	}
}

func equalOrigins(a, b []contract.Origin) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
