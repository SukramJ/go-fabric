// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
)

// NewFixedLabel builds a FixedLabel (0x0040) server whose LabelList holds
// labels. It is the generated server with nothing added.
//
// Mirrors matter.js packages/node/src/behaviors/fixed-label/FixedLabelServer.ts,
// `export class FixedLabelServer extends FixedLabelBehavior {}`: the
// behavior generated from the model, with no server logic of its own.
func NewFixedLabel(labels []fixedlabeldef.LabelStruct) (*spec.Server, error) {
	srv, err := spec.NewServer(fixedlabeldef.Definition, spec.Options{}, spec.ServerConfig{
		Initial: map[uint32]any{
			fixedlabeldef.AttrLabelList: spec.List[fixedlabeldef.LabelStruct](slices.Clone(labels)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: FixedLabel: %w", err)
	}
	return srv, nil
}
