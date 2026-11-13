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
//
// MatterIdentify runs with the server's IdentifyTime queue held: a request
// holds it from the handler's call to the stored value, the countdown from
// its decrement to the stop it reports with 0 (cluster/core, Identify.op —
// matter.js runs its tick under the behaviour's lock and the handler as a
// reactor of the same value, IdentifyServer.ts:54, :58 at db021da0). The
// handler therefore must not set IdentifyTime on the same server from
// inside the call — neither an IdentifyTime write nor the Identify command,
// directly or by waiting for a goroutine that does: the nested request
// waits for the queue the outer call holds, and neither returns. A handler
// that wants a different IdentifyTime returns and sets it afterwards, from
// another goroutine. It should return promptly for the same reason: while
// it runs, the countdown does not tick and no other request for this
// server's IdentifyTime is served. MatterTriggerEffect is called without
// the queue.
//
// Removing the endpoint does not wait for the handler: the server's Close
// returns while a MatterIdentify is still running — a request's, or the
// stop of a countdown that just ended, which is called from a goroutine of
// its own — so a handler may take a lock that is held across the removal,
// and may return into a server that is closed.
type IdentifyHandler interface {
	// MatterIdentify is the Identify command or an IdentifyTime write:
	// identify for identifyTime seconds, 0 stops. It runs with the
	// server's IdentifyTime queue held and must not set IdentifyTime on
	// the same server from inside the call (see the type's comment).
	MatterIdentify(ctx context.Context, identifyTime uint16) error
	// MatterTriggerEffect is the TriggerEffect command: the effect
	// identifier and variant (IdentifyEffectIdentifierEnum,
	// IdentifyEffectVariantEnum).
	MatterTriggerEffect(ctx context.Context, effectIdentifier, effectVariant uint8) error
}
