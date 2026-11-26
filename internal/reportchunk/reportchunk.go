// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package reportchunk splits a ReportData interaction into messages,
// generated one report at a time: the chunker the device side (bridge)
// and the client stack's served reads (client/interaction) share
// (review II, decision 44: "generated chunk by chunk, as matter.js";
// R2-P5-write-08).
//
// A chunker pulls attribute reports from a source and then the event
// reports, and hands out each chunk as soon as it is full, so a
// responder sends chunk N and waits for the peer's answer before chunk
// N+1 is generated. A list attribute that does not fit one chunk alone is
// split into a REPLACE-ALL followed by ListIndex=null appends; any other
// entry that does not fit one message is answered ResourceExhausted for
// its path. Mirrors matter.js
// packages/protocol/src/interaction/InteractionMessenger.ts:470-797 at
// db021da0 (sendDataReport fills a message from its payload iterator,
// pulled at :555 — the read response's generator,
// AttributeReadResponse.ts:64 — and sendAndResetReport awaits
// sendDataReportMessage, :510, before it pulls further).
//
// The chunker resolves every value once, when it admits the report: the
// attribute value or event payload is encoded alone, on an encoder of its
// own under a recover ([valueenc.Encode]), and carried on as its bytes
// ([Encoded]); every later encode — the size measurement, the chunk the
// caller sends — writes those bytes back. A value whose encoder panics,
// leaves a container open or writes other than one element answers its
// path FAILURE (an AttributeStatusIB, an EventStatusIB) and the rest of the
// report goes out, as connectedhomeip's report engine rolls the partial
// AttributeReportIB back and writes a status for the path
// (src/app/reporting/Engine.cpp:449-450, :487, :498 at 6170af84). matter.js
// has no containment per attribute; the divergence is recorded in
// notes/parity/by_design.md (#625).
package reportchunk

import (
	"fmt"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// List is a list attribute value carried as the TLV encodings of its
// members, as [tlv.SplitArrayMembers] returns them. The chunker builds it
// when it scopes or splits a list; a writer handed to it writes it back
// verbatim ([Writer]).
type List [][]byte

// Member is one member of a [List], the item of a list split into appends.
type Member []byte

// Encoded is a value the chunker has resolved: the one anonymous TLV
// element its writer wrote for it, alone and in isolation. A writer
// handed to the chunker writes it back verbatim under the tag the message
// needs ([Writer]).
type Encoded []byte

// EncodeFailure is a value the chunker could not encode: the path it
// answered FAILURE for instead, and why. IsEvent tells which path is set.
type EncodeFailure struct {
	IsEvent       bool
	AttributePath im.ConcreteAttributePath
	EventPath     im.ConcreteEventPath
	Err           error
}

// Writer is write that also writes a [List], a [Member] and an [Encoded]
// value verbatim.
func Writer(write im.AttributeValueWriter) im.AttributeValueWriter {
	return func(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		if !v.IsNull {
			switch x := v.Value.(type) {
			case List:
				enc.StartArray(tag)
				for _, m := range x {
					_ = enc.PutRawElement(tlv.AnonymousTag(), m)
				}
				_ = enc.EndContainer()
				return
			case Member:
				_ = enc.PutRawElement(tag, x)
				return
			case Encoded:
				_ = enc.PutRawElement(tag, x)
				return
			}
		}
		write(enc, tag, v)
	}
}

// Encode is rd's ReportData payload, every value written by write.
func Encode(rd im.ReportData, write im.AttributeValueWriter) ([]byte, error) {
	enc := tlv.NewEncoder()
	rd.MarshalTLV(enc, write)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("reportchunk: report data: %w", err)
	}
	return buf, nil
}

// Source yields a report's attribute reports one at a time; ok is false
// once they are exhausted.
type Source func() (im.AttributeReport, bool)

// Slice is the source over reports already built.
func Slice(reports []im.AttributeReport) Source {
	i := 0
	return func() (im.AttributeReport, bool) {
		if i >= len(reports) {
			return im.AttributeReport{}, false
		}
		i++
		return reports[i-1], true
	}
}

