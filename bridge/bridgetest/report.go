// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// Decoded TLV and the ReportData the device sends, in the shape a test
// asserts over.

import (
	"errors"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TLV is a decoded TLV element and, for a container, its children.
type TLV struct {
	tlv.Element
	Children []TLV
}

// Child returns the direct child with context tag tag.
func (n TLV) Child(tag uint8) (TLV, bool) {
	for _, c := range n.Children {
		if c.Tag.Kind == tlv.TagKindContext && c.Tag.Number == uint32(tag) {
			return c, true
		}
	}
	return TLV{}, false
}

// u8, u16 and u32 narrow an unsigned value to the width its field
// declares; the device encodes no wider value there.
func (n TLV) u8() uint8   { return uint8(n.Uint & 0xFF) }
func (n TLV) u16() uint16 { return uint16(n.Uint & 0xFFFF) }
func (n TLV) u32() uint32 { return uint32(n.Uint & 0xFFFF_FFFF) }

// DecodeTLV decodes the first element of raw.
func DecodeTLV(raw []byte) (TLV, error) {
	dec := tlv.NewDecoder(raw)
	el, err := dec.Next()
	if err != nil {
		return TLV{}, err
	}
	return decodeNode(dec, el)
}

func decodeNode(dec *tlv.Decoder, el tlv.Element) (TLV, error) {
	n := TLV{Element: el}
	if !el.IsContainer {
		return n, nil
	}
	for {
		c, err := dec.Next()
		if err != nil {
			return TLV{}, err
		}
		if c.IsEndContainer {
			return n, nil
		}
		child, err := decodeNode(dec, c)
		if err != nil {
			return TLV{}, err
		}
		n.Children = append(n.Children, child)
	}
}

// AttributeData is one AttributeDataIB of a report.
type AttributeData struct {
	Path        im.ConcreteAttributePath
	DataVersion uint32
	Value       TLV
}

// AttributeStatus is one AttributeStatusIB of a report.
type AttributeStatus struct {
	Path   im.ConcreteAttributePath
	Status im.StatusCode
}

// Report is a ReportData, its chunks merged.
type Report struct {
	// SubscriptionID is the report's subscription, 0 for a read.
	SubscriptionID uint32
	Attributes     []AttributeData
	Statuses       []AttributeStatus

	more, suppress bool // of the last chunk
}

// Value returns the first value the report carries for the attribute.
func (r *Report) Value(endpoint uint16, cluster, attribute uint32) (TLV, bool) {
	for i := range r.Attributes {
		if a := &r.Attributes[i]; a.Path.Endpoint == endpoint && a.Path.Cluster == cluster && a.Path.Attribute == attribute {
			return a.Value, true
		}
	}
	return TLV{}, false
}

// Status returns the status the report carries for the attribute.
func (r *Report) Status(endpoint uint16, cluster, attribute uint32) (im.StatusCode, bool) {
	for _, s := range r.Statuses {
		if s.Path.Endpoint == endpoint && s.Path.Cluster == cluster && s.Path.Attribute == attribute {
			return s.Status, true
		}
	}
	return 0, false
}

// merge appends chunk to r (nil r starts a report).
func (r *Report) merge(chunk *Report) *Report {
	if r == nil {
		return chunk
	}
	r.Attributes = append(r.Attributes, chunk.Attributes...)
	r.Statuses = append(r.Statuses, chunk.Statuses...)
	r.more, r.suppress = chunk.more, chunk.suppress
	return r
}

// decodeReport decodes one ReportData message (im/read.go:28-45:
// [0] SubscriptionId, [1] AttributeReports of AttributeReportIB {[0]
// AttributeStatusIB {[0] path, [1] StatusIB}, [1] AttributeDataIB {[0]
// DataVersion, [1] path, [2] data}}, [3] MoreChunkedMessages, [4]
// SuppressResponse).
func decodeReport(payload []byte) (*Report, error) {
	tree, err := DecodeTLV(payload)
	if err != nil {
		return nil, err
	}
	if tree.Type != tlv.TypeStructure {
		return nil, errors.New("bridgetest: ReportData is not a structure")
	}
	r := &Report{}
	if id, ok := tree.Child(0); ok {
		r.SubscriptionID = id.u32()
	}
	if more, ok := tree.Child(3); ok {
		r.more = more.Bool
	}
	if suppress, ok := tree.Child(4); ok {
		r.suppress = suppress.Bool
	}
	reports, _ := tree.Child(1)
	for _, ib := range reports.Children {
		if data, ok := ib.Child(1); ok {
			version, _ := data.Child(0)
			path, _ := data.Child(1)
			value, _ := data.Child(2)
			r.Attributes = append(r.Attributes, AttributeData{Path: attributePath(path), DataVersion: version.u32(), Value: value})
			continue
		}
		status, _ := ib.Child(0)
		path, _ := status.Child(0)
		sib, _ := status.Child(1)
		code, _ := sib.Child(0)
		r.Statuses = append(r.Statuses, AttributeStatus{Path: attributePath(path), Status: im.StatusCode(code.u8())})
	}
	return r, nil
}

// attributePath reads an AttributePathIB (im/path.go:17-22).
func attributePath(n TLV) im.ConcreteAttributePath {
	var p im.ConcreteAttributePath
	if v, ok := n.Child(2); ok {
		p.Endpoint, p.HasEndpoint = v.u16(), true
	}
	if v, ok := n.Child(3); ok {
		p.Cluster, p.HasCluster = v.u32(), true
	}
	if v, ok := n.Child(4); ok {
		p.Attribute, p.HasAttribute = v.u32(), true
	}
	return p
}
