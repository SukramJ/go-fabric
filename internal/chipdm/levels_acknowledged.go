// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chipdm

// LevelAcknowledged are the reviewed differences of the specification-level
// cross-check (CompareLevel): per entry the class, the path pattern (a
// leading "*." stands for the level segment), the presence on each side,
// the reason and its source. Never a value copied from the XML: the paths
// are ids and the values "present" or "absent".
var LevelAcknowledged = []Acknowledgement{
	{
		Class: ClassMatterJS, Path: "*.cluster.0x0201.response.0xfd", Property: "presence", Chip: absent, Ours: present,
		Reason: "AtomicResponse (0xFD) is a global command in CHIP's data model (globals/), which matter.js models in " +
			"the clusters that use it; the whole-snapshot cross-check reads it the same way",
		Source: "internal/chipdm/chipxml.go (globals: global commands, matter.js load-data-model.ts globalCommands); " +
			"parity/schema.json Thermostat command 0xFD",
	},
	{
		Class: ClassMatterJS, Path: "*.cluster.0x0201.request.0xfe", Property: "presence", Chip: absent, Ours: present,
		Reason: "AtomicRequest (0xFE) is a global command in CHIP's data model (globals/), which matter.js models in " +
			"the clusters that use it; the whole-snapshot cross-check reads it the same way",
		Source: "internal/chipdm/chipxml.go (globals: global commands, matter.js load-data-model.ts globalCommands); " +
			"parity/schema.json Thermostat command 0xFE",
	},
	{
		Class: ClassMatterJS, Path: "*.deviceType.0x0091", Property: "presence", Chip: present, Ours: absent,
		Reason: "Measured: ThreadBorderRouter (0x0091) is in CHIP's data_model/1.4, 1.4.1 and 1.4.2 and not in " +
			"matter.js's model at v0.13.0, v0.14.0 and v0.16.0 (parity/levels). The level table is matter.js's " +
			"release history by decision, so a ThreadBorderRouter device is withheld below 1.5.1; whether that " +
			"should change is an open question of brief S3",
		Source: "docs/adr/0030-the-served-surface-is-a-value-under-policies.md decision 5; parity/levels/1.4.json, " +
			"1.4.1.json, 1.4.2.json",
	},
	{
		// Seen once the cross-check compared the directions apart
		// (R2-P6-test-2853); classified by fable-6b-w3-items.md D4.
		Class: ClassCHIP, Path: "*.cluster.0x0201.response.0x01", Property: "presence", Chip: absent, Ours: present,
		Reason: "GetRelayStatusLogResponse (0x01): matter.js's model at v0.13.0, v0.14.0 and v0.16.0 keeps the " +
			"response while its request GetRelayStatusLog (0x04) is not in the model — the response's conformance " +
			"names a command the model does not hold; CHIP's data_model/1.4, 1.4.1 and 1.4.2 hold neither. " +
			"matter.js's model drops it from v0.17.0",
		Source: "matter.js packages/model/src/standard/elements/Thermostat.ts:1229-1235 at v0.13.0, " +
			"thermostat-cluster.element.ts:338 at v0.14.0 and :335 at v0.16.0; parity/levels/1.4.json, " +
			"1.4.1.json, 1.4.2.json",
		Impact: "Nothing served depends on it: the cluster definition at the pin has no response 0x01 " +
			"(parity/levels/1.6.1.json responseCommands [2, 253]). No upstream candidate: matter.js removed it at v0.17.0.",
	},
}

// LevelsWithoutDataModel are the levels the cross-check does not compare,
// with the reason. None: 1.6.0 is compared with data_model/1.6
// (DataModelVersion; owner decision 2026-10-30, brief S4 carry-over), and
// its two differences are the Thermostat entries above.
var LevelsWithoutDataModel = map[string]string{}
