// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// relay is an OnOff Sink: the device's switch.
type relay struct {
	mu       sync.Mutex
	switched []bool
}

func (r *relay) SetOnOff(_ context.Context, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.switched = append(r.switched, on)
	return nil
}

func (r *relay) calls() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.switched...)
}

// uintValue is an unsigned integer as a write's value.
type uintValue uint64

func (v uintValue) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, uint64(v)) }

func attrPath(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestDeclareOverTheWire: every field of a cluster's Declare is what a
// controller observes — the Initial state read back, the Features in the
// FeatureMap, the OnWrite veto refusing a write, a Handle replacing a
// command, and the declared port receiving one (brief BU §1).
func TestDeclareOverTheWire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})

	sink := &relay{}
	var toggles int
	var mu sync.Mutex
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "Lamp"},
		onoffdef.Declare{
			Initial: onoffdef.Patch{OnOff: new(true), OnTime: new(uint16(7))},
			Sink:    sink,
			OnWrite: func(_ context.Context, next onoffdef.State, attrID uint32) error {
				if attrID == onoffdef.AttrOnTime && next.OnTime > 100 {
					return spec.Errorf(im.StatusConstraintError, "OnTime %d above the device's 100", next.OnTime)
				}
				return nil
			},
			HandleToggle: func(context.Context, onoffdef.ToggleRequest) (struct{}, error) {
				mu.Lock()
				toggles++
				mu.Unlock()
				return struct{}{}, nil
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	cover, err := device.NewWindowCovering(device.Options{StableKey: endpoint.StringKey("cover"), Name: "Blind"},
		wcdef.Declare{Features: wcdef.FeatureLift | wcdef.FeaturePositionAwareLift})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light, cover}}})
	lightEP, ok := light.EndpointNumber()
	if !ok {
		t.Fatal("the light has no endpoint number after it was added")
	}
	coverEP, _ := cover.EndpointNumber()

	// Initial.
	got, err := fabrictest.Read(ctx, pair, attrPath(lightEP, onoffdef.ClusterID, onoffdef.AttrOnTime))
	if err != nil || got.Value.El.Uint != 7 {
		t.Fatalf("OnTime = %+v, %v; want the declared 7", got.Value.El, err)
	}
	// Features.
	got, err = fabrictest.Read(ctx, pair, attrPath(coverEP, wcdef.ClusterID, spec.AttrFeatureMap))
	if want := uint64(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift); err != nil || got.Value.El.Uint != want {
		t.Fatalf("FeatureMap = %+v, %v; want 0x%X", got.Value.El, err, want)
	}
	// OnWrite.
	var se *interaction.StatusError
	if err := fabrictest.Write(ctx, pair, attrPath(lightEP, onoffdef.ClusterID, onoffdef.AttrOnTime), uintValue(500)); !errors.As(err, &se) || se.Status != im.StatusConstraintError {
		t.Fatalf("write OnTime 500: %v, want CONSTRAINT_ERROR from the veto", err)
	}
	if light.OnOff().Get().OnTime != 7 {
		t.Errorf("OnTime after the vetoed write = %d, want 7", light.OnOff().Get().OnTime)
	}
	if err := fabrictest.Write(ctx, pair, attrPath(lightEP, onoffdef.ClusterID, onoffdef.AttrOnTime), uintValue(50)); err != nil {
		t.Fatalf("write OnTime 50: %v", err)
	}
	// Handle replaces Toggle: the host's handler ran, the state stayed.
	toggle := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := toggles
	mu.Unlock()
	if n != 1 || !light.OnOff().Get().OnOff {
		t.Errorf("after Toggle: the handler ran %d times, OnOff %v; want once and still on", n, light.OnOff().Get().OnOff)
	}
	// The port: Off reaches the Sink before it is stored.
	off := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdOff}
	if _, err := fabrictest.Invoke(ctx, pair, off, nil); err != nil {
		t.Fatal(err)
	}
	if calls := sink.calls(); len(calls) != 1 || calls[0] || light.OnOff().Get().OnOff {
		t.Errorf("after Off: the Sink saw %v, OnOff %v; want [false] and off", calls, light.OnOff().Get().OnOff)
	}
}

// TestDeclareOnAnyServer: a declaration's hooks go on the host's own server
// (HostServer) as on the kit's, and an empty declaration of an assembler
// cluster the type allows opts the endpoint in.
func TestDeclareOnAnyServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{Features: uint32(onoffdef.FeatureLighting)}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	var handled bool
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("host")},
		onoffdef.Declare{HostServer: srv, HandleOn: func(context.Context, onoffdef.OnRequest) (struct{}, error) { handled = true; return struct{}{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if light.Server(onoffdef.ClusterID) != srv {
		t.Fatal("the host's server is not mounted")
	}
	if _, err := srv.MatterInvoke(ctx, onoffdef.CmdOn, onoffdef.OnRequest{}); err != nil || !handled {
		t.Errorf("On on the host's server: %v, handled %v; want the declared handler", err, handled)
	}
	purifier, err := device.NewAirPurifier(device.Options{StableKey: endpoint.StringKey("air")}, groupsdef.Declare{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(purifier.Spec().OptIn, groupsdef.ClusterID) {
		t.Errorf("groupsdef.Declare{} opt-ins %v, want Groups", purifier.Spec().OptIn)
	}
}

// TestDeclareRefusals: a cluster declared twice, and a declaration whose
// hook the server refuses, are configuration errors.
func TestDeclareRefusals(t *testing.T) {
	t.Parallel()
	opts := device.Options{StableKey: endpoint.StringKey("x")}
	if _, err := device.NewOnOffLight(opts, onoffdef.Declare{}, onoffdef.Declare{}); !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("OnOff declared twice: %v, want ErrConfiguration", err)
	}
	if _, err := device.NewOnOffLight(opts, wcdef.Declare{}); !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("WindowCovering on a light: %v, want ErrConfiguration", err)
	}
	host, _ := spec.NewServer(onoffdef.Definition, spec.Options{Features: uint32(onoffdef.FeatureLighting)}, spec.ServerConfig{Unimplemented: true})
	if _, err := device.NewOnOffLight(opts, onoffdef.Declare{HostServer: host, Sink: &relay{}}); !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("a port beside a HostServer: %v, want ErrConfiguration", err)
	}
	if _, err := device.NewOnOffLight(opts, onoffdef.Declare{HostServer: host, Initial: onoffdef.Patch{OnOff: new(true)}}); !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("Initial beside a HostServer: %v, want ErrConfiguration", err)
	}
	if _, err := device.NewOnOffLight(opts, onoffdef.Declare{
		HandleOffWithEffect: func(context.Context, onoffdef.OffWithEffectRequest) (struct{}, error) { return struct{}{}, nil },
		Features:            onoffdef.FeatureOffOnly,
	}); !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("a handler for a command the selection excludes: %v, want ErrConfiguration", err)
	}
}