// Reads is the source over a read's reports, generated as they are
// pulled ([im.ReadReports]).
func Reads(r *im.ReadReports) Source {
	return r.Next
}

// Split splits rd into chunks whose encoded size stays within budget, as
// a [Chunker] over its reports does; a report that fits budget whole is
// its one chunk. Every value is resolved first, as the chunker admits it
// ([Encoded]; failed, when not nil, hears of each that answered FAILURE),
// so a report that fits is its one chunk with its values resolved.
func Split(rd im.ReportData, budget, hardCap int, write im.AttributeValueWriter, failed func(EncodeFailure)) ([]im.ReportData, error) {
	r := resolver{write: Writer(write), failed: failed}
	if len(rd.Reports) > 0 {
		reports := make([]im.AttributeReport, len(rd.Reports))
		for i, rep := range rd.Reports {
			reports[i] = r.attribute(rep)
		}
		rd.Reports = reports
	}
	if len(rd.EventReports) > 0 {
		events := make([]im.EventReport, len(rd.EventReports))
		for i, ev := range rd.EventReports {
			events[i] = r.event(ev)
		}
		rd.EventReports = events
	}
	probe, err := Encode(rd, r.write)
	if err != nil {
		return nil, err
	}
	if len(probe) <= budget {
		return []im.ReportData{rd}, nil
	}
	c := New(rd, budget, hardCap, write, Slice(rd.Reports), func() []im.EventReport { return rd.EventReports }, failed)
	chunks := make([]im.ReportData, 0, 4)
	for {
		chunk, ok, err := c.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return chunks, nil
		}
		chunks = append(chunks, chunk)
	}
}

// Chunker is the greedy fill, fed one report at a time. What it holds
// between two calls of [Chunker.Next] is the chunk being filled, the
// chunks a split list completed and not yet handed out, and the source's
// own cursor.
//
// Every chunk but the last carries MoreChunkedMessages; only the last
// carries the template's SuppressResponse (Matter §10.6.6). A report
// without any entry is one empty chunk, the template itself.
type Chunker struct {
	template        im.ReportData
	budget, hardCap int
	write           im.AttributeValueWriter
	resolve         resolver
	attributes      Source
	events          func() []im.EventReport
	eventReports    []im.EventReport
	eventsTaken     bool
	current         im.ReportData
	pending         []im.ReportData
	emitted, done   bool
	// The greedy fill keeps the chunk's encoded size as it grows instead
	// of re-encoding the chunk for every report it adds, which costs the
	// square of the reports per chunk (R2-C2-bound-0555). A report array
	// is a flat TLV container (im.ReportData.MarshalTLV writes StartArray,
	// each entry, EndContainer; an array only when it has an entry), so a
	// chunk's size is the empty chunk's, plus each array's open and close
	// once it has an entry, plus each entry's own bytes. The three
	// constants are measured with the chunker's own encoder, not assumed:
	// freshSize at construction, an array's overhead from its first entry.
	currentSize                 int
	freshSize                   int
	attrOverhead, eventOverhead int
	attrMeasured, eventMeasured bool
}

// New chunks the reports of template's subscription (its
// HasSubscription, SubscriptionID and SuppressResponse; its report arrays
// are ignored) drawn from attributes and then events (nil: none), each
// chunk at most budget bytes when encoded with write ([Writer] wraps it),
// an entry above hardCap alone answered ResourceExhausted. Every value is
// resolved when its report is admitted ([Encoded]): write runs once per
// value, and a value it cannot encode answers its path FAILURE — failed,
// when not nil, hears of each.
func New(template im.ReportData, budget, hardCap int, write im.AttributeValueWriter, attributes Source, events func() []im.EventReport, failed func(EncodeFailure)) *Chunker {
	template.Reports, template.EventReports, template.MoreChunkedMessages = nil, nil, false
	c := &Chunker{template: template, budget: budget, hardCap: hardCap, write: Writer(write), attributes: attributes, events: events}
	c.resolve = resolver{write: c.write, failed: failed}
	c.current = c.fresh()
	if body, err := c.encode(c.current); err == nil {
		c.freshSize = len(body)
	}
	c.currentSize = c.freshSize
	return c
}

