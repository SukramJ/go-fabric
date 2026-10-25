// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestHostStatusError: a host handler's contract.StatusError reaches the
// controller as its status — TIMEOUT for a late Toggle, FAILURE with a
// cluster status for On, CONSTRAINT_ERROR for a vetoed write — where any
// other error answers FAILURE.
func TestHostStatusError(t *testing.T) {
	t.Parallel()
	clusterStatus := uint8(0x42)
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"},
		onoffdef.Declare{
			HandleToggle: func(context.Context, onoffdef.ToggleRequest) (struct{}, error) {
				return struct{}{}, contract.StatusError{Status: contract.StatusTimeout, Message: "the device did not answer"}
			},
			HandleOn: func(context.Context, onoffdef.OnRequest) (struct{}, error) {
				return struct{}{}, &contract.StatusError{Status: uint8(im.StatusFailure), ClusterStatus: &clusterStatus}
			},
			HandleOff: func(context.Context, onoffdef.OffRequest) (struct{}, error) {
				return struct{}{}, errors.New("plain")
			},
			OnWrite: func(context.Context, onoffdef.State, uint32) error {
				return contract.StatusError{Status: uint8(im.StatusConstraintError)}
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, _ := light.EndpointNumber()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		cmd           uint32
		fields        spec.Encodable
		status        im.StatusCode
		clusterStatus *uint8
	}{
		{onoffdef.CmdToggle, onoffdef.ToggleRequest{}, im.StatusCode(contract.StatusTimeout), nil},
		{onoffdef.CmdOn, onoffdef.OnRequest{}, im.StatusFailure, &clusterStatus},
		{onoffdef.CmdOff, onoffdef.OffRequest{}, im.StatusFailure, nil},
	} {
		_, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, onoffdef.ClusterID, tc.cmd), tc.fields)
		var se *client.StatusError
		if !errors.As(err, &se) || se.Status != tc.status || (se.ClusterStatus == nil) != (tc.clusterStatus == nil) ||
			(se.ClusterStatus != nil && *se.ClusterStatus != *tc.clusterStatus) {
			t.Fatalf("command 0x%02X: %v, want status 0x%02X cluster status %v", tc.cmd, err, uint8(tc.status), tc.clusterStatus)
		}
	}
	err = fabrictest.Write(ctx, pair, client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnTime), spec.Uint16(10))
	var se *client.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusConstraintError {
		t.Fatalf("a vetoed write: %v, want CONSTRAINT_ERROR", err)
	}
}
