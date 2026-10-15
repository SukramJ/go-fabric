// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
)

// InputState is the MediaInput attribute state (media-input.element.ts:
// 25-29). InputList is mandatory and has no default: nil refuses
// construction ([ErrMissingState]).
type InputState struct {
	InputList    []inputdef.InputInfoStruct
	CurrentInput uint8
}

func (st InputState) values() map[uint32]any {
	return map[uint32]any{
		inputdef.AttrInputList:    listOr(st.InputList),
		inputdef.AttrCurrentInput: st.CurrentInput,
	}
}

func (st InputState) clone() InputState {
	st.InputList = slices.Clone(st.InputList)
	return st
}

// Inputs is the MediaInput host port: SelectInput, ShowInputStatus and
// HideInputStatus (:30-35), status only. A port that also implements
// [InputRenamer] takes RenameInput.
type Inputs interface {
	SelectInput(ctx context.Context, st *InputState, req inputdef.SelectInputRequest) error
	ShowInputStatus(ctx context.Context, st *InputState) error
	HideInputStatus(ctx context.Context, st *InputState) error
}

// InputRenamer is RenameInput, required with NameUpdates (NU, :36-40).
type InputRenamer interface {
	RenameInput(ctx context.Context, st *InputState, req inputdef.RenameInputRequest) error
}

// InputsConfig carries the construction parameters of a MediaInput server.
type InputsConfig struct {
	// Port carries out the commands; required.
	Port Inputs
	// Features is the FeatureMap (NU).
	Features inputdef.Feature
	// Initial is the state the server starts with.
	Initial InputState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// InputsServer is a MediaInput (0x0507) server.
type InputsServer struct {
	*spec.Server
	stateful[InputState]
}

// NewInputs builds a MediaInput server over cfg.Port.
func NewInputs(cfg InputsConfig) (*InputsServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: MediaInput", ErrNilPort)
	}
	port := cfg.Port
	renamer, canRename := port.(InputRenamer)
	cmds := []command{
		{inputdef.CmdSelectInput, true},
		{inputdef.CmdShowInputStatus, true},
		{inputdef.CmdHideInputStatus, true},
		{inputdef.CmdRenameInput, canRename},
	}
	srv, err := spec.NewServer(inputdef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: MediaInput: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s := &InputsServer{Server: srv}
	if err := s.init(srv, cfg.Initial, InputState.values, InputState.clone); err != nil {
		return nil, fmt.Errorf("media: MediaInput: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	handleState(srv, &s.stateful, inputdef.CmdSelectInput, statusOnly(port.SelectInput))
	handleState(srv, &s.stateful, inputdef.CmdShowInputStatus, func(ctx context.Context, st *InputState, _ inputdef.ShowInputStatusRequest) (any, error) {
		return nil, port.ShowInputStatus(ctx, st)
	})
	handleState(srv, &s.stateful, inputdef.CmdHideInputStatus, func(ctx context.Context, st *InputState, _ inputdef.HideInputStatusRequest) (any, error) {
		return nil, port.HideInputStatus(ctx, st)
	})
	if canRename {
		handleState(srv, &s.stateful, inputdef.CmdRenameInput, statusOnly(renamer.RenameInput))
	}
	return s, nil
}