// fresh is an empty intermediate chunk of the template's subscription.
func (c *Chunker) fresh() im.ReportData {
	return im.ReportData{HasSubscription: c.template.HasSubscription, SubscriptionID: c.template.SubscriptionID}
}

// encode is rd's payload by the chunker's writer.
func (c *Chunker) encode(rd im.ReportData) ([]byte, error) { return Encode(rd, c.write) }

// Next returns the next chunk; ok is false once the last went out.
func (c *Chunker) Next() (chunk im.ReportData, ok bool, err error) {
	for len(c.pending) == 0 && !c.done {
		if rep, more := c.nextAttribute(); more {
			if err := c.addAttributeReport(rep); err != nil {
				return im.ReportData{}, false, err
			}
			continue
		}
		if ev, more := c.nextEvent(); more {
			if err := c.addEventReport(ev); err != nil {
				return im.ReportData{}, false, err
			}
			continue
		}
		c.done = true
		last := c.current
		if len(last.Reports) > 0 || len(last.EventReports) > 0 || !c.emitted {
			if !c.emitted {
				// Nothing at all: the empty report still closes the
				// interaction as one ReportDataMessage.
				last.HasSubscription, last.SubscriptionID = c.template.HasSubscription, c.template.SubscriptionID
			}
			last.SuppressResponse = c.template.SuppressResponse
			c.pending = append(c.pending, last)
		}
		c.current = im.ReportData{}
	}
	if len(c.pending) == 0 {
		return im.ReportData{}, false, nil
	}
	chunk = c.pending[0]
	c.pending = c.pending[1:]
	if len(c.pending) == 0 {
		c.pending = nil
	}
	c.emitted = true
	return chunk, true, nil
}

func (c *Chunker) nextAttribute() (im.AttributeReport, bool) {
	if c.attributes == nil {
		return im.AttributeReport{}, false
	}
	rep, ok := c.attributes()
	if !ok {
		c.attributes = nil
	}
	return rep, ok
}

func (c *Chunker) nextEvent() (im.EventReport, bool) {
	if !c.eventsTaken {
		c.eventsTaken = true
		if c.events != nil {
			c.eventReports = c.events()
		}
	}
	if len(c.eventReports) == 0 {
		c.eventReports = nil
		return im.EventReport{}, false
	}
	ev := c.eventReports[0]
	c.eventReports = c.eventReports[1:]
	return ev, true
}

// closeCurrent hands out the chunk being filled — more follows: the entry
// that did not fit it — and starts the next with seed, whose encoded size
// is seedSize.
func (c *Chunker) closeCurrent(seed im.ReportData, seedSize int) {
	full := c.current
	full.MoreChunkedMessages = true
	c.pending = append(c.pending, full)
	c.current = seed
	c.currentSize = seedSize
}

// measureOverhead is the bytes a report array adds to a chunk beyond its
// entries — its open and its close — measured from alone, a chunk of the
// one entry e of encoded size aloneSize, and the same chunk with e twice:
// the second copy adds e's own bytes alone.
func (c *Chunker) measureOverhead(aloneSize int, twice im.ReportData) (int, error) {
	body, err := c.encode(twice)
	if err != nil {
		return 0, err
	}
	entry := len(body) - aloneSize
	return aloneSize - c.freshSize - entry, nil
}

