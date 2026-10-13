// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ReadRequestMessage tags — matter.js
// packages/types/src/protocol/messages/TlvReadRequest.ts:19-35.
const (
	tagReadAttributeRequests = 0
	tagReadIsFabricFiltered  = 3
	tagRevision              = 0xFF
)

// ReportDataMessage tags (Matter §10.7.3; matter.js TlvDataReport) and
// those of its AttributeReportIB, AttributeDataIB, AttributeStatusIB,
// StatusIB and AttributePathIB.
const (
	tagReportAttributeReports = 1
	tagReportMoreChunked      = 3
	tagReportSuppressResponse = 4
	tagAttrReportStatus       = 0
	tagAttrReportData         = 1
	tagAttrStatusPath         = 0
	tagAttrStatusStatus       = 1
	tagStatusIBStatus         = 0
	tagStatusIBClusterStatus  = 1
	tagAttrDataDataVersion    = 0
	tagAttrDataPath           = 1
	tagAttrDataData           = 2
	tagAttrPathEndpoint       = 2
	tagAttrPathCluster        = 3
	tagAttrPathAttribute      = 4
	tagAttrPathListIndex      = 5
	tagAttrPathNode           = 1
	listIndexNullType         = tlv.TypeNull
)

// AttributeData is one attribute value of a report.
type AttributeData struct {
	Path        im.ConcreteAttributePath
	DataVersion uint32
	Value       spec.Node
}

// AttributeStatus is one attribute status of a report.
type AttributeStatus struct {
	Path          im.ConcreteAttributePath
	Status        im.StatusCode
	ClusterStatus *uint8
}

// Report is the data of a read, its chunks merged.
type Report struct {
	Attributes []AttributeData
	Statuses   []AttributeStatus
}

// Value returns the value the report carries for the attribute.
func (r *Report) Value(endpoint uint16, cluster, attribute uint32) (spec.Node, bool) {
	for i := range r.Attributes {
		if a := &r.Attributes[i]; a.Path.Endpoint == endpoint && a.Path.Cluster == cluster && a.Path.Attribute == attribute {
			return a.Value, true
		}
	}
	return spec.Node{}, false
}

// Read reads attributes on a fresh exchange and returns the report.
// Mirrors matter.js ClientInteraction.read (ClientInteraction.ts:262-298)
// with a Read() of attribute selectors (action/request/Read.ts:28-91:
// fabric-filtered by default, the InteractionModelRevision set) and
// IncomingInteractionClientMessenger.readDataReports
// (interaction/InteractionMessenger.ts:938-972) at 0ceb202d: a
// StatusResponse Success after every chunk with more to come, and after
// the last one unless it suppresses the response — that last one sent
// before the exchange closes (the tracked end message, :951-961), a
// failure to send it only logged. A list split across chunks (an
// AttributeDataIB with a null ListIndex appends an element) is
// reassembled into one value, as matter.js's InputChunk does. Errors of
// the exchange layer pass through unchanged.
func (c *Interaction) Read(ctx context.Context, paths ...im.ConcreteAttributePath) (*Report, error) {
	m, err := c.begin()
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	req, err := encodeReadRequest(paths, true)
	if err != nil {
		return nil, err
	}
	if err := m.send(ctx, im.OpcodeReadRequest, req, 0); err != nil {
		return nil, err
	}
	report := &Report{}
	for {
		msg, err := m.next(ctx, im.OpcodeReportData, 0)
		if err != nil {
			return nil, err
		}
		more, suppress, err := report.merge(msg.Payload)
		if err != nil {
			return nil, err
		}
		if more {
			if err := m.sendStatus(ctx, im.StatusSuccess); err != nil {
				return nil, err
			}
			continue
		}
		if !suppress {
			if err := m.sendStatus(context.WithoutCancel(ctx), im.StatusSuccess); err != nil {
				c.log.Info("matter.controller.interaction.read_final_status_failed", slog.String("err", err.Error()))
			}
		}
		return report, nil
	}
}

// encodeReadRequest is TlvReadRequest.encode of a Read(): [0] the
// attribute paths when there are any, [3] isFabricFiltered, [255] the
// InteractionModelRevision.
func encodeReadRequest(paths []im.ConcreteAttributePath, fabricFiltered bool) ([]byte, error) {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	if len(paths) > 0 {
		enc.StartArray(tlv.ContextTag(tagReadAttributeRequests))
		for _, p := range paths {
			p.MarshalTLV(enc, tlv.AnonymousTag())
		}
		_ = enc.EndContainer()
	}
	enc.PutBool(tlv.ContextTag(tagReadIsFabricFiltered), fabricFiltered)
	enc.PutUint(tlv.ContextTag(tagRevision), uint64(im.MatterInteractionModelRevision))
	_ = enc.EndContainer()
	return enc.Bytes()
}

