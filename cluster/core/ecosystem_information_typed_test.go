// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/contract"
)

// typedEcoDir is a directory with one device.
type typedEcoDir struct{}

func (typedEcoDir) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct {
	return []ecodef.EcosystemDeviceStruct{{
		BridgedEndpoint: ptrU16(2), OriginalEndpoint: ptrU16(3), DeviceTypes: []ecodef.DeviceTypeStruct{{DeviceType: 0x0100, Revision: 1}},
		UniqueLocationIds: []string{}, UniqueLocationIdsLastEdit: 1, FabricIndex: 1,
	}}
}

func (typedEcoDir) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct { return nil }

// TestTypedEcosystemInformationServer drives core.EcosystemInformation
// through its optional interfaces: both lists are the directory's (Set
// refuses them as not held), the directory's change reaches OnChanged, and
// a handler and an event are refused (the cluster has neither).
func TestTypedEcosystemInformationServer(t *testing.T) {
	t.Parallel()
	e := core.NewEcosystemInformation(typedEcoDir{})
	s := ecodef.Wrap(e)

	if err := e.SetAttributes(map[uint32]any{ecodef.AttrDeviceDirectory: []ecodef.EcosystemDeviceStruct{}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set DeviceDirectory: %v, want ErrNotHeld", err)
	}
	if err := s.Set(ecodef.Patch{}); err != nil {
		t.Errorf("an empty patch: %v", err)
	}
	var changed [][]uint32
	unsubscribe := s.OnChanged(func(_ ecodef.State, change contract.Change) { ids := change.Attributes; changed = append(changed, ids) })
	defer unsubscribe()
	e.DirectoryChanged()
	if len(changed) != 1 || len(changed[0]) != 2 {
		t.Errorf("OnChanged %v", changed)
	}
	if err := e.Handle(0x00, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle: %v, want ErrNotAccepted", err)
	}
	if err := e.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v, want ErrNotEmitted", err)
	}
	if err := s.OnWrite(func(context.Context, ecodef.State, uint32) error { return nil }); err != nil {
		t.Errorf("OnWrite: %v", err)
	}
}