func (c *Chunker) addAttributeReport(rep im.AttributeReport) error {
	rep = c.resolve.attribute(rep)
	rep = ApplyFabricScope(rep, c.write)
	if parts, ok := SplitList(c.template, rep, c.budget, c.write); ok {
		if oversized, err := c.anyPartOversized(parts); err != nil {
			return err
		} else if oversized {
			return c.addAttributeReport(AttributeStatus(rep, im.StatusResourceExhausted))
		}
		for _, part := range parts {
			if err := c.addAttributeReport(part); err != nil {
				return err
			}
		}
		return nil
	}
	rep, aloneSize, err := c.capOversizedAttributeReport(rep)
	if err != nil {
		return err
	}
	if !c.attrMeasured {
		twice := c.fresh()
		twice.Reports = []im.AttributeReport{rep, rep}
		if c.attrOverhead, err = c.measureOverhead(aloneSize, twice); err != nil {
			return err
		}
		c.attrMeasured = true
	}
	// What rep adds to the chunk: its own bytes, and the array's open and
	// close when it is the array's first entry.
	added := aloneSize - c.freshSize
	if len(c.current.Reports) > 0 {
		added -= c.attrOverhead
	}
	current := c.current
	if c.currentSize+added > c.budget && (len(current.Reports) > 0 || len(current.EventReports) > 0) {
		seed := c.fresh()
		seed.Reports = []im.AttributeReport{rep}
		c.closeCurrent(seed, aloneSize)
		return nil
	}
	c.current.Reports = append(c.current.Reports, rep)
	c.currentSize += added
	return nil
}

func (c *Chunker) addEventReport(ev im.EventReport) error {
	ev, aloneSize, err := c.capOversizedEventReport(c.resolve.event(ev))
	if err != nil {
		return err
	}
	if !c.eventMeasured {
		twice := c.fresh()
		twice.EventReports = []im.EventReport{ev, ev}
		if c.eventOverhead, err = c.measureOverhead(aloneSize, twice); err != nil {
			return err
		}
		c.eventMeasured = true
	}
	added := aloneSize - c.freshSize
	if len(c.current.EventReports) > 0 {
		added -= c.eventOverhead
	}
	current := c.current
	if c.currentSize+added > c.budget && (len(current.Reports) > 0 || len(current.EventReports) > 0) {
		seed := c.fresh()
		seed.EventReports = []im.EventReport{ev}
		c.closeCurrent(seed, aloneSize)
		return nil
	}
	c.current.EventReports = append(c.current.EventReports, ev)
	c.currentSize += added
	return nil
}

// ApplyFabricScope applies rep's fabric scope to its value before the
// report is chunked, so a list split across messages is split into the
// entries the reader receives: the value becomes the scoped list's encoded
// members ([List]) and the scope is cleared; a value that cannot be scoped
// answers the attribute's path FAILURE ([im.MaskFabricScoped]). A report
// without a scope, and a status, is returned as it is. write encodes the
// value. [im.AttributeReport]'s encoder applies a scope still set the same
// way.
func ApplyFabricScope(rep im.AttributeReport, write im.AttributeValueWriter) im.AttributeReport {
	if rep.FabricScope == nil || rep.IsStatus {
		return rep
	}
	scope := *rep.FabricScope
	rep.FabricScope = nil
	encoded, err := encodeValue(Writer(write), rep.Value)
	if err != nil {
		return AttributeStatus(rep, im.StatusFailure)
	}
	masked, ok := im.MaskFabricScoped(encoded, scope)
	if !ok {
		return AttributeStatus(rep, im.StatusFailure)
	}
	if len(masked) == 1 && tlv.ElementType(masked[0]) == tlv.TypeNull {
		rep.Value = im.AttributeValue{IsNull: true}
		return rep
	}
	members, err := tlv.SplitArrayMembers(masked)
	if err != nil {
		return AttributeStatus(rep, im.StatusFailure)
	}
	rep.Value = im.AttributeValue{Value: List(members)}
	return rep
}

