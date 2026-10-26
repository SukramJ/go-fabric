// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

// The Large Message quality ("L"). A command whose matter.js
// model element carries it (Quality.largeMessage —
// ../matter.js/packages/model/src/aspects/Quality.ts:71, the "L" flag at
// :259) may carry a payload larger than one UDP datagram, so its invoke needs
// a transport that carries large messages. matter.js's client reads it from
// the request command's schema
// (../matter.js/packages/protocol/src/action/client/ClientInteraction.ts:111-127
// inferLargeMessage) and sends the invoke as a large-message request when
// the caller did not decide (:716-719).
//
// largeMessageCommands (large_message_gen.go) is generated from
// parity/schema.json by script/generate_matter_schema.go: every request
// command of the snapshot whose effective quality carries "largeMessage".
// Only request commands are listed — the client probes the command it
// sends; the responses that carry "L" at the pin answer those requests.

// CommandLargeMessage reports whether the request command (cluster, command)
// carries the matter.js Large Message quality ("L"). Mirrors the
// `command.schema?.effectiveQuality?.largeMessage` probe of matter.js
// ClientInteraction.ts:111-127 (inferLargeMessage).
func CommandLargeMessage(clusterID, commandID uint32) bool {
	_, ok := largeMessageCommands[clusterID][commandID]
	return ok
}
