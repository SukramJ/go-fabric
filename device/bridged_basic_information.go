// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"thingfabric.dev/go-fabric/cluster/spec"
	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
)

// bridgedInfoAssembler are the BridgedDeviceBasicInformation attributes a
// declaration's Initial may not state: Reachable is [Device.SetReachable]'s,
// UniqueId and ConfigurationVersion the assembler's, and the rest are
// attributes the bridged server does not serve.
var bridgedInfoAssembler = []uint32{
	bdbidef.AttrReachable, bdbidef.AttrUniqueId, bdbidef.AttrConfigurationVersion,
	bdbidef.AttrDataModelRevision, bdbidef.AttrLocation, bdbidef.AttrLocalConfigDisabled,
	bdbidef.AttrCapabilityMinima, bdbidef.AttrSpecificationVersion, bdbidef.AttrMaxPathsPerInvoke,
}

// declareBridgedBasicInformation takes a bridgeddevicebasicinformationdef
// Declare's Initial: what the host states of the bridged device, which the
// assembler serves ahead of its fallbacks ([endpoint.Spec.
// BridgedBasicInformation]). The assembler mounts the server itself, so
// everything else a declaration carries — Features, HostServer, OnWrite,
// a handler, extra elements — is refused, as is an attribute the assembler
// or [Device.SetReachable] owns (review II, R2-P5-apidev-02, decision 143).
func (s *settings) declareBridgedBasicInformation(d Declaration, decl spec.ClusterDeclaration) error {
	var o bdbidef.Declare
	switch v := d.(type) {
	case bdbidef.Declare:
		o = v
	case *bdbidef.Declare:
		o = *v // a typed nil is refused before (declare)
	}
	if o.OnWrite != nil || o.HandleKeepActive != nil || o.NoFeatures || decl.Server != nil || decl.HasFeatures || hasPorts(decl.Ports) ||
		len(decl.Elements.Attributes)+len(decl.Elements.Commands)+len(decl.Elements.Events) > 0 {
		return fmt.Errorf("%w: the endpoint assembler mounts BridgedDeviceBasicInformation (0x0039) itself; its declaration takes Initial only", ErrConfiguration)
	}
	for _, id := range slices.Sorted(maps.Keys(decl.Initial)) {
		if slices.Contains(bridgedInfoAssembler, id) {
			name := bdbidef.Definition.Attribute(id).Name
			return fmt.Errorf("%w: BridgedDeviceBasicInformation %s (0x%04X) is not the declaration's to state", ErrConfiguration, name, id)
		}
		if err := checkBridgedInfoValue(id, decl.Initial[id]); err != nil {
			return err
		}
	}
	p := copyPatch(o.Initial)
	s.bridgedInfo = &p
	return nil
}

// checkBridgedInfoValue holds a stated BridgedDeviceBasicInformation value
// to the generated definition's type and constraint — the check a
// controller's write of the attribute meets (spec.CheckWritten) — and
// refuses one beyond it with ErrConfiguration, naming the attribute: the
// assembler serves a stated value as stated, so one the device could
// not serve within its constraint is refused here rather than served
// over-long or cut (R2-P6-wire-2502, decision 201).
func checkBridgedInfoValue(id uint32, v any) error {
	if err := spec.CheckWritten(bdbidef.Definition, 0, id, v, nil); err != nil {
		name := bdbidef.Definition.Attribute(id).Name
		return fmt.Errorf("%w: BridgedDeviceBasicInformation %s (0x%04X): %w", ErrConfiguration, name, id, err)
	}
	return nil
}

// checkBridgedInfoOptions refuses a fact stated twice: Options.Name beside
// a declared NodeLabel, Options.Vendor beside a declared VendorName; and
// an Options.Vendor beyond VendorName's constraint, which the device
// serves as its VendorName ([checkBridgedInfoValue]).
func checkBridgedInfoOptions(opts Options, p *bdbidef.Patch) error {
	if opts.Vendor != "" {
		if err := spec.CheckWritten(bdbidef.Definition, 0, bdbidef.AttrVendorName, opts.Vendor, nil); err != nil {
			return fmt.Errorf("%w: Options.Vendor, served as BridgedDeviceBasicInformation VendorName: %w", ErrConfiguration, err)
		}
	}
	if p == nil {
		return nil
	}
	var both []string
	if opts.Name != "" && p.NodeLabel != nil {
		both = append(both, "Options.Name and NodeLabel")
	}
	if opts.Vendor != "" && p.VendorName != nil {
		both = append(both, "Options.Vendor and VendorName")
	}
	if len(both) > 0 {
		return fmt.Errorf("%w: %s state one fact twice", ErrConfiguration, strings.Join(both, ", "))
	}
	return nil
}

// copyPatch copies p with its own pointees, so a host's later change of a
// value it declared changes nothing the device serves.
func copyPatch(p bdbidef.Patch) bdbidef.Patch {
	src := reflect.ValueOf(p)
	out := reflect.New(src.Type()).Elem()
	for i := range src.NumField() {
		f := src.Field(i)
		if f.Kind() != reflect.Pointer || f.IsNil() {
			continue
		}
		c := reflect.New(f.Elem().Type())
		c.Elem().Set(f.Elem())
		out.Field(i).Set(c)
	}
	c, _ := out.Interface().(bdbidef.Patch)
	return c
}