// merge decodes one ReportData chunk into r and returns its
// MoreChunkedMessages and SuppressResponse flags.
func (r *Report) merge(payload []byte) (more, suppress bool, err error) {
	n, err := decodeNode(payload)
	if err != nil {
		return false, false, err
	}
	if n.El.Type != tlv.TypeStructure {
		return false, false, fmt.Errorf("%w: ReportData is not a structure", ErrUnexpectedMessage)
	}
	if f, ok := child(n, tagReportMoreChunked); ok {
		more = f.El.Bool
	}
	if f, ok := child(n, tagReportSuppressResponse); ok {
		suppress = f.El.Bool
	}
	reports, _ := child(n, tagReportAttributeReports)
	for _, ib := range reports.Children {
		if data, ok := child(ib, tagAttrReportData); ok {
			r.addData(data)
			continue
		}
		status, ok := child(ib, tagAttrReportStatus)
		if !ok {
			continue
		}
		path, _ := child(status, tagAttrStatusPath)
		sib, _ := child(status, tagAttrStatusStatus)
		s := AttributeStatus{Path: decodePath(path)}
		if code, ok := child(sib, tagStatusIBStatus); ok {
			s.Status = im.StatusCode(code.El.Uint) //nolint:gosec // a status code is one byte on the wire
		}
		if cs, ok := child(sib, tagStatusIBClusterStatus); ok {
			v := uint8(cs.El.Uint) //nolint:gosec // a cluster status is one byte on the wire
			s.ClusterStatus = &v
		}
		r.Statuses = append(r.Statuses, s)
	}
	return more, suppress, nil
}

// addData adds one AttributeDataIB: a null ListIndex appends its value to
// the list the same path carried last.
func (r *Report) addData(data spec.Node) {
	pathNode, _ := child(data, tagAttrDataPath)
	path := decodePath(pathNode)
	value, _ := child(data, tagAttrDataData)
	if path.ListAppend {
		for i := len(r.Attributes) - 1; i >= 0; i-- {
			a := &r.Attributes[i]
			if a.Path.Endpoint == path.Endpoint && a.Path.Cluster == path.Cluster && a.Path.Attribute == path.Attribute {
				value.El.Tag = tlv.AnonymousTag()
				a.Value.Children = append(a.Value.Children, value)
				return
			}
		}
		return
	}
	d := AttributeData{Path: path, Value: value}
	if v, ok := child(data, tagAttrDataDataVersion); ok {
		d.DataVersion = uint32(v.El.Uint) //nolint:gosec // a data version is 32 bits on the wire
	}
	r.Attributes = append(r.Attributes, d)
}

// decodePath reads an AttributePathIB (TlvAttributePath: [1] node, [2]
// endpoint, [3] cluster, [4] attribute, [5] listIndex, null to append).
func decodePath(n spec.Node) im.ConcreteAttributePath {
	var p im.ConcreteAttributePath
	if v, ok := child(n, tagAttrPathNode); ok {
		p.Node, p.HasNode = v.El.Uint, true
	}
	if v, ok := child(n, tagAttrPathEndpoint); ok {
		p.Endpoint, p.HasEndpoint = uint16(v.El.Uint), true //nolint:gosec // an endpoint id is 16 bits on the wire
	}
	if v, ok := child(n, tagAttrPathCluster); ok {
		p.Cluster, p.HasCluster = uint32(v.El.Uint), true //nolint:gosec // a cluster id is 32 bits on the wire
	}
	if v, ok := child(n, tagAttrPathAttribute); ok {
		p.Attribute, p.HasAttribute = uint32(v.El.Uint), true //nolint:gosec // an attribute id is 32 bits on the wire
	}
	if v, ok := child(n, tagAttrPathListIndex); ok {
		if v.El.Type == listIndexNullType {
			p.ListAppend = true
		} else {
			p.ListIndex, p.HasListIndex = uint16(v.El.Uint), true //nolint:gosec // a list index is 16 bits on the wire
		}
	}
	return p
}
