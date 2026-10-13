// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// Decoded TLV and the reports the device sends, in the shape a test
// asserts over — converted from controller/interaction's, which reads the
// ReportData chunks and merges them.

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
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

// u8 narrows an unsigned value to the width its field declares; the
// device encodes no wider value there.
func (n TLV) u8() uint8 { return uint8(n.Uint & 0xFF) }

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

// fromNode converts a node tree of controller/interaction into a TLV.
func fromNode(n spec.Node) TLV {
	t := TLV{Element: n.El}
	if len(n.Children) > 0 {
		t.Children = make([]TLV, len(n.Children))
		for i, c := range n.Children {
			t.Children[i] = fromNode(c)
		}
	}
	return t
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
}

// fromReport converts a report of controller/interaction.
func fromReport(r *interaction.Report) *Report {
	out := &Report{SubscriptionID: r.SubscriptionID}
	for i := range r.Attributes {
		a := &r.Attributes[i]
		out.Attributes = append(out.Attributes, AttributeData{Path: a.Path, DataVersion: a.DataVersion, Value: fromNode(a.Value)})
	}
	for _, s := range r.Statuses {
		out.Statuses = append(out.Statuses, AttributeStatus{Path: s.Path, Status: s.Status})
	}
	return out
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
