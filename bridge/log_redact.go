// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// redactedValue is the text a secret-bearing value is logged as when it
// cannot be redacted field by field: spec.Redact's placeholder.
const redactedValue = "(redacted)"

// loggedAttributeValue is an attribute value as the module's debug logs
// carry it: the value with every part the model marks secret replaced.
// The attribute read log of
// matter.js packages/protocol/src/action/server/AttributeReadResponse.ts:303-306 at b25ed960
// passes the value through SecretRedaction.redact; this module redacts
// always, without matter.js's Logger.redactSecrets switch.
//
// It is resolved when a handler writes the record ([slog.LogValuer]), so a
// record a handler drops costs nothing. A path whose attribute model holds
// no secret logs the Go value as it is, with no encoding; a path that holds
// one logs the value's wire form ([defaultAttributeValueWriter], read back
// to a [spec.Node]) redacted by the model ([spec.Redact]), rendered as the
// untyped JSON form of [spec.DecodeJSON] — struct members keyed by field
// id. It fails closed: a value the writer cannot encode (it writes null
// for a Go type it does not know) or a form that does not read back is
// logged as "(redacted)", never raw. A null stays null.
type loggedAttributeValue struct {
	cluster, attribute uint32
	v                  im.AttributeValue
}

// LogValue implements [slog.LogValuer].
func (l loggedAttributeValue) LogValue() slog.Value {
	field := secretBearingAttribute(l.cluster, l.attribute)
	if field == nil || l.v.IsNull || l.v.Value == nil {
		return slog.AnyValue(l.v.Value)
	}
	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.AnonymousTag(), l.v)
	wire, err := enc.Bytes()
	if err != nil {
		return slog.StringValue(redactedValue)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(wire))
	if err != nil || n.El.IsNull || n.El.Type == tlv.TypeNull {
		return slog.StringValue(redactedValue)
	}
	out, err := spec.DecodeJSON(nil, spec.Redact(field, n))
	if err != nil {
		return slog.StringValue(redactedValue)
	}
	return slog.StringValue(string(out))
}

// secretBearingAttribute is the model of the attribute at (cluster,
// attribute) when it, or any member of it, is secret; nil for an attribute
// without a secret and for a path no registered definition describes (no
// model, nothing to redact: SecretRedaction.redact without a model,
// matter.js packages/model/src/logic/SecretRedaction.ts:32-37 at b25ed960).
func secretBearingAttribute(cluster, attribute uint32) *spec.Field {
	def := spec.Lookup(cluster)
	if def == nil {
		return nil
	}
	a := def.Attribute(attribute)
	if a == nil {
		return nil
	}
	f := spec.AttributeField(a)
	if !spec.ContainsSecret(f) {
		return nil
	}
	return f
}
