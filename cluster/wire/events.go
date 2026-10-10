// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

import "github.com/SukramJ/go-fabric/tlv"

// FieldlessEvent is the payload of a cluster event that declares no
// fields — SmokeCoAlarm HardwareFault / EndOfService / SelfTestComplete /
// AlarmMuted / MuteEnded / AllClear (matter.js
// smoke-co-alarm-cluster.element.ts:59-72) and every
// PumpConfigurationAndControl alarm event
// (pump-configuration-and-control.element.ts:112-128) among them.
//
// It encodes as an empty structure, as every generated fieldless event
// payload does (e.g. cluster/spec/smokecoalarm HardwareFaultEvent): the
// EventDataIB Data slot (tag 7) still has to carry a well-formed
// element, and chip's StructDecodeIterator expects a structure there
// even when it has nothing to iterate — the same reasoning
// BasicInformation's ShutDown event already follows. A nil payload
// would encode as TLV null, which a controller decoding the event as
// its (empty) struct type rejects.
type FieldlessEvent struct{}

// EncodeTLV implements spec.Encodable: an empty structure.
func (FieldlessEvent) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	_ = enc.EndContainer()
}
