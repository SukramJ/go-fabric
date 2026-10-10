// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/im"
)

// DefaultUserLabelMaxLabels is the LabelList length a UserLabel server
// accepts when its config names none: matter.js UserLabelServer.State
// maxLabels = 255 (packages/node/src/behaviors/user-label/
// UserLabelServer.ts:39); the specification asks for at least 4 (:37).
const DefaultUserLabelMaxLabels = 255

// ErrUserLabelConfig rejects a UserLabel configuration whose initial
// labels exceed MaxLabels.
var ErrUserLabelConfig = errors.New("core: UserLabel: labels exceed MaxLabels")

// UserLabelConfig carries the construction parameters of a UserLabel
// server.
type UserLabelConfig struct {
	// Labels is the LabelList the server starts with — what the host
	// persisted from an earlier write.
	Labels []userlabeldef.LabelStruct
	// MaxLabels caps the LabelList length a write may set; 0 is
	// [DefaultUserLabelMaxLabels].
	MaxLabels int
	// OnWrite, when set, is handed every LabelList a controller wrote
	// that passed the checks, before it is stored — the host persists it
	// here. An error refuses the write (FAILURE, or the status the error
	// carries) and leaves LabelList as it was.
	OnWrite func(ctx context.Context, labels []userlabeldef.LabelStruct) error
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewUserLabel builds a UserLabel (0x0041) server on the generated
// definition (cluster/spec/userlabel, ADR 0013).
//
// Mirrors matter.js packages/node/src/behaviors/user-label/
// UserLabelServer.ts: a LabelList longer than maxLabels is refused with
// RESOURCE_EXHAUSTED (#validateLabelListLength, :24-31), checked as the
// labelList$Changing reaction (:21) — after the value decoded against
// the attribute's schema, so a label or value longer than 16 characters
// is CONSTRAINT_ERROR first, as connectedhomeip answers it too
// (src/app/clusters/user-label-server/UserLabelCluster.cpp IsValidLabelEntry
// :47-51, :70, :84 at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20). A list-append write reaches
// the server as the whole list with the entry appended
// (endpoint.appendListValue), so the cap holds for it too, the status
// chip answers an append beyond its capacity with (:87-91).
func NewUserLabel(cfg UserLabelConfig) (*spec.Server, error) {
	limit := cfg.MaxLabels
	if limit <= 0 {
		limit = DefaultUserLabelMaxLabels
	}
	if len(cfg.Labels) > limit {
		return nil, fmt.Errorf("%w: %d labels, at most %d", ErrUserLabelConfig, len(cfg.Labels), limit)
	}
	srv, err := spec.NewServer(userlabeldef.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        userLabelSink{limit: limit, onWrite: cfg.OnWrite},
		Initial: map[uint32]any{
			userlabeldef.AttrLabelList: spec.List[userlabeldef.LabelStruct](slices.Clone(cfg.Labels)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: UserLabel: %w", err)
	}
	return srv, nil
}

// userLabelSink applies the maxLabels rule and hands the accepted list to
// the host.
type userLabelSink struct {
	limit   int
	onWrite func(ctx context.Context, labels []userlabeldef.LabelStruct) error
}

// MatterWriteAttribute implements [spec.Sink].
func (s userLabelSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	// The bridge decodes a LabelList write into the generated list type
	// (bridge/attribute_value_reader.go), the type the server reads back
	// and encodes; any other shape would be stored unencodable.
	labels, ok := value.(spec.List[userlabeldef.LabelStruct])
	if !ok {
		return spec.Errorf(im.StatusConstraintError, "UserLabel: LabelList %T is not a list of LabelStruct", value)
	}
	if len(labels) > s.limit {
		// matter.js UserLabelServer.ts:25-30.
		return spec.Errorf(im.StatusResourceExhausted, "UserLabel: LabelList length %d exceeds supported maximum of %d", len(labels), s.limit)
	}
	if s.onWrite != nil {
		if err := s.onWrite(ctx, slices.Clone(labels)); err != nil {
			return fmt.Errorf("core: UserLabel: %w", err)
		}
	}
	return nil
}
