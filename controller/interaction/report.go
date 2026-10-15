// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// AttributeData is one attribute value of a report.
type AttributeData struct {
	// Path is the attribute's concrete path (node, endpoint, cluster,
	// attribute; no list index).
	Path im.ConcreteAttributePath
	// DataVersion is the cluster's data version, 0 when the report carried
	// none (InputChunk.ts:309-310).
	DataVersion uint32
	// Value is the value as a node tree; a list reassembled from its chunks.
	Value spec.Node
}

// AttributeStatus is one attribute status of a report.
type AttributeStatus struct {
	Path          im.ConcreteAttributePath
	Status        im.StatusCode
	ClusterStatus *uint8
}

// EventData is one event of a report — matter.js ReadResult.EventValue as
// InputChunk.ts:356-413 builds it.
type EventData struct {
	Path     im.ConcreteEventPath
	Number   uint64
	Priority im.EventPriority
	// Timestamp is the lossy convenience of InputChunk.ts:388-389: the
	// epoch timestamp, else the system one, else the delta epoch one, else
	// the delta system one, else 0.
	Timestamp uint64
	// The four timestamps as the event carried them, nil when absent.
	EpochTimestamp       *uint64
	SystemTimestamp      *uint64
	DeltaEpochTimestamp  *uint64
	DeltaSystemTimestamp *uint64
	// Value is the event's data, HasValue false when it carried none.
	Value    spec.Node
	HasValue bool
}

// EventStatus is one event status of a report.
type EventStatus struct {
	Path          im.ConcreteEventPath
	Status        im.StatusCode
	ClusterStatus *uint8
}

