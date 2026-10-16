// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel

// The entry types the generator emitted, empty, before it resolved
// ExternalIdList's ContentLauncher.AdditionalInfoStruct (register line
// 450). They name the resolved struct now, so code that spelled them keeps
// compiling and gains the Name and Value fields.
type (
	// ProgramStructExternalIdListEntry is [AdditionalInfoStruct].
	ProgramStructExternalIdListEntry = AdditionalInfoStruct
	// GetProgramGuideRequestExternalIdListEntry is [AdditionalInfoStruct].
	GetProgramGuideRequestExternalIdListEntry = AdditionalInfoStruct
)
