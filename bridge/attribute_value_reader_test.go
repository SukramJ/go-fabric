// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for attributeValueReader, primitiveAttributeValue and
// skipContainerTLV. Lives in package bridge to access unexported functions.
// The ACL, Extension and GroupKeyMap writes decode through their generated
// definitions (written_value_refused_test.go).

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// advanceToContent advances dec past the first element and returns the element.
func advanceToContent(t *testing.T, dec *tlv.Decoder) tlv.Element {
	t.Helper()
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("advanceToContent: %v", err)
	}
	return el
}

// ─── primitiveAttributeValue ──────────────────────────────────────────────────

func TestPrimitiveAttributeValue_Null(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutNull(tlv.AnonymousTag())
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsNull {
		t.Error("expected IsNull=true for null element")
	}
}

func TestPrimitiveAttributeValue_BoolTrue(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutBool(tlv.AnonymousTag(), true)
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value != true {
		t.Errorf("expected true, got %v", got.Value)
	}
}

func TestPrimitiveAttributeValue_BoolFalse(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutBool(tlv.AnonymousTag(), false)
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value != false {
		t.Errorf("expected false, got %v", got.Value)
	}
}

func TestPrimitiveAttributeValue_UTF8String(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutUTF8(tlv.AnonymousTag(), "hello")
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Production code uses el.Octets for UTF8 strings; decoder puts value in
	// el.String, so Octets is empty — resulting in "". This is a known
	// current behavior: we verify no error and the returned string type.
	if _, ok := got.Value.(string); !ok {
		t.Errorf("expected string value type, got %T", got.Value)
	}
}

func TestPrimitiveAttributeValue_OctetStr(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutOctets(tlv.AnonymousTag(), []byte{0x01, 0x02, 0x03})
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, ok := got.Value.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", got.Value)
	}
	if len(b) != 3 || b[0] != 0x01 || b[1] != 0x02 || b[2] != 0x03 {
		t.Errorf("unexpected bytes: %v", b)
	}
}

func TestPrimitiveAttributeValue_Uint(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutUint(tlv.AnonymousTag(), 42)
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value != uint64(42) {
		t.Errorf("expected uint64(42), got %v", got.Value)
	}
}

func TestPrimitiveAttributeValue_Container_DrainsToUndecoded(t *testing.T) {
	t.Parallel()
	// Build a structure with some content; primitiveAttributeValue drains
	// it and answers an undecoded value, never a null (R2-P6-wire-0442).
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 99)
	_ = enc.EndContainer()
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error draining container: %v", err)
	}
	if _, undecoded := got.Value.(undecodedValue); !undecoded || got.IsNull {
		t.Errorf("expected an undecoded value after the container drain, got %+v", got)
	}
}

// ─── skipContainerTLV ────────────────────────────────────────────────────────

func TestSkipContainerTLV_EmptyStruct(t *testing.T) {
	t.Parallel()
	// Encode: struct-open, end-container; give decoder bytes after the opener.
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	_ = enc.EndContainer()
	raw, _ := enc.Bytes()

	// Consume the opener so dec is positioned at the EndContainer.
	dec := tlv.NewDecoder(raw)
	if _, err := dec.Next(); err != nil { // struct opener
		t.Fatalf("consume opener: %v", err)
	}
	if err := skipContainerTLV(dec); err != nil {
		t.Fatalf("skipContainerTLV: %v", err)
	}
}

func TestSkipContainerTLV_NestedContainers(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(0))
	enc.PutUint(tlv.AnonymousTag(), 1)
	_ = enc.EndContainer() // end array
	_ = enc.EndContainer() // end struct
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	if _, err := dec.Next(); err != nil { // outer struct opener
		t.Fatalf("consume outer opener: %v", err)
	}
	if err := skipContainerTLV(dec); err != nil {
		t.Fatalf("skipContainerTLV with nested: %v", err)
	}
}

// ─── attributeValueReader ────────────────────────────────────────────────────

func buildACLArrayTLV(entries []mattercore.AccessControlEntryStruct) []byte {
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.AnonymousTag())
	for _, e := range entries {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), uint64(e.Privilege))
		enc.PutUint(tlv.ContextTag(2), uint64(e.AuthMode))
		enc.PutNull(tlv.ContextTag(3)) // subjects null
		enc.PutNull(tlv.ContextTag(4)) // targets null
		enc.PutUint(tlv.ContextTag(254), uint64(e.FabricIndex))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		panic("buildACLArrayTLV: " + err.Error())
	}
	return b
}

func TestAttributeValueReader_ACLCluster(t *testing.T) {
	t.Parallel()
	// cluster=0x001F attribute=0x0000 → ACL path
	path := im.ConcreteAttributePath{
		HasCluster: true, HasAttribute: true,
		Cluster: 0x001F, Attribute: 0x0000,
	}
	raw := buildACLArrayTLV([]mattercore.AccessControlEntryStruct{
		{Privilege: 3, AuthMode: 1},
	})
	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)

	av, err := attributeValueReader(path, el, dec)
	if err != nil {
		t.Fatalf("attributeValueReader: %v", err)
	}
	list, ok := av.Value.([]mattercore.AccessControlEntryStruct)
	if !ok || len(list) != 1 {
		t.Errorf("expected 1-entry ACL list, got %v", av.Value)
	}
}

func TestAttributeValueReader_OtherCluster_Primitive(t *testing.T) {
	t.Parallel()
	// Any other path → primitiveAttributeValue
	path := im.ConcreteAttributePath{
		HasCluster: true, HasAttribute: true,
		Cluster: 0x0006, Attribute: 0x0000,
	}
	enc := tlv.NewEncoder()
	enc.PutBool(tlv.AnonymousTag(), true)
	raw, _ := enc.Bytes()
	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)

	av, err := attributeValueReader(path, el, dec)
	if err != nil {
		t.Fatalf("attributeValueReader: %v", err)
	}
	if av.Value != true {
		t.Errorf("expected true, got %v", av.Value)
	}
}

// TestPrimitiveAttributeValue_UTF8String_ReturnsDecodedContent verifies that
// primitiveAttributeValue returns the textual content from el.String for UTF-8
// string elements, not string(el.Octets) (which is always empty because the
// decoder only populates el.Octets for octet-string types). The pre-fix code
// path read el.Octets for UTF-8 elements, silently dropping every string write
// such as NodeLabel or Location to the empty string "".
// Mirrors matter.js packages/node/src/behaviors/basic-information/
// BasicInformationServer.ts nodeLabel write path.
func TestPrimitiveAttributeValue_UTF8String_ReturnsDecodedContent(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutUTF8(tlv.AnonymousTag(), "NodeLabel-Test")
	raw, _ := enc.Bytes()

	dec := tlv.NewDecoder(raw)
	el := advanceToContent(t, dec)
	got, err := primitiveAttributeValue(el, dec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.Value.(string)
	if !ok {
		t.Fatalf("expected string value, got %T", got.Value)
	}
	if s != "NodeLabel-Test" {
		t.Errorf("primitiveAttributeValue UTF-8 = %q, want %q (el.String=%q el.OctetsLen=%d)",
			s, "NodeLabel-Test", el.String, len(el.Octets))
	}
}
