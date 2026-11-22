// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	opdef "thingfabric.dev/go-fabric/cluster/spec/operationalstate"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
)

// opPort is an OperationalState host port: every command succeeds.
type opPort struct{}

func (opPort) HandleOperationalCommand(context.Context, uint32) (opdef.ErrorStateStruct, error) {
	return opdef.ErrorStateStruct{}, nil
}

// TestDeclaredCommandNeedsAHandler (review II, phase 5b): a declaration
// that selects an optional command (Declare.Commands) for a cluster the
// generated default server serves, and gives it no Handle field, is
// refused by New with ErrConfiguration naming the command — the generated
// server would leave it out of AcceptedCommandList, so the declaration
// would have no effect. With its Handle field the command is accepted; a
// kit server, which carries the command out through its port, is not
// held to it. Before, New built the device and the command was silently
// unlisted.
func TestDeclaredCommandNeedsAHandler(t *testing.T) {
	t.Parallel()
	stopped := opdef.OperationalStateStopped
	build := func(d opdef.Declare) (*device.Device, error) {
		d.Initial.OperationalState = &stopped
		return device.New(device.LaundryWasherType(), device.Options{StableKey: endpoint.StringKey("washer"), Name: "washer"}, d)
	}
	accepted := func(t *testing.T, d *device.Device) []uint32 {
		t.Helper()
		lister, ok := d.Server(opdef.ClusterID).(contract.ClusterCommandLister)
		if !ok {
			t.Fatalf("OperationalState server %T lists no commands", d.Server(opdef.ClusterID))
		}
		return lister.MatterAcceptedCommands()
	}

	_, err := build(opdef.Declare{Commands: []uint32{opdef.CmdPause}})
	if !errors.Is(err, device.ErrConfiguration) || !strings.Contains(err.Error(), "Pause (0x00)") {
		t.Errorf("Pause declared without a handler = %v, want ErrConfiguration naming Pause (0x00)", err)
	}

	d, err := build(opdef.Declare{Commands: []uint32{opdef.CmdPause}, HandlePause: func(context.Context, opdef.PauseRequest) (opdef.OperationalCommandResponse, error) {
		return opdef.OperationalCommandResponse{}, nil
	}})
	if err != nil {
		t.Fatalf("Pause declared with HandlePause: %v", err)
	}
	if got := accepted(t, d); !slices.Contains(got, opdef.CmdPause) {
		t.Errorf("AcceptedCommandList %v, want Pause in it", got)
	}

	// The kit's opstate server takes Pause with Resume, and a state list
	// with the error state.
	states := make([]opdef.OperationalStateStruct, 0, 4)
	for _, id := range []opdef.OperationalStateEnum{0, 1, 2, 3} {
		states = append(states, opdef.OperationalStateStruct{OperationalStateId: id})
	}
	d, err = build(opdef.Declare{
		Commands: []uint32{opdef.CmdPause, opdef.CmdResume}, CommandHandler: opPort{},
		Initial: opdef.Patch{OperationalStateList: &states},
	})
	if err != nil {
		t.Fatalf("Pause declared on the kit server (through its port): %v", err)
	}
	if got := accepted(t, d); !slices.Contains(got, opdef.CmdPause) {
		t.Errorf("kit server's AcceptedCommandList %v, want Pause in it", got)
	}
}
