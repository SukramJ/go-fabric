// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// IdentifyHandler is an optional interface of an [EndpointSource]: the
// Identify server the endpoint assembler mounts on a bridged endpoint
// hands it the controller's Identify and TriggerEffect commands and its
// IdentifyTime writes, so the host makes its device blink, and calls
// MatterIdentify with 0 when the countdown reaches zero. An error refuses
// the command or the write with its status ([StatusError]; FAILURE for
// any other error) and leaves IdentifyTime as it was.
//
// matter.js's IdentifyServer runs the application's override of identify
// and triggerEffect (packages/node/src/behaviors/identify/IdentifyServer.ts:129-135
// at 862ad75f) and emits startIdentifying, stopIdentifying and
// effectTriggered (:91-105, :133-135); here the assembler's server keeps
// the IdentifyTime countdown and the host's handler runs first.
type IdentifyHandler interface {
	// MatterIdentify is the Identify command or an IdentifyTime write:
	// identify for identifyTime seconds, 0 stops.
	MatterIdentify(ctx context.Context, identifyTime uint16) error
	// MatterTriggerEffect is the TriggerEffect command: the effect
	// identifier and variant (IdentifyEffectIdentifierEnum,
	// IdentifyEffectVariantEnum).
	MatterTriggerEffect(ctx context.Context, effectIdentifier, effectVariant uint8) error
}