// Report is the data of a read or of one subscription report, its chunks
// merged.
type Report struct {
	// SubscriptionID is the report's subscription id, 0 for a read's
	// report: the first chunk's — matter.js hands each chunk's DataReport,
	// subscriptionId included, to InputChunk (action/client/InputChunk.ts:166-169)
	// and the handler reads the id off the initial report
	// (ClientSubscriptionHandler.ts:77) at 0ceb202d.
	SubscriptionID uint32
	// Session is the session the report arrived over — the exchange's
	// session matter.js takes the peer from and hands to reportStarted
	// (ClientSubscriptionHandler.ts:89, :108-112).
	Session       *exchange.Session
	Attributes    []AttributeData
	Statuses      []AttributeStatus
	Events        []EventData
	EventStatuses []EventStatus
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

// Empty reports whether the report carries no attribute or event entry.
func (r *Report) Empty() bool {
	return len(r.Attributes) == 0 && len(r.Statuses) == 0 && len(r.Events) == 0 && len(r.EventStatuses) == 0
}

// errMalformedEntry is an entry InputChunk skips (UnexpectedDataError,
// InputChunk.ts:227-230, :312-320, :349-352).
var errMalformedEntry = errors.New("interaction: malformed report entry")

// resolvedPath is InputChunk's ResolvedAttributePath (InputChunk.ts:144-150).
type resolvedPath struct {
	path       im.ConcreteAttributePath
	version    uint32
	hasVersion bool
}

// assembler is InputChunk (action/client/InputChunk.ts:166-432 at
// 0ceb202d) with the leftover list one read or report keeps across its
// chunks (ClientInteraction.ts:279-285, :1288-1294;
// ClientSubscriptionHandler.ts:168-195).
type assembler struct {
	report   *Report
	leftover []im.AttributeReport
	log      *slog.Logger
	chunks   int
}

func newAssembler(log *slog.Logger) *assembler {
	return &assembler{report: &Report{}, log: log}
}

// add takes one DataReport chunk — InputChunk (:166-172): the attributes,
// then the events.
func (a *assembler) add(rd *im.ReportData) {
	if a.chunks == 0 && rd.HasSubscription {
		a.report.SubscriptionID = rd.SubscriptionID
	}
	a.chunks++
	a.emitAttributes(rd)
	a.emitEvents(rd)
}

// group is AttributeDataGroup (:152-156).
type group struct {
	path       resolvedPath
	hasVersion bool
	version    uint32
	entries    []*im.AttributeReport
}

// emitAttributes is InputChunk.ts:174-245: the leftover of the previous
// chunk first; adjacent data entries of one path form a group; tag
// compression resolves against the last complete path; a malformed entry
// is skipped; a group at the end of a chunk with more to come that may
// continue is kept for the next chunk.
func (a *assembler) emitAttributes(rd *im.ReportData) {
	attrs := make([]im.AttributeReport, 0, len(a.leftover)+len(rd.Reports))
	attrs = append(attrs, a.leftover...)
	attrs = append(attrs, rd.Reports...)
	a.leftover = nil
	if len(attrs) == 0 {
		return
	}
	var last *resolvedPath
	var g *group
	for i := range attrs {
		entry := &attrs[i]
		if !entry.IsStatus {
			resolved, err := resolvePath(entry, last)
			if err != nil {
				a.log.Warn("matter.controller.interaction.malformed_attribute_entry", slog.String("err", err.Error()))
				continue
			}
			// :196-201: a compressed entry without a version takes the
			// last path's.
			if entry.TagCompression && !entry.HasDataVersion && last != nil && last.hasVersion {
				entry.DataVersion, entry.HasDataVersion = last.version, true
			}
			if !entry.TagCompression {
				last = &resolvedPath{path: resolved.path, version: entry.DataVersion, hasVersion: entry.HasDataVersion}
			}
			if g != nil && !samePath(g.path.path, resolved.path) {
				a.flush(g)
				g = nil
			}
			if g == nil {
				g = &group{path: resolved, hasVersion: entry.HasDataVersion, version: entry.DataVersion}
			} else if !g.hasVersion {
				g.version, g.hasVersion = entry.DataVersion, entry.HasDataVersion
			}
			g.entries = append(g.entries, entry)
			continue
		}
		resolved, err := resolvePath(entry, last)
		if err != nil {
			a.log.Warn("matter.controller.interaction.malformed_attribute_entry", slog.String("err", err.Error()))
			continue
		}
		if !entry.TagCompression {
			next := resolvedPath{path: resolved.path}
			if last != nil {
				next.version, next.hasVersion = last.version, last.hasVersion
			}
			last = &next
		}
		if g != nil {
			a.flush(g)
			g = nil
		}
		s := AttributeStatus{Path: resolved.path, Status: entry.Status.Status}
		if entry.Status.HasClusterStatus {
			cs := entry.Status.ClusterStatus
			s.ClusterStatus = &cs
		}
		a.report.Statuses = append(a.report.Statuses, s)
	}
	// :233-241: a group that may continue waits for the next chunk.
	if g != nil && rd.MoreChunkedMessages && mayContinueInNextReport(g.entries[len(g.entries)-1]) {
		for _, e := range g.entries {
			a.leftover = append(a.leftover, *e)
		}
		return
	}
	if g != nil {
		a.flush(g)
	}
}

// resolvePath is InputChunk.ts:252-273: a compressed path takes the
// missing fields of the last one (an error without one); an uncompressed
// path must name endpoint, cluster and attribute. The entry's path is
// completed in place, as matter.js mutates it.
func resolvePath(entry *im.AttributeReport, last *resolvedPath) (resolvedPath, error) {
	p := &entry.Path
	if entry.TagCompression {
		if last == nil {
			return resolvedPath{}, fmt.Errorf("%w: tag compression enabled, but no previous path", errMalformedEntry)
		}
		if !p.HasNode && last.path.HasNode {
			p.Node, p.HasNode = last.path.Node, true
		}
		if !p.HasEndpoint {
			p.Endpoint, p.HasEndpoint = last.path.Endpoint, true
		}
		if !p.HasCluster {
			p.Cluster, p.HasCluster = last.path.Cluster, true
		}
		if !p.HasAttribute {
			p.Attribute, p.HasAttribute = last.path.Attribute, true
		}
	} else if !p.HasEndpoint || !p.HasCluster || !p.HasAttribute {
		return resolvedPath{}, fmt.Errorf("%w: tag compression disabled, but path is incomplete", errMalformedEntry)
	}
	out := im.ConcreteAttributePath{
		Node: p.Node, HasNode: p.HasNode,
		Endpoint: p.Endpoint, Cluster: p.Cluster, Attribute: p.Attribute,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	return resolvedPath{path: out}, nil
}

// samePath is InputChunk.ts:275-282.
func samePath(a, b im.ConcreteAttributePath) bool {
	return a.HasNode == b.HasNode && a.Node == b.Node && a.Endpoint == b.Endpoint && a.Cluster == b.Cluster && a.Attribute == b.Attribute
}

// mayContinueInNextReport is InputChunk.ts:284-289: a list action (a list
// index) or a value that is an array.
func mayContinueInNextReport(entry *im.AttributeReport) bool {
	if entry.Path.HasListIndex {
		return true
	}
	n, ok := entry.Value.Value.(spec.Node)
	return ok && n.El.Type == tlv.TypeArray
}

// flush is flushDataGroup / decodeAttributeGroup (InputChunk.ts:247-250,
// :291-321) without a schema — decodeUnknownAttributeValue
// (interaction/AttributeDataDecoder.ts:347-365): one entry without a list
// index is the value; otherwise the chunks build an array
// (ArraySchema.decodeFromChunkedArray, types/src/tlv/TlvArray.ts:130-154).
// A group that cannot be decoded is skipped.
func (a *assembler) flush(g *group) {
	value, err := decodeGroup(g.entries)
	if err != nil {
		a.log.Warn("matter.controller.interaction.malformed_attribute",
			slog.Int("endpoint", int(g.path.path.Endpoint)), slog.Uint64("cluster", uint64(g.path.path.Cluster)),
			slog.Uint64("attribute", uint64(g.path.path.Attribute)), slog.String("err", err.Error()))
		return
	}
	a.report.Attributes = append(a.report.Attributes, AttributeData{Path: g.path.path, DataVersion: g.version, Value: value})
}

func decodeGroup(entries []*im.AttributeReport) (spec.Node, error) {
	nodeOf := func(e *im.AttributeReport) spec.Node {
		n, _ := e.Value.Value.(spec.Node)
		n.El.Tag = tlv.AnonymousTag()
		return n
	}
	if len(entries) == 1 && !entries[0].Path.HasListIndex {
		return nodeOf(entries[0]), nil
	}
	if entries[0].Path.HasListIndex {
		return spec.Node{}, fmt.Errorf("%w: the first chunk of a list needs no list index", errMalformedEntry)
	}
	var list spec.Node
	for _, e := range entries {
		n := nodeOf(e)
		switch {
		case !e.Path.HasListIndex:
			// No list index replaces the whole list.
			if n.El.Type != tlv.TypeArray {
				return spec.Node{}, fmt.Errorf("%w: a list chunk that is not an array", errMalformedEntry)
			}
			list = n
		case e.Path.ListAppend:
			list.Children = append(list.Children, n)
		case int(e.Path.ListIndex) >= len(list.Children):
			return spec.Node{}, fmt.Errorf("%w: list index %d past the list", errMalformedEntry, e.Path.ListIndex)
		case n.El.Type == tlv.TypeNull || n.El.IsNull:
			// A null element removes the entry at the index.
			list.Children = append(list.Children[:e.Path.ListIndex], list.Children[e.Path.ListIndex+1:]...)
		default:
			list.Children[e.Path.ListIndex] = n
		}
	}
	return list, nil
}

// emitEvents is InputChunk.ts:336-354 with decodeEventValue (:356-413)
// and buildEventStatus (:415-432): an entry without a complete path is
// skipped.
func (a *assembler) emitEvents(rd *im.ReportData) {
	for i := range rd.EventReports {
		ev := &rd.EventReports[i]
		if !ev.Path.HasEndpoint || !ev.Path.HasCluster || !ev.Path.HasEvent {
			a.log.Warn("matter.controller.interaction.malformed_event_entry",
				slog.Int("endpoint", int(ev.Path.Endpoint)), slog.Uint64("cluster", uint64(ev.Path.Cluster)), slog.Uint64("event", uint64(ev.Path.Event)))
			continue
		}
		if ev.IsStatus {
			s := EventStatus{Path: ev.Path, Status: ev.Status.Status}
			if ev.Status.HasClusterStatus {
				cs := ev.Status.ClusterStatus
				s.ClusterStatus = &cs
			}
			a.report.EventStatuses = append(a.report.EventStatuses, s)
			continue
		}
		d := EventData{Path: ev.Path, Number: ev.Number, Priority: ev.Priority}
		if r := ev.Received; r != nil {
			if r.HasEpochTimestamp {
				d.EpochTimestamp = ptr(ev.Timestamp)
			}
			if r.HasSystemTimestamp {
				d.SystemTimestamp = ptr(r.SystemTimestamp)
			}
			if r.HasDeltaEpochTimestamp {
				d.DeltaEpochTimestamp = ptr(r.DeltaEpochTimestamp)
			}
			if r.HasDeltaSystemTimestamp {
				d.DeltaSystemTimestamp = ptr(r.DeltaSystemTimestamp)
			}
			if r.HasData {
				d.Value, d.HasValue = ev.Data.Value.(spec.Node)
			}
		}
		for _, ts := range []*uint64{d.EpochTimestamp, d.SystemTimestamp, d.DeltaEpochTimestamp, d.DeltaSystemTimestamp} {
			if ts != nil {
				d.Timestamp = *ts
				break
			}
		}
		a.report.Events = append(a.report.Events, d)
	}
}

func ptr[T any](v T) *T { return &v }

// readAttributeValue keeps a report's value as a node tree.
func readAttributeValue(_ im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	return readNode(el, dec)
}

// readEventValue keeps an event's data as a node tree.
func readEventValue(_ im.ConcreteEventPath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	return readNode(el, dec)
}

func readNode(el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if el.IsContainer {
		n, err := spec.ReadContainer(dec, el)
		return im.AttributeValue{Value: n}, err
	}
	return im.AttributeValue{Value: spec.Node{El: el}}, nil
}

// decodeReport decodes one ReportData message.
func decodeReport(payload []byte) (*im.ReportData, error) {
	rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(payload), readAttributeValue, readEventValue)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnexpectedMessage, err)
	}
	return &rd, nil
}

