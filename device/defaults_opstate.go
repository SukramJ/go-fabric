// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	ovendef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	rvcopdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	"github.com/SukramJ/go-fabric/contract"
)

// The OperationalState derivations' server ([opstate]) on the declared
// CommandHandler (<derivation>def.Declare) — matter.js's
// OperationalStateServer, RvcOperationalStateServer and
// OvenCavityOperationalStateServer, whose state the host's device moves:
// OperationalStateList, OperationalState, PhaseList and CurrentPhase come
// from the declaration's Initial, the commands, CountdownTime and the
// OperationCompletion event from the selection.

func init() {
	selfHeldDefaults[opdef.ClusterID] = opstateServer(opstate.NewServer, func(p any) opstate.CommandHandler {
		if d, _ := p.(opdef.Declare); d.CommandHandler != nil {
			return opHandler[opdef.ErrorStateStruct]{d.CommandHandler}
		}
		return nil
	})
	selfHeldDefaults[rvcopdef.ClusterID] = opstateServer(opstate.NewRvcServer, func(p any) opstate.CommandHandler {
		if d, _ := p.(rvcopdef.Declare); d.CommandHandler != nil {
			return opHandler[rvcopdef.ErrorStateStruct]{d.CommandHandler}
		}
		return nil
	})
	selfHeldDefaults[ovendef.ClusterID] = opstateServer(opstate.NewOvenCavityServer, func(p any) opstate.CommandHandler {
		if d, _ := p.(ovendef.Declare); d.CommandHandler != nil {
			return opHandler[ovendef.ErrorStateStruct]{d.CommandHandler}
		}
		return nil
	})
}

// opPort is a derivation's declared CommandHandler, answering with its
// own generated ErrorStateStruct E.
type opPort[E any] interface {
	HandleOperationalCommand(ctx context.Context, cmdID uint32) (E, error)
}

// opHandler is an opPort as the [opstate.CommandHandler] the server calls.
type opHandler[E any] struct{ p opPort[E] }

// HandleOperationalCommand implements [opstate.CommandHandler].
func (h opHandler[E]) HandleOperationalCommand(ctx context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	e, err := h.p.HandleOperationalCommand(ctx, opCommandIDs[cmd])
	if err != nil {
		return opstate.ErrorState{}, err
	}
	rv := reflect.ValueOf(e)
	return opstate.ErrorState{
		ID:      opstate.ErrorID(uintField(rv, "ErrorStateId")), //nolint:gosec // the generated field is a uint8 enum
		Label:   stringField(rv, "ErrorStateLabel"),
		Details: stringField(rv, "ErrorStateDetails"),
	}, nil
}

// opCommandIDs are the command ids of [opstate.Command]'s bits.
var opCommandIDs = map[opstate.Command]uint32{
	opstate.CommandPause: opstate.CmdPause, opstate.CommandStop: opstate.CmdStop, opstate.CommandStart: opstate.CmdStart,
	opstate.CommandResume: opstate.CmdResume, opstate.CommandGoHome: opstate.CmdGoHome,
}

// opstateServer is the factory of a derivation's server.
func opstateServer(build func(opstate.Config) (*opstate.Server, error), port func(any) opstate.CommandHandler) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		h := port(b.ports)
		if h == nil {
			return nil, notServed(fmt.Errorf("%s: %w", b.def.Name, errNoPort))
		}
		cfg := opstate.Config{
			Handler: h, DeviceType: b.deviceType, Clock: b.clock, DataVersion: b.cfg.DataVersion,
			CountdownTime:       slices.Contains(b.opts.Attributes, opdef.AttrCountdownTime),
			OperationCompletion: slices.Contains(b.opts.Events, opdef.EventOperationCompletion),
		}
		for cmd, id := range opCommandIDs {
			if slices.Contains(b.opts.Commands, id) {
				cfg.Commands |= cmd
			}
		}
		if v, ok := b.initial[opdef.AttrOperationalStateList]; ok {
			delete(b.initial, opdef.AttrOperationalStateList)
			cfg.States = stateEntriesOf(v)
		}
		if v, ok := consume8(b.initial, opdef.AttrOperationalState); ok {
			cfg.State = opstate.State(v)
		}
		if v, ok := b.initial[opdef.AttrPhaseList]; ok {
			delete(b.initial, opdef.AttrPhaseList)
			cfg.Phases, _ = v.([]string)
		}
		if v, ok := consume8(b.initial, opdef.AttrCurrentPhase); ok {
			cfg.CurrentPhase = &v
		} else {
			delete(b.initial, opdef.AttrCurrentPhase) // null
		}
		return build(cfg)
	}
}

// stateEntriesOf converts a derivation's generated OperationalStateList —
// each derivation's own OperationalStateStruct, the same fields in every
// one — to the server's.
func stateEntriesOf(v any) []opstate.StateEntry {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	out := make([]opstate.StateEntry, 0, rv.Len())
	for i := range rv.Len() {
		e := reflect.Indirect(rv.Index(i))
		out = append(out, opstate.StateEntry{
			ID:    opstate.State(uintField(e, "OperationalStateId")), //nolint:gosec // the generated field is a uint8 enum
			Label: stringField(e, "OperationalStateLabel"),
		})
	}
	return out
}

// uintField is the unsigned field name of the struct v, the name compared
// without case; zero when there is none.
func uintField(v reflect.Value, name string) uint64 {
	f := fieldNamed(v, name)
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return f.Uint()
	default:
		return 0
	}
}

// stringField is the string or *string field name of the struct v; ""
// for none or nil.
func stringField(v reflect.Value, name string) string {
	f := fieldNamed(v, name)
	if f.Kind() == reflect.Pointer {
		if f.IsNil() {
			return ""
		}
		f = f.Elem()
	}
	if f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

// fieldNamed is the field name of the struct v, the name compared without
// case; the zero Value when there is none.
func fieldNamed(v reflect.Value, name string) reflect.Value {
	v = reflect.Indirect(v)
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, name) })
}
