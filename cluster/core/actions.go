// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// actionsMaxListLength bounds ActionList and EndpointLists: connectedhomeip
// reads at most kMaxActionListLength / kMaxEndpointListLength entries
// from its delegate (src/app/clusters/actions-server/ActionsCluster.cpp
// :34-35, :86, :103 at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20), the definition's "max 256".
const actionsMaxListLength = 256

// actionsCommands is every Actions request command. connectedhomeip
// accepts all twelve on every instance (ActionsCluster.cpp
// AcceptedCommands :231-252); which one an action supports is its
// SupportedCommands bitmap, checked per invoke.
var actionsCommands = []uint32{
	actionsdef.CmdInstantAction,
	actionsdef.CmdInstantActionWithTransition,
	actionsdef.CmdStartAction,
	actionsdef.CmdStartActionWithDuration,
	actionsdef.CmdStopAction,
	actionsdef.CmdPauseAction,
	actionsdef.CmdPauseActionWithDuration,
	actionsdef.CmdResumeAction,
	actionsdef.CmdEnableAction,
	actionsdef.CmdEnableActionWithDuration,
	actionsdef.CmdDisableAction,
	actionsdef.CmdDisableActionWithDuration,
}

// ActionRequest is one Actions command, decoded, for the host.
type ActionRequest struct {
	// Command is the command id (actionsdef.Cmd…).
	Command uint32
	// ActionID names the action; the server has checked it exists and
	// supports Command.
	ActionID uint16
	// InvokeID is the request's optional InvokeID.
	InvokeID *uint32
	// TransitionTime is InstantActionWithTransition's, nil otherwise.
	TransitionTime *uint16
	// Duration is the …WithDuration commands', nil otherwise.
	Duration *uint32
}

// ActionsHost is the host port of an Actions server, connectedhomeip's
// Actions::Delegate (src/app/clusters/actions-server/ActionsDelegate.h):
// the lists, read on every read, and the command handler.
type ActionsHost interface {
	// ActionList is the ActionList attribute; entries past the 256th are
	// not read.
	ActionList() []actionsdef.ActionStruct
	// EndpointLists is the EndpointLists attribute; entries past the
	// 256th are not read.
	EndpointLists() []actionsdef.EndpointListStruct
	// HandleAction carries out an accepted command. A nil error is
	// SUCCESS; an error carrying a status (a spec.StatusError) answers
	// that status, as a delegate's returned Status does
	// (ActionsCluster.cpp :167-211).
	HandleAction(ctx context.Context, req ActionRequest) error
}

// ActionsConfig carries the construction parameters of an Actions server.
type ActionsConfig struct {
	// Host supplies the lists and handles the commands. Required.
	Host ActionsHost
	// SetupURL, when set, serves the optional SetupURL attribute
	// (connectedhomeip serves it only when the endpoint carries it and
	// it is not empty: CodegenIntegration.cpp BuildOptionalAttributes
	// :33-41, SetupURLSpan :53-57).
	SetupURL *string
}

// Actions is an Actions (0x0025) server: the generated server with
// connectedhomeip's command validation in front of the host.
//
// Mirrors matter.js packages/node/src/behaviors/actions/ActionsServer.ts
// :14, `export class ActionsServer extends ActionsBehavior {}`: no server
// logic. The rules are ActionsCluster.cpp's at the harness pin:
//
//   - ActionList and EndpointLists are the host's, read on every read
//     (:70-77, :83-115).
//   - Every command first finds its action: an unknown ActionID is
//     NOT_FOUND (ValidateActionExists :117-122); then the action must
//     list the command in SupportedCommands, else INVALID_COMMAND
//     (ValidateCommandSupported :124-143, the bit test :141). A payload
//     that does not decode is INVALID_COMMAND (:154).
//   - The host's answer is the command's status (:165-212).
//   - StateChanged and ActionFailed are emitted on the host's word
//     ([Actions.EmitStateChanged], [Actions.EmitActionFailed]), as the
//     delegate generates them (:38-48).
type Actions struct {
	*spec.Server

	host ActionsHost
	dv   cluster.DataVersionTracker

	// mu guards endpoint.
	mu       sync.Mutex
	endpoint uint16
}

// NewActions builds an Actions server over cfg.Host.
func NewActions(cfg ActionsConfig) (*Actions, error) {
	if cfg.Host == nil {
		return nil, fmt.Errorf("core: Actions: %w", errNilActionsHost)
	}
	opts := spec.Options{Commands: actionsCommands}
	initial := map[uint32]any{}
	if cfg.SetupURL != nil {
		opts.Attributes = []uint32{actionsdef.AttrSetupUrl}
		initial[actionsdef.AttrSetupUrl] = *cfg.SetupURL
	}
	a := &Actions{host: cfg.Host}
	handlers := make(map[uint32]spec.CommandHandler, len(actionsCommands))
	for _, cmd := range actionsCommands {
		handlers[cmd] = func(ctx context.Context, fields any) (any, error) { return nil, a.invoke(ctx, cmd, fields) }
	}
	srv, err := spec.NewServer(actionsdef.Definition, opts, spec.ServerConfig{
		Handlers:    handlers,
		DataVersion: &a.dv,
		Source:      actionsSource{cfg.Host},
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("core: Actions: %w", err)
	}
	a.Server = srv
	return a, nil
}

// errNilActionsHost: NewActions without a host.
var errNilActionsHost = errors.New("core: a host is required")

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it when it mounts the server.
func (a *Actions) SetEndpoint(endpoint uint16) {
	a.mu.Lock()
	a.endpoint = endpoint
	a.mu.Unlock()
}

func (a *Actions) eventEndpoint() uint16 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.endpoint
}

