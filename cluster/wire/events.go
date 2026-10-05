// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

// FieldlessEvent is the payload of a cluster event that declares no
// fields — SmokeCoAlarm HardwareFault / EndOfService / SelfTestComplete /
// AlarmMuted / MuteEnded / AllClear (matter.js
// smoke-co-alarm-cluster.element.ts:59-72) and every
// PumpConfigurationAndControl alarm event
// (pump-configuration-and-control.element.ts:112-128) among them.
//
// The bridge's value writer encodes it as an empty structure: the
// EventDataIB Data slot (tag 7) still has to carry a well-formed
// element, and chip's StructDecodeIterator expects a structure there
// even when it has nothing to iterate — the same reasoning
// BasicInformation's ShutDown event already follows. A nil payload
// would encode as TLV null, which a controller decoding the event as
// its (empty) struct type rejects.
type FieldlessEvent struct{}
