// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The chunking of a write into WriteRequest messages — matter.js
// InteractionClientMessenger.#chunkWriteRequest and sendWriteCommand
// (packages/protocol/src/interaction/InteractionMessenger.ts:1269-1450 at
// 862ad75f, Matter 1.6.1 § 10.6.4.3.1, § 10.7.6.2).

// writeValue encodes an AttributeDataIB's Data.
func writeValue(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
	if value, ok := v.Value.(spec.Encodable); ok {
		value.EncodeTLV(e, tag)
	}
}

// encodeWrite encodes req.
func encodeWrite(req im.WriteRequest) ([]byte, error) {
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc, writeValue)
	return enc.Bytes()
}

// writeSize is the size of req's encoding.
func writeSize(req im.WriteRequest) int {
	b, err := encodeWrite(req)
	if err != nil {
		return 0
	}
	return len(b)
}

// attributeDataSize is the size of w as one AttributeDataIB of a
// WriteRequest (TlvAttributeData.encode).
func attributeDataSize(w im.AttributeWrite) int {
	return writeSize(im.WriteRequest{Writes: []im.AttributeWrite{w}}) - writeSize(im.WriteRequest{})
}

// responseSize is the size of a WriteResponse of statuses.
func responseSize(statuses ...im.AttributeStatus) int {
	enc := tlv.NewEncoder()
	im.WriteResponse{Responses: statuses}.MarshalTLV(enc)
	b, _ := enc.Bytes()
	return len(b)
}

// maxStatusSizeOf is the largest AttributeStatusIB a peer answers path
// with (InteractionMessenger.ts:102-106).
func maxStatusSizeOf(path im.ConcreteAttributePath) int {
	worst := im.AttributeStatus{Path: path, Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: uint8(im.StatusFailure), HasClusterStatus: true}}
	return responseSize(worst) - responseSize()
}

// isVersionedListAdd is an ADD (null ListIndex) with a DataVersion
// (:108-110).
func isVersionedListAdd(w im.AttributeWrite) bool { return w.Path.ListAppend && w.HasDataVersion }

func sameAttribute(a, b im.ConcreteAttributePath) bool {
	return a.Endpoint == b.Endpoint && a.Cluster == b.Cluster && a.Attribute == b.Attribute
}

// chunkWrite splits req into WriteRequest messages that each fit
// maxPayload, and whose WriteResponse — every status at its largest —
// fits it too (#chunkWriteRequest, :1320-1435): the AttributeDataIBs keep
// their order, so a chunked list's ADDs follow its REPLACE; a whole-list IB
// too large for one message goes as a REPLACE and an ADD per remaining
// entry, the REPLACE of the ACL or its Extension carrying as many leading
// entries as fit (the ACL is never emptied in between) and any other
// empty; a versioned REPLACE ends its chunk. A write that fits one
// message goes as one unless it holds a versioned ADD; a timed or
// suppressed one that does not fit is [ErrRequestTooLarge], as is one
// value that fits no message.
func chunkWrite(req im.WriteRequest, maxPayload int) ([]im.WriteRequest, error) {
	if one, err := fitsOneWrite(req, maxPayload); one || err != nil {
		if err != nil {
			return nil, err
		}
		return []im.WriteRequest{req}, nil
	}
	c := newWriteChunker(req, maxPayload)
	for _, w := range req.Writes {
		if err := c.write(w); err != nil {
			return nil, err
		}
	}
	return c.done(), nil
}

// fitsOneWrite reports whether req goes as one message (:1336-1350): it
// fits, its statuses fit one WriteResponse, and it holds no versioned ADD
// — a timed or suppressed write that fits goes as one either way, one that
// does not is [ErrRequestTooLarge].
func fitsOneWrite(req im.WriteRequest, maxPayload int) (bool, error) {
	encodedSize := writeSize(req)
	statusesSize := 0
	if !req.SuppressResponse {
		for _, w := range req.Writes {
			statusesSize += maxStatusSizeOf(w.Path)
		}
	}
	if encodedSize <= maxPayload && (req.SuppressResponse || statusesSize <= maxPayload-responseSize()) {
		if req.TimedRequest || req.SuppressResponse {
			return true, nil
		}
		for _, w := range req.Writes {
			if isVersionedListAdd(w) {
				return false, nil
			}
		}
		return true, nil
	}
	if req.TimedRequest || req.SuppressResponse {
		kind := "timed"
		if !req.TimedRequest {
			kind = "sent with suppressResponse"
		}
		return false, fmt.Errorf("%w: a write of %d bytes with statuses of up to %d bytes exceeds the maximum message size of %d bytes and cannot be chunked because it is %s",
			ErrRequestTooLarge, encodedSize, statusesSize, maxPayload, kind)
	}
	return false, nil
}

// writeChunker gathers AttributeDataIBs into chunks (the flush and add of
// #chunkWriteRequest, :1357-1395).
type writeChunker struct {
	header                im.WriteRequest
	maxPayload, available int
	availableForStatuses  int
	chunks                []im.WriteRequest
	current               []im.AttributeWrite
	used, usedForStatuses int
}

func newWriteChunker(req im.WriteRequest, maxPayload int) *writeChunker {
	header := req
	header.Writes, header.MoreChunkedMessages = nil, true
	return &writeChunker{header: header, maxPayload: maxPayload, available: maxPayload - writeSize(header), availableForStatuses: maxPayload - responseSize()}
}

