// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ReadRequest is a read — matter.js Read() (action/request/Read.ts:28-91)
// with numeric paths.
type ReadRequest struct {
	// Attributes are the attribute paths, wildcards allowed.
	Attributes []im.ConcreteAttributePath
	// Events are the event paths, wildcards allowed.
	Events []im.ConcreteEventPath
	// EventFilters are the minimum event numbers; matter.js sets eventMin
	// only (Read.ts:171-176).
	EventFilters []im.EventMinimumNumber
	// DataVersionFilters skip clusters whose data the client holds; dropped
	// or trimmed when the request would not fit one message
	// (InteractionMessenger.ts:998-1050).
	DataVersionFilters []im.DataVersionFilter
	// NoFabricFilter reads fabric-scoped lists unfiltered; a read is
	// fabric-filtered by default (fabricFilter = true, Read.ts:40).
	NoFabricFilter bool
}

// ErrRequestTooLarge is a request that does not fit one message even
// without its DataVersionFilters — matter.js's MatterFlowError of
// InteractionMessenger.ts:1010-1014.
var ErrRequestTooLarge = errors.New("interaction: request does not fit one message")

// Read reads attributes on a fresh exchange — [Interaction.ReadWith] with
// attribute paths only.
func (c *Interaction) Read(ctx context.Context, paths ...im.ConcreteAttributePath) (*Report, error) {
	return c.ReadWith(ctx, ReadRequest{Attributes: paths})
}

// ReadWith reads attributes and events on a fresh exchange and returns the
// report, its chunks merged. Mirrors matter.js ClientInteraction.read
// (ClientInteraction.ts:262-298) at 0ceb202d: more paths than the
// provider's readPathsSupported is only logged (:263-268); the request is
// encoded to fit one message (InteractionMessenger.ts:994-1050); the
// reports are read as readDataReports does (:938-972) and each chunk goes
// through InputChunk (action/client/InputChunk.ts) — a list continued in
// the next chunk is reassembled, a malformed entry skipped. Errors of the
// exchange layer pass through unchanged.
func (c *Interaction) ReadWith(ctx context.Context, r ReadRequest) (*Report, error) {
	if n := len(r.Attributes) + len(r.Events); n > c.provider.ReadPathsSupported() {
		c.log.Info("matter.controller.interaction.read_paths", slog.Int("paths", n), slog.Int("supported", c.provider.ReadPathsSupported()))
	}
	req, err := encodeReadingRequest(im.ReadRequest{
		AttributeRequests: r.Attributes, EventRequests: r.Events, EventFilters: r.EventFilters,
		FabricFiltered: !r.NoFabricFilter, DataVersionFilters: r.DataVersionFilters,
	})
	if err != nil {
		return nil, err
	}
	m, err := c.begin(ctx, ExchangeOptions{})
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	if err := m.send(ctx, im.OpcodeReadRequest, req, 0); err != nil {
		return nil, err
	}
	a := newAssembler(c.log)
	a.report.Session = m.x.Session()
	if err := readDataReports(ctx, c.log, m, nil, func(rd *im.ReportData) error {
		a.add(rd)
		return nil
	}); err != nil {
		return nil, err
	}
	return a.report, nil
}

// readingRequest is a ReadRequest or a SubscribeRequest, whose
// DataVersionFilters #encodeReadingRequest may shorten.
type readingRequest interface {
	encode(filters []im.DataVersionFilter) ([]byte, error)
	filters() []im.DataVersionFilter
}

type readRequest im.ReadRequest

func (r readRequest) encode(filters []im.DataVersionFilter) ([]byte, error) {
	req := im.ReadRequest(r)
	req.DataVersionFilters = filters
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc)
	return enc.Bytes()
}

func (r readRequest) filters() []im.DataVersionFilter { return r.DataVersionFilters }

// encodeReadingRequest encodes a ReadRequest to fit one message.
func encodeReadingRequest(r im.ReadRequest) ([]byte, error) {
	return encodeFitting(readRequest(r))
}

// emptyArraySize is what matter.js's `dataVersionFilters: []` adds to the
// request it measures (InteractionMessenger.ts:1006-1009): the array's
// control byte, context tag and end of container.
const emptyArraySize = 3

// encodeFitting is #encodeReadingRequest and #shortenDataVersionFilters
// (InteractionMessenger.ts:998-1050): the request as it is when it fits
// exchange.MaxPayloadSize; otherwise without filters, an error when even
// that does not fit; otherwise with as many filters, in order, as fit the
// space left, the first that does not fit ending the list.
func encodeFitting(r readingRequest) ([]byte, error) {
	b, err := r.encode(r.filters())
	if err != nil || len(b) <= exchange.MaxPayloadSize {
		return b, err
	}
	without, err := r.encode(nil)
	if err != nil {
		return nil, err
	}
	base := len(without) + emptyArraySize
	if base > exchange.MaxPayloadSize {
		return nil, fmt.Errorf("%w: %d bytes without DataVersionFilters", ErrRequestTooLarge, len(without))
	}
	available := exchange.MaxPayloadSize - base
	var kept []im.DataVersionFilter
	for _, f := range r.filters() {
		size := im.DataVersionFilterSize(f)
		if size > available {
			break
		}
		kept = append(kept, f)
		available -= size
	}
	return r.encode(kept)
}
