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
	m, err := c.begin(ctx, ExchangeOptions{})
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: paths, FabricFiltered: true}.MarshalTLV(enc)
	req, err := enc.Bytes()
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

// merge decodes one ReportData chunk into r — im.UnmarshalReportDataTLV
// with the values as node trees — and returns its MoreChunkedMessages and
// SuppressResponse flags.
func (r *Report) merge(payload []byte) (more, suppress bool, err error) {
	rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(payload), readAttributeValue, nil)
	if err != nil {
		return false, false, fmt.Errorf("%w: %w", ErrUnexpectedMessage, err)
	}
	for i := range rd.Reports {
		rep := &rd.Reports[i]
		if !rep.IsStatus {
			r.addData(rep)
			continue
		}
		s := AttributeStatus{Path: rep.Path, Status: rep.Status.Status}
		if rep.Status.HasClusterStatus {
			v := rep.Status.ClusterStatus
			s.ClusterStatus = &v
		}
		r.Statuses = append(r.Statuses, s)
	}
	return rd.MoreChunkedMessages, rd.SuppressResponse, nil
}

// readAttributeValue keeps a report's value as a node tree.
func readAttributeValue(_ im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if el.IsContainer {
		n, err := spec.ReadContainer(dec, el)
		return im.AttributeValue{Value: n}, err
	}
	return im.AttributeValue{Value: spec.Node{El: el}}, nil
}

// addData adds one AttributeDataIB: a null ListIndex appends its value to
// the list the same path carried last.
func (r *Report) addData(rep *im.AttributeReport) {
	path := rep.Path
	value, _ := rep.Value.Value.(spec.Node)
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
	r.Attributes = append(r.Attributes, AttributeData{Path: path, DataVersion: rep.DataVersion, Value: value})
}
