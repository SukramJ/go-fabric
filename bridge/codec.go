// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "thingfabric.dev/go-fabric/im"

// Codec is the bridge's Interaction Model codec of the endpoint tree it
// serves: how a command's fields and a written value are read off the wire
// into what the cluster servers take — the hand-written servers'
// (OperationalCredentials, GeneralCommissioning, AccessControl) own request
// types among them — and how a response's fields and a reported value go
// back. A server of the same tree on another stack — a node serves it on
// the sessions its client stack opened too (docs/adr/0032) — decodes and
// encodes with it, as the bridge does.
type Codec struct {
	FieldsReader im.CommandFieldsReader
	FieldsWriter im.CommandFieldsWriter
	ValueReader  im.AttributeValueReader
	ValueWriter  im.AttributeValueWriter
}

// Codec returns the bridge's [Codec].
func (b *Bridge) Codec() Codec {
	return Codec{
		FieldsReader: commandFieldsReader,
		FieldsWriter: defaultCommandFieldsWriter,
		ValueReader:  attributeValueReader,
		ValueWriter:  defaultAttributeValueWriter,
	}
}
