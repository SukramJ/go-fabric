// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// administratorCommissioningClusterID is AdministratorCommissioning (0x003C).
const administratorCommissioningClusterID uint32 = 0x003C

// decodeOpenCommissioningWindowRequest reads OpenCommissioningWindow
// (AdministratorCommissioning 0x00) into the [wire.OpenWindowParams] the
// cluster server takes, following matter.js
// packages/model/src/standard/elements/administrator-commissioning.element.ts:
// [0] CommissioningTimeout uint16, [1] PakePasscodeVerifier octets,
// [2] Discriminator uint16 (max 4095), [3] Iterations uint32, [4] Salt
// octets.
//
// Without it the command reached the server as the generic tag map, which
// it rejects with INVALID_COMMAND: no controller could open an enhanced
// commissioning window over the wire — the multi-admin share every
// ecosystem app offers. Found by the CHIP Python harness (TC-IDM-1.2 step
// 5, TC-CADMIN-*), whose controller opens one.
func decodeOpenCommissioningWindowRequest(dec *tlv.Decoder) (wire.OpenWindowParams, error) {
	const cmd = "OpenCommissioningWindow"
	var p wire.OpenWindowParams
	for {
		el, err := dec.Next()
		if err != nil {
			return p, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			return p, nil
		}
		if el.IsContainer {
			if err := drainContainer(dec); err != nil {
				return p, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			if p.CommissioningTimeoutSeconds, err = fieldUint16(cmd, "CommissioningTimeout", el); err != nil {
				return p, err
			}
		case 1:
			p.PAKEPasscodeVerifier = append([]byte(nil), el.Octets...)
		case 2:
			if p.Discriminator, err = fieldUint16(cmd, "Discriminator", el); err != nil {
				return p, err
			}
			if p.Discriminator > 0x0FFF {
				return p, fieldConstraintError{fmt.Sprintf("%s: Discriminator %d > 4095", cmd, p.Discriminator)}
			}
		case 3:
			if el.Uint > 0xFFFFFFFF {
				return p, fieldConstraintError{fmt.Sprintf("%s: Iterations %d > uint32 max", cmd, el.Uint)}
			}
			p.Iterations = uint32(el.Uint)
		case 4:
			p.Salt = append([]byte(nil), el.Octets...)
		}
	}
}
