// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/tlv"
)

// The harnesses below cover the decoders of a commissionee's command
// responses and attributes. The commissioner reads them from the peer
// during commissioning. They are harness only: the functions are on the
// commissioning path and are not changed here. The spec.Node inputs are
// built by decoding the fuzz bytes as one TLV element, the way the client
// interaction hands a response's fields over. An input that is not one
// element is skipped.

// fuzzNode decodes b as one TLV element. ok is false when it is not one.
func fuzzNode(b []byte) (spec.Node, bool) {
	if len(b) > 1<<16 {
		return spec.Node{}, false
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	return n, err == nil
}

// fuzzEncode is the TLV of e under an anonymous tag.
func fuzzEncode(f *testing.F, e spec.Encodable) []byte {
	f.Helper()
	enc := tlv.NewEncoder()
	e.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		f.Fatal(err)
	}
	return b
}

// encodableFunc adapts a function to spec.Encodable.
type encodableFunc func(enc *tlv.Encoder, tag tlv.Tag)

func (fn encodableFunc) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { fn(enc, tag) }

// FuzzDecodeCertificateChainResponse: decodeCertificateChainResponse
// decodes or returns an error and never panics. An accepted certificate
// is at most 600 bytes.
func FuzzDecodeCertificateChainResponse(f *testing.F) {
	f.Add(fuzzEncode(f, encodableFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutOctets(tlv.ContextTag(0), make([]byte, 400))
		_ = enc.EndContainer()
	})))
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, b []byte) {
		n, ok := fuzzNode(b)
		if !ok {
			return
		}
		cert, err := decodeCertificateChainResponse(n)
		if err == nil && len(cert) > 600 {
			t.Fatalf("accepted a certificate of %d bytes", len(cert))
		}
	})
}

// FuzzDecodeNOCResponse: decodeNOCResponse decodes or returns an error
// and never panics. An accepted FabricIndex lies in 1..254 and DebugText
// is at most 128 UTF-16 units.
func FuzzDecodeNOCResponse(f *testing.F) {
	f.Add(fuzzEncode(f, encodableFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), 0)
		enc.PutUint(tlv.ContextTag(1), 1)
		enc.PutUTF8(tlv.ContextTag(2), "ok")
		_ = enc.EndContainer()
	})))
	f.Add([]byte{0x15, 0x24, 0x00, 0x05, 0x18})
	f.Fuzz(func(t *testing.T, b []byte) {
		n, ok := fuzzNode(b)
		if !ok {
			return
		}
		r, err := decodeNOCResponse(n)
		if err != nil {
			return
		}
		if r.fabricIndex != nil && (*r.fabricIndex < 1 || *r.fabricIndex > 254) {
			t.Fatalf("FabricIndex %d outside 1..254", *r.fabricIndex)
		}
		if r.debugText != nil && spec.StringLength(*r.debugText) > 128 {
			t.Fatalf("DebugText of %d units", spec.StringLength(*r.debugText))
		}
	})
}

// FuzzDecodeCertSigningRequest: decodeCertSigningRequest, over the
// NOCSRElements bytes, decodes or returns an error and never panics.
func FuzzDecodeCertSigningRequest(f *testing.F) {
	f.Add(fuzzEncode(f, encodableFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutOctets(tlv.ContextTag(1), []byte{0x30, 0x00})
		enc.PutOctets(tlv.ContextTag(2), make([]byte, 32))
		_ = enc.EndContainer()
	})))
	f.Add([]byte{0x15})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<16 {
			return
		}
		_, _ = decodeCertSigningRequest(b)
	})
}

// FuzzDecodeNetworks: decodeNetworks, over a Networks attribute value,
// decodes or returns an error and never panics. The decoded list holds no
// more entries than the input has bytes.
func FuzzDecodeNetworks(f *testing.F) {
	f.Add(fuzzEncode(f, encodableFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		netcomm.NetworkInfoStruct{NetworkId: []byte("home"), Connected: true}.EncodeTLV(enc, tlv.AnonymousTag())
		netcomm.NetworkInfoStruct{NetworkId: []byte{1, 2, 3, 4, 5, 6, 7, 8}}.EncodeTLV(enc, tlv.AnonymousTag())
		_ = enc.EndContainer()
	})))
	f.Add([]byte{0x16, 0x18})
	f.Fuzz(func(t *testing.T, b []byte) {
		n, ok := fuzzNode(b)
		if !ok {
			return
		}
		nets, err := decodeNetworks(n)
		if err == nil && len(nets) > len(b) {
			t.Fatalf("decoded %d networks from %d bytes", len(nets), len(b))
		}
	})
}