// errStopReports ends readDataReports without an error once the consumer
// answered the chunk itself.
var errStopReports = errors.New("interaction: reports stopped")

// readDataReports reads the DataReport chunks of an exchange as matter.js
// IncomingInteractionClientMessenger.readDataReports does
// (interaction/InteractionMessenger.ts:938-972 at 0ceb202d): the next
// message must be a ReportData (a StatusResponse error in its place is a
// *StatusError, :914-931); the consumer takes the chunk first; then a
// chunk with more to come is answered with a StatusResponse Success and
// the next one awaited, the last one with a Success unless it suppresses
// the response — sent before the exchange closes, a failure only logged
// (the tracked end message, :950-966). first, when set, is the chunk
// already received. A consumer returning errStopReports ends the reading
// with nil and no answer.
func readDataReports(ctx context.Context, log *slog.Logger, m *messenger, first *im.ReportData, consume func(rd *im.ReportData) error) error {
	rd := first
	for {
		if rd == nil {
			msg, err := m.nextReport(ctx)
			if err != nil {
				return err
			}
			if rd, err = decodeReport(msg.Payload); err != nil {
				return err
			}
		}
		if err := consume(rd); err != nil {
			if errors.Is(err, errStopReports) {
				return nil
			}
			return err
		}
		if rd.MoreChunkedMessages {
			if err := m.sendStatus(ctx, im.StatusSuccess); err != nil {
				return err
			}
			rd = nil
			continue
		}
		if !rd.SuppressResponse {
			if err := m.sendStatus(context.WithoutCancel(ctx), im.StatusSuccess); err != nil {
				log.Info("matter.controller.interaction.final_status_failed", slog.String("err", err.Error()))
			}
		}
		return nil
	}
}

// nextReport is IncomingInteractionClientMessenger.waitFor("DataReport")
// (InteractionMessenger.ts:914-931): a StatusResponse in place of the
// report is a *StatusError, any other message an ErrUnexpectedMessage.
func (m *messenger) nextReport(ctx context.Context) (*exchange.Message, error) {
	return m.next(ctx, im.OpcodeReportData, 0)
}
