// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
)

// OutputState is the AudioOutput attribute state (audio-output.element.ts:
// 25-29). OutputList is mandatory and has no default: nil refuses
// construction ([ErrMissingState]).
type OutputState struct {
	OutputList    []outputdef.OutputInfoStruct
	CurrentOutput uint8
}

func (st OutputState) values() map[uint32]any {
	return map[uint32]any{
		outputdef.AttrOutputList:    listOr(st.OutputList),
		outputdef.AttrCurrentOutput: st.CurrentOutput,
	}
}

func (st OutputState) clone() OutputState {
	st.OutputList = slices.Clone(st.OutputList)
	return st
}

// Outputs is the AudioOutput host port: SelectOutput (:30-33), status
// only; the port makes the output current in st. A port that also
// implements [OutputRenamer] takes RenameOutput.
type Outputs interface {
	SelectOutput(ctx context.Context, st *OutputState, req outputdef.SelectOutputRequest) error
}

// OutputRenamer is RenameOutput, required with NameUpdates (NU, :34-38).
type OutputRenamer interface {
	RenameOutput(ctx context.Context, st *OutputState, req outputdef.RenameOutputRequest) error
}

// OutputsConfig carries the construction parameters of an AudioOutput
// server.
type OutputsConfig struct {
	// Port carries out the commands; required.
	Port Outputs
	// Features is the FeatureMap (NU).
	Features outputdef.Feature
	// Initial is the state the server starts with.
	Initial OutputState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// OutputsServer is an AudioOutput (0x050B) server.
type OutputsServer struct {
	*spec.Server
	stateful[OutputState]
}

// NewOutputs builds an AudioOutput server over cfg.Port.
func NewOutputs(cfg OutputsConfig) (*OutputsServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: AudioOutput", ErrNilPort)
	}
	renamer, canRename := cfg.Port.(OutputRenamer)
	cmds := []command{{outputdef.CmdSelectOutput, true}, {outputdef.CmdRenameOutput, canRename}}
	s := &OutputsServer{}
	h := handlers{}
	handleState(h, &s.stateful, outputdef.CmdSelectOutput, statusOnly(cfg.Port.SelectOutput))
	if canRename {
		handleState(h, &s.stateful, outputdef.CmdRenameOutput, statusOnly(renamer.RenameOutput))
	}
	srv, err := newServer(outputdef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: AudioOutput: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, OutputState.values, OutputState.clone); err != nil {
		return nil, fmt.Errorf("media: AudioOutput: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}