// EmitStateChanged emits StateChanged for actionID with the InvokeID the
// command carried (Matter: 0 when it carried none).
func (a *Actions) EmitStateChanged(actionID uint16, invokeID uint32, state actionsdef.ActionStateEnum) error {
	return a.Emit(a.eventEndpoint(), actionsdef.EventStateChanged, actionsdef.StateChangedEvent{
		ActionId: actionID, InvokeId: invokeID, NewState: state,
	})
}

// EmitActionFailed emits ActionFailed for actionID.
func (a *Actions) EmitActionFailed(actionID uint16, invokeID uint32, state actionsdef.ActionStateEnum, cause actionsdef.ActionErrorEnum) error {
	return a.Emit(a.eventEndpoint(), actionsdef.EventActionFailed, actionsdef.ActionFailedEvent{
		ActionId: actionID, InvokeId: invokeID, NewState: state, Error: cause,
	})
}

// ActionListChanged reports that the host's ActionList moved
// (ActionsCluster.cpp ActionListModified :254-257).
func (a *Actions) ActionListChanged() { a.changed(actionsdef.AttrActionList) }

// EndpointListsChanged reports that the host's EndpointLists moved
// (EndpointListsModified :259-262).
func (a *Actions) EndpointListsChanged() { a.changed(actionsdef.AttrEndpointLists) }

func (a *Actions) changed(attrID uint32) {
	a.dv.Bump()
	a.Notify(attrID)
}

// invoke validates one command as DECODE_AND_VALIDATE does (:152-161) and
// hands it to the host.
func (a *Actions) invoke(ctx context.Context, cmd uint32, fields any) error {
	req, ok := actionRequest(cmd, fields)
	if !ok {
		return spec.Errorf(im.StatusInvalidCommand, "Actions: command 0x%02X payload %T", cmd, fields)
	}
	action, found := a.findAction(req.ActionID)
	if !found {
		return spec.Errorf(im.StatusNotFound, "Actions: no action 0x%04X", req.ActionID)
	}
	if uint32(action.SupportedCommands)&(1<<cmd) == 0 {
		return spec.Errorf(im.StatusInvalidCommand, "Actions: action 0x%04X does not support command 0x%02X", req.ActionID, cmd)
	}
	if err := a.host.HandleAction(ctx, req); err != nil {
		return fmt.Errorf("core: Actions: %w", err)
	}
	return nil
}

// findAction looks actionID up in the list a read would answer.
func (a *Actions) findAction(actionID uint16) (actionsdef.ActionStruct, bool) {
	for _, act := range boundActionList(a.host.ActionList()) {
		if act.ActionId == actionID {
			return act, true
		}
	}
	return actionsdef.ActionStruct{}, false
}

// actionRequest flattens a decoded command payload; false for a payload
// that is not cmd's.
func actionRequest(cmd uint32, fields any) (ActionRequest, bool) {
	r := ActionRequest{Command: cmd}
	var want uint32
	switch f := fields.(type) {
	case actionsdef.InstantActionRequest:
		want = actionsdef.CmdInstantAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.InstantActionWithTransitionRequest:
		want = actionsdef.CmdInstantActionWithTransition
		r.ActionID, r.InvokeID, r.TransitionTime = f.ActionId, f.InvokeId, &f.TransitionTime
	case actionsdef.StartActionRequest:
		want = actionsdef.CmdStartAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.StartActionWithDurationRequest:
		want = actionsdef.CmdStartActionWithDuration
		r.ActionID, r.InvokeID, r.Duration = f.ActionId, f.InvokeId, &f.Duration
	case actionsdef.StopActionRequest:
		want = actionsdef.CmdStopAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.PauseActionRequest:
		want = actionsdef.CmdPauseAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.PauseActionWithDurationRequest:
		want = actionsdef.CmdPauseActionWithDuration
		r.ActionID, r.InvokeID, r.Duration = f.ActionId, f.InvokeId, &f.Duration
	case actionsdef.ResumeActionRequest:
		want = actionsdef.CmdResumeAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.EnableActionRequest:
		want = actionsdef.CmdEnableAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.EnableActionWithDurationRequest:
		want = actionsdef.CmdEnableActionWithDuration
		r.ActionID, r.InvokeID, r.Duration = f.ActionId, f.InvokeId, &f.Duration
	case actionsdef.DisableActionRequest:
		want = actionsdef.CmdDisableAction
		r.ActionID, r.InvokeID = f.ActionId, f.InvokeId
	case actionsdef.DisableActionWithDurationRequest:
		want = actionsdef.CmdDisableActionWithDuration
		r.ActionID, r.InvokeID, r.Duration = f.ActionId, f.InvokeId, &f.Duration
	default:
		return r, false
	}
	return r, want == cmd
}

func boundActionList(l []actionsdef.ActionStruct) []actionsdef.ActionStruct {
	return l[:min(len(l), actionsMaxListLength)]
}

// actionsSource answers the two lists from the host.
type actionsSource struct{ host ActionsHost }

// MatterAttribute implements [spec.Source].
func (s actionsSource) MatterAttribute(attrID uint32) (any, bool) {
	switch attrID {
	case actionsdef.AttrActionList:
		return spec.List[actionsdef.ActionStruct](append([]actionsdef.ActionStruct{}, boundActionList(s.host.ActionList())...)), true
	case actionsdef.AttrEndpointLists:
		l := s.host.EndpointLists()
		return spec.List[actionsdef.EndpointListStruct](append([]actionsdef.EndpointListStruct{}, l[:min(len(l), actionsMaxListLength)]...)), true
	}
	return nil, false
}

var _ contract.EventReceiver = (*Actions)(nil)
