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
package reportchunk

import (
	"fmt"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// List is a list attribute value carried as the TLV encodings of its
// members, as [tlv.SplitArrayMembers] returns them. The chunker builds it
// when it scopes or splits a list; a writer handed to it writes it back
// verbatim ([Writer]).
type List [][]byte

// Member is one member of a [List], the item of a list split into appends.
type Member []byte

// Writer is write that also writes a [List] and a [Member] verbatim.
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
// its one chunk.
func Split(rd im.ReportData, budget, hardCap int, write im.AttributeValueWriter) ([]im.ReportData, error) {
	probe, err := Encode(rd, Writer(write))
	if err != nil {
		return nil, err
	}
	if len(probe) <= budget {
		return []im.ReportData{rd}, nil
	}
	c := New(rd, budget, hardCap, write, Slice(rd.Reports), func() []im.EventReport { return rd.EventReports })
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
	attributes      Source
	events          func() []im.EventReport
	eventReports    []im.EventReport
	eventsTaken     bool
	current         im.ReportData
	pending         []im.ReportData
	emitted, done   bool
}

// New chunks the reports of template's subscription (its
// HasSubscription, SubscriptionID and SuppressResponse; its report arrays
// are ignored) drawn from attributes and then events (nil: none), each
// chunk at most budget bytes when encoded with write ([Writer] wraps it),
// an entry above hardCap alone answered ResourceExhausted.
func New(template im.ReportData, budget, hardCap int, write im.AttributeValueWriter, attributes Source, events func() []im.EventReport) *Chunker {
	template.Reports, template.EventReports, template.MoreChunkedMessages = nil, nil, false
	c := &Chunker{template: template, budget: budget, hardCap: hardCap, write: Writer(write), attributes: attributes, events: events}
	c.current = c.fresh()
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
// that did not fit it — and starts the next with seed.
func (c *Chunker) closeCurrent(seed im.ReportData) {
	full := c.current
	full.MoreChunkedMessages = true
	c.pending = append(c.pending, full)
	c.current = seed
}

func (c *Chunker) addAttributeReport(rep im.AttributeReport) error {
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
	rep, err := c.capOversizedAttributeReport(rep)
	if err != nil {
		return err
	}
	current := c.current
	candidate := current
	candidate.Reports = append(candidate.Reports, rep)
	body, err := c.encode(candidate)
	if err != nil {
		return err
	}
	if len(body) > c.budget && (len(current.Reports) > 0 || len(current.EventReports) > 0) {
		seed := c.fresh()
		seed.Reports = []im.AttributeReport{rep}
		c.closeCurrent(seed)
		return nil
	}
	c.current = candidate
	return nil
}

func (c *Chunker) addEventReport(ev im.EventReport) error {
	ev, err := c.capOversizedEventReport(ev)
	if err != nil {
		return err
	}
	current := c.current
	candidate := current
	candidate.EventReports = append(candidate.EventReports, ev)
	body, err := c.encode(candidate)
	if err != nil {
		return err
	}
	if len(body) > c.budget && (len(current.Reports) > 0 || len(current.EventReports) > 0) {
		seed := c.fresh()
		seed.EventReports = []im.EventReport{ev}
		c.closeCurrent(seed)
		return nil
	}
	c.current = candidate
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
	enc := tlv.NewEncoder()
	Writer(write)(enc, tlv.AnonymousTag(), rep.Value)
	encoded, err := enc.Bytes()
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
		enc := tlv.NewEncoder()
		write(enc, tlv.AnonymousTag(), rep.Value)
		encoded, err := enc.Bytes()
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
	head := rep
	n := 1
	for n < len(members) {
		head.Value = im.AttributeValue{Value: members[:n+1]}
		if size(head) > budget {
			break
		}
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

// capOversizedAttributeReport downgrades rep to an
// AttributeStatusIB(ResourceExhausted) when its own encoded size — alone,
// with no sibling report in the chunk — already breaches the hard cap:
// such an entry can never occupy a chunk by itself.
func (c *Chunker) capOversizedAttributeReport(rep im.AttributeReport) (im.AttributeReport, error) {
	if rep.IsStatus {
		return rep, nil
	}
	alone := c.fresh()
	alone.Reports = []im.AttributeReport{rep}
	aloneBody, err := c.encode(alone)
	if err != nil {
		return im.AttributeReport{}, err
	}
	if len(aloneBody) <= c.hardCap {
		return rep, nil
	}
	return AttributeStatus(rep, im.StatusResourceExhausted), nil
}

// anyPartOversized reports a part of a split list too large for any
// message. Such an entry fails the whole attribute: one status on the
// attribute's path, never a status on a list-item path, which a reader's
// list buffering does not take (connectedhomeip's BufferedReadCallback
// passes a failed list item through, and its Python controller aborts on
// it, matter/clusters/attribute.cpp:110-116 at 6170af84 — TC-TLSCERT-2.9).
func (c *Chunker) anyPartOversized(parts []im.AttributeReport) (bool, error) {
	for _, part := range parts {
		capped, err := c.capOversizedAttributeReport(part)
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
func (c *Chunker) capOversizedEventReport(ev im.EventReport) (im.EventReport, error) {
	if ev.IsStatus {
		return ev, nil
	}
	alone := c.fresh()
	alone.EventReports = []im.EventReport{ev}
	aloneBody, err := c.encode(alone)
	if err != nil {
		return im.EventReport{}, err
	}
	if len(aloneBody) <= c.hardCap {
		return ev, nil
	}
	return im.EventReport{
		Path:     ev.Path,
		IsStatus: true,
		Status:   im.StatusIB{Status: im.StatusResourceExhausted},
	}, nil
}