// SplitList splits a list attribute whose report alone does not fit
// budget into a REPLACE-ALL report carrying as many leading members as
// fit, followed by one ListIndex=null append report per remaining member,
// all at the attribute's DataVersion. Mirrors matter.js
// InteractionMessenger.sendDataReport, which chunks such an attribute with
// chunkAttributePayload (packages/protocol/src/interaction/
// AttributeDataEncoder.ts) and packs as many members into the initial
// REPLACE-ALL as the message holds; chip's ReportDataEncoder does the
// same (src/app/reporting/Engine.cpp, the AttributeValueEncoder list
// chunking). Without it a list larger than one datagram — the NOCs of
// three fabrics, a long ACL — rides in a single oversized message that a
// chip controller cannot authenticate and discards.
//
// ok is false when rep is no splittable list: a status, an entry that
// already addresses a list item, a non-list value, an empty list, or one
// that fits budget alone. rd is the report's subscription; write encodes
// the value.
func SplitList(rd im.ReportData, rep im.AttributeReport, budget int, write im.AttributeValueWriter) ([]im.AttributeReport, bool) {
	if rep.IsStatus || rep.Path.ListAppend || rep.Path.HasListIndex || rep.Value.IsNull || rep.Value.Value == nil {
		return nil, false
	}
	switch v := rep.Value.Value.(type) {
	case List:
		// A list already carried as its members — a fabric-scoped value
		// ([ApplyFabricScope]) or a split's head — splits again only while
		// it holds more than one: a head of one member is final.
		if len(v) < 2 {
			return nil, false
		}
	case Encoded:
		// A resolved value splits only when it is a list.
		if len(v) == 0 || (tlv.ElementType(v[0]) != tlv.TypeArray && tlv.ElementType(v[0]) != tlv.TypeList) {
			return nil, false
		}
	case Member, []byte, string:
		return nil, false
	}
	write = Writer(write)
	size := func(r im.AttributeReport) int {
		body, err := Encode(im.ReportData{HasSubscription: rd.HasSubscription, SubscriptionID: rd.SubscriptionID, Reports: []im.AttributeReport{r}}, write)
		if err != nil {
			return budget + 1
		}
		return len(body)
	}
	if size(rep) <= budget {
		return nil, false
	}
	members, ok := rep.Value.Value.(List)
	if !ok {
		encoded, err := encodeValue(write, rep.Value)
		if err != nil {
			return nil, false
		}
		if members, err = tlv.SplitArrayMembers(encoded); err != nil {
			return nil, false
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	// The head grows by one member's bytes at a time: a [List] is written
	// as StartArray, each member by PutRawElement with an anonymous tag,
	// EndContainer ([Writer]), so a member adds what that one call writes —
	// measured by the same call — and the head is encoded once, not once
	// per member (R2-C2-bound-0555).
	head := rep
	head.Value = im.AttributeValue{Value: members[:1]}
	headSize := size(head)
	n := 1
	for n < len(members) {
		grown := headSize + memberSize(write, members[n])
		if grown > budget {
			break
		}
		headSize = grown
		n++
	}
	head.Value = im.AttributeValue{Value: members[:n]}
	out := make([]im.AttributeReport, 0, 1+len(members)-n)
	out = append(out, head)
	for _, m := range members[n:] {
		item := rep
		item.Path.ListAppend = true
		item.Value = im.AttributeValue{Value: Member(m)}
		out = append(out, item)
	}
	return out, true
}

// memberSize is the bytes write adds for m as one member of a [List].
func memberSize(write im.AttributeValueWriter, m []byte) int {
	enc := tlv.NewEncoder()
	write(enc, tlv.AnonymousTag(), im.AttributeValue{Value: Member(m)})
	b, err := enc.Bytes()
	if err != nil {
		return len(m)
	}
	return len(b)
}

// capOversizedAttributeReport downgrades rep to an
// AttributeStatusIB(ResourceExhausted) when its own encoded size — alone,
// with no sibling report in the chunk — already breaches the hard cap:
// such an entry can never occupy a chunk by itself. size is the encoded
// size of a chunk holding the returned report alone.
func (c *Chunker) capOversizedAttributeReport(rep im.AttributeReport) (out im.AttributeReport, size int, err error) {
	alone := c.fresh()
	alone.Reports = []im.AttributeReport{rep}
	aloneBody, err := c.encode(alone)
	if err != nil {
		return im.AttributeReport{}, 0, err
	}
	if rep.IsStatus || len(aloneBody) <= c.hardCap {
		return rep, len(aloneBody), nil
	}
	return c.capOversizedAttributeReport(AttributeStatus(rep, im.StatusResourceExhausted))
}

// anyPartOversized reports a part of a split list too large for any
// message. Such an entry fails the whole attribute: one status on the
// attribute's path, never a status on a list-item path, which a reader's
// list buffering does not take (connectedhomeip's BufferedReadCallback
// passes a failed list item through, and its Python controller aborts on
// it, matter/clusters/attribute.cpp:110-116 at 6170af84 — TC-TLSCERT-2.9).
func (c *Chunker) anyPartOversized(parts []im.AttributeReport) (bool, error) {
	for _, part := range parts {
		capped, _, err := c.capOversizedAttributeReport(part)
		if err != nil {
			return false, err
		}
		if capped.IsStatus {
			return true, nil
		}
	}
	return false, nil
}

// AttributeStatus is rep's attribute answered with status: the path
// without a list index, which a status never carries here.
func AttributeStatus(rep im.AttributeReport, status im.StatusCode) im.AttributeReport {
	path := rep.Path
	path.ListAppend, path.HasListIndex, path.ListIndex = false, false, 0
	return im.AttributeReport{Path: path, IsStatus: true, Status: im.StatusIB{Status: status}}
}

// capOversizedEventReport is [Chunker.capOversizedAttributeReport]'s twin
// for an EventReport.
func (c *Chunker) capOversizedEventReport(ev im.EventReport) (out im.EventReport, size int, err error) {
	alone := c.fresh()
	alone.EventReports = []im.EventReport{ev}
	aloneBody, err := c.encode(alone)
	if err != nil {
		return im.EventReport{}, 0, err
	}
	if ev.IsStatus || len(aloneBody) <= c.hardCap {
		return ev, len(aloneBody), nil
	}
	return c.capOversizedEventReport(im.EventReport{
		Path:     ev.Path,
		IsStatus: true,
		Status:   im.StatusIB{Status: im.StatusResourceExhausted},
	})
}

// resolver resolves a report's value once ([Encoded]), or answers its path
// FAILURE when the value cannot be encoded.
type resolver struct {
	write  im.AttributeValueWriter
	failed func(EncodeFailure)
}

// attribute is rep with its value resolved, or the AttributeStatusIB
// FAILURE of its path. A status, and a value already carried as bytes,
// is returned as it is.
func (r resolver) attribute(rep im.AttributeReport) im.AttributeReport {
	if rep.IsStatus || resolved(rep.Value) {
		return rep
	}
	el, err := encodeValue(r.write, rep.Value)
	if err != nil {
		if r.failed != nil {
			r.failed(EncodeFailure{AttributePath: rep.Path, Err: err})
		}
		return AttributeStatus(rep, im.StatusFailure)
	}
	rep.Value = im.AttributeValue{Value: Encoded(el)}
	return rep
}

// event is ev with its payload resolved, or the EventStatusIB FAILURE of
// its path. A status, an event without a payload and a payload already
// carried as bytes are returned as they are.
func (r resolver) event(ev im.EventReport) im.EventReport {
	if ev.IsStatus || ev.Data.IsNull || ev.Data.Value == nil || resolved(ev.Data) {
		return ev
	}
	el, err := encodeValue(r.write, ev.Data)
	if err != nil {
		if r.failed != nil {
			r.failed(EncodeFailure{IsEvent: true, EventPath: ev.Path, Err: err})
		}
		return im.EventReport{Path: ev.Path, IsStatus: true, Status: im.StatusIB{Status: im.StatusFailure}}
	}
	ev.Data = im.AttributeValue{Value: Encoded(el)}
	return ev
}

// resolved reports a value already carried as its encoding.
func resolved(v im.AttributeValue) bool {
	if v.IsNull {
		return false
	}
	switch v.Value.(type) {
	case Encoded, List, Member:
		return true
	}
	return false
}

// encodeValue is v as write writes it, in isolation ([valueenc.Encode]).
func encodeValue(write im.AttributeValueWriter, v im.AttributeValue) ([]byte, error) {
	return valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { write(enc, tag, v) })
}
