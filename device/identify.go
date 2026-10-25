// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/contract"
)

// The endpoint assembler mounts Identify on every bridged endpoint and
// keeps its IdentifyTime countdown; the device carries the host's handlers
// of its two commands and hands them to the assembler's server
// ([contract.IdentifyHandler]). An identifydef.Declare on a device states
// those handlers only — HandleIdentify and HandleTriggerEffect — and
// [Device.Identify] registers or replaces them at run time.
//
// matter.js's application overrides IdentifyServer's identify and
// triggerEffect (packages/node/src/behaviors/identify/IdentifyServer.ts:129-135
// at 862ad75f).

var _ contract.IdentifyHandler = (*Device)(nil)

// declareIdentify takes an identifydef.Declare: its command handlers, and
// nothing the assembler's server could not honour.
func (s *settings) declareIdentify(d Declaration, decl spec.ClusterDeclaration) error {
	var onWrite bool
	switch o := d.(type) {
	case identifydef.Declare:
		onWrite = o.OnWrite != nil
	case *identifydef.Declare:
		onWrite = o != nil && o.OnWrite != nil
	}
	if onWrite || decl.Server != nil || decl.HasFeatures || len(decl.Initial) > 0 || hasPorts(decl.Ports) ||
		len(decl.Elements.Attributes)+len(decl.Elements.Events) > 0 {
		return fmt.Errorf("%w: the endpoint assembler mounts Identify (0x0003) itself; its declaration takes HandleIdentify and HandleTriggerEffect only", ErrConfiguration)
	}
	for _, c := range decl.Elements.Commands {
		if c != identifydef.CmdIdentify && c != identifydef.CmdTriggerEffect {
			return fmt.Errorf("%w: Identify has no command 0x%02X", ErrConfiguration, c)
		}
	}
	if decl.Attach != nil {
		s.identify = append(s.identify, decl.Attach)
	}
	return nil
}

// newIdentifyHandlers builds the holder of the device's Identify handlers:
// a generated Identify server that is never mounted, whose commands are
// unimplemented until a handler is installed.
func newIdentifyHandlers(s *settings) (*spec.Server, error) {
	srv, err := spec.NewServer(identifydef.Definition, spec.Options{Commands: []uint32{identifydef.CmdTriggerEffect}}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		return nil, err
	}
	for _, attach := range s.identify {
		if err := attach(srv); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConfiguration, err)
		}
	}
	return srv, nil
}

// Identify is the device's Identify handlers, typed: HandleIdentify and
// HandleTriggerEffect install (or with nil remove) the host's handler of
// the controller's Identify and TriggerEffect commands on the Identify
// server the endpoint assembler mounts for the device. It is not that
// server: IdentifyTime and its countdown stay the assembler's, and a write
// veto (OnWrite) is not honoured.
func (d *Device) Identify() *identifydef.Server { return identifydef.Wrap(d.identify) }

// MatterIdentify implements [contract.IdentifyHandler]: the host's
// HandleIdentify, when installed.
func (d *Device) MatterIdentify(ctx context.Context, identifyTime uint16) error {
	if d.identify == nil || !d.identify.Handles(identifydef.CmdIdentify) {
		return nil
	}
	_, err := d.identify.MatterInvoke(ctx, identifydef.CmdIdentify, identifydef.IdentifyRequest{IdentifyTime: identifyTime})
	return err
}

// MatterTriggerEffect implements [contract.IdentifyHandler]: the host's
// HandleTriggerEffect, when installed.
func (d *Device) MatterTriggerEffect(ctx context.Context, effectIdentifier, effectVariant uint8) error {
	if d.identify == nil || !d.identify.Handles(identifydef.CmdTriggerEffect) {
		return nil
	}
	_, err := d.identify.MatterInvoke(ctx, identifydef.CmdTriggerEffect, identifydef.TriggerEffectRequest{
		EffectIdentifier: identifydef.EffectIdentifierEnum(effectIdentifier), EffectVariant: identifydef.EffectVariantEnum(effectVariant),
	})
	return err
}