func (c *writeChunker) flush() {
	if len(c.current) > 0 {
		chunk := c.header
		chunk.Writes = c.current
		c.chunks = append(c.chunks, chunk)
		c.current, c.used, c.usedForStatuses = nil, 0, 0
	}
}

// add appends w, flushing first when it does not fit the chunk, or when
// it is a versioned ADD right after its list's REPLACE.
func (c *writeChunker) add(w im.AttributeWrite) error {
	size, statusSize := attributeDataSize(w), maxStatusSizeOf(w.Path)
	afterReplace := false
	if n := len(c.current); n > 0 {
		prev := c.current[n-1].Path
		afterReplace = !prev.HasListIndex && !prev.ListAppend && sameAttribute(prev, w.Path)
	}
	if c.used+size > c.available || c.usedForStatuses+statusSize > c.availableForStatuses || (isVersionedListAdd(w) && afterReplace) {
		c.flush()
	}
	if size > c.available {
		return fmt.Errorf("%w: write data for endpoint %d cluster 0x%04X attribute 0x%04X has %d bytes and does not fit a single message of %d bytes",
			ErrRequestTooLarge, w.Path.Endpoint, w.Path.Cluster, w.Path.Attribute, size, c.maxPayload)
	}
	c.current = append(c.current, w)
	c.used += size
	c.usedForStatuses += statusSize
	return nil
}

// write adds w — a whole-list IB too large for one message as a REPLACE
// and an ADD per remaining entry (:1397-1430).
func (c *writeChunker) write(w im.AttributeWrite) error {
	entries := c.oversizedListEntries(w)
	if entries == nil {
		return c.add(w)
	}
	if !w.Path.HasEndpoint {
		return fmt.Errorf("%w: a write of the list at a wildcard path does not fit a single message and cannot be chunked, because list ADDs need a concrete path", ErrRequestTooLarge)
	}
	replaceAll := func(count int) im.AttributeWrite {
		r := w
		r.Value = im.AttributeValue{Value: rawNode(spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, Children: entries[:count]})}
		return r
	}
	count := 0
	if !ListChunked(w.Path.Cluster, w.Path.Attribute) {
		for count < len(entries) && attributeDataSize(replaceAll(count+1)) <= c.available {
			count++
		}
	}
	if err := c.add(replaceAll(count)); err != nil {
		return err
	}
	for _, e := range entries[count:] {
		a := w
		a.Path.HasListIndex, a.Path.ListAppend = true, true
		a.Value = im.AttributeValue{Value: rawNode(e)}
		if err := c.add(a); err != nil {
			return err
		}
	}
	return nil
}

// oversizedListEntries are the entries of w, a whole-list IB too large for
// one message; nil for anything else (listEntriesOf, :113-140).
func (c *writeChunker) oversizedListEntries(w im.AttributeWrite) []spec.Node {
	if w.Path.HasListIndex || w.Path.ListAppend || attributeDataSize(w) <= c.available {
		return nil
	}
	v, ok := w.Value.Value.(spec.Encodable)
	if !ok {
		return nil
	}
	elements, isList, err := spec.ListElements(v)
	if err != nil || !isList {
		return nil
	}
	if elements == nil {
		elements = []spec.Node{}
	}
	return elements
}

// done flushes and marks the last chunk.
func (c *writeChunker) done() []im.WriteRequest {
	c.flush()
	c.chunks[len(c.chunks)-1].MoreChunkedMessages = false
	return c.chunks
}

// unversionReplacedAdds drops the DataVersion of the ADDs of a list whose
// versioned REPLACE succeeded in an earlier chunk: the REPLACE moved the
// version on (sendWriteCommand, :1283-1292).
func unversionReplacedAdds(writes []im.AttributeWrite, replaced []im.ConcreteAttributePath) []im.AttributeWrite {
	out := make([]im.AttributeWrite, len(writes))
	for i, w := range writes {
		if w.Path.ListAppend && w.HasDataVersion {
			for _, p := range replaced {
				if sameAttribute(p, w.Path) {
					w.DataVersion, w.HasDataVersion = 0, false
					break
				}
			}
		}
		out[i] = w
	}
	return out
}

// noteReplaced updates the lists whose versioned REPLACE succeeded, after
// a chunk's statuses (:1300-1312): a versioned REPLACE followed by ADDs
// ends its chunk, so its status is the last for its attribute.
func noteReplaced(replaced []im.ConcreteAttributePath, sent []im.AttributeWrite, statuses []im.AttributeStatus) []im.ConcreteAttributePath {
	for _, w := range sent {
		if w.Path.HasListIndex || w.Path.ListAppend || !w.HasDataVersion {
			continue
		}
		for i, p := range replaced {
			if sameAttribute(p, w.Path) {
				replaced = append(replaced[:i], replaced[i+1:]...)
				break
			}
		}
		for i := len(statuses) - 1; i >= 0; i-- {
			if sameAttribute(statuses[i].Path, w.Path) {
				if statuses[i].Status.Status == im.StatusSuccess {
					replaced = append(replaced, w.Path)
				}
				break
			}
		}
	}
	return replaced
}
