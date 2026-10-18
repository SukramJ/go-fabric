// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/CommandInvokeResponseTest.ts at
// 862ad75f: command invokes against a server node — concrete and wildcard,
// absent elements, client clusters, the access passes, handler errors, the
// batch, unimplemented and malformed commands, fabric-scoped commands over
// PASE, and the endpoints an invoke dispatches to.
//
// matter.js runs CommandInvokeResponse on a MockServerNode at the mock's
// default access level, Operate (invokeCmdRaw, :827-829); the port invokes
// over the wire from a commissioned fabrictest pair granted Operate
// (Pair.Grant), or over a PASE session (Controller.PASE) where matter.js
// builds a session without fabric. A batch, a wildcard path, a
// SuppressResponse request and a malformed payload go out raw
// (fabrictest.Exchange), past the controller's single-command
// formulation. A matter.js server subclass that throws is a Go cluster
// server wrapping the OnOff server whose MatterInvoke returns the error;
// matter.js's counts are not on the wire, the statuses are.
//
// The OnOffLight is a bridged endpoint below the Aggregator (endpoint 1),
// so the lights sit on endpoints 2 and 3 where matter.js has 1 and 2; the
// OTA requestor is a bridged OtaRequestor endpoint.
package node_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	admincomm "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/chime"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	otareq "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// invokeEntry is one expected InvokeResponse entry, a status.
type invokeEntry struct {
	ep            uint16
	cluster, cmd  uint32
	status        im.StatusCode
	clusterStatus *uint8
	ref           *uint16
}

func TestCommandInvokeResponseParity(t *testing.T) {
	t.Parallel()

	on := func(ep uint16) im.CommandInvocation {
		return im.CommandInvocation{Path: cmdPath(ep, onoff.ClusterID, onoff.CmdOn)}
	}

	t.Run("invoke concrete command", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate, lightSpec("light:1"))
		ep := h.ep("light:1")
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{on(ep)}}),
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusSuccess})
	})

	t.Run("invokes existing endpoint wildcard commands", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate, lightSpec("light:1"), lightSpec("light:2"))
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wildcardCmd(onoff.ClusterID, onoff.CmdOn)}}}),
			invokeEntry{ep: h.ep("light:1"), cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusSuccess},
			invokeEntry{ep: h.ep("light:2"), cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusSuccess})
	})

	t.Run("produces results regardless of suppressResponse", func(t *testing.T) {
		t.Parallel()
		// matter.js asserts CommandInvokeResponse.process's results; the
		// messaging layer applies SuppressResponse. On the wire: no
		// response, both commands run.
		h := newInvokeHost(t, store.PrivilegeOperate, lightSpec("light:1"), lightSpec("light:2"))
		payload := encodeInvoke(t, im.InvokeRequest{SuppressResponse: true, Invokes: []im.CommandInvocation{{Path: wildcardCmd(onoff.ClusterID, onoff.CmdOn)}}})
		if err := fabrictest.Send(parityCtx(t), h.pair, im.OpcodeInvokeRequest, payload); err != nil {
			t.Fatal(err)
		}
		if err := h.site.Eventually(parityCtx(t), "both lights on", func() bool {
			return h.lights["light:1"].OnOff() && h.lights["light:2"].OnOff()
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invokes non-existing endpoint wildcard command", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate)
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: wildcardCmd(onoff.ClusterID, onoff.CmdOn)}}}))
	})

	t.Run("invoke non existing concrete command", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate)
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{on(0)}}),
			invokeEntry{ep: 0, cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusUnsupportedCluster})
	})

	t.Run("invokes model-known absent high-privilege command as existence status for operate-only subject", func(t *testing.T) {
		t.Parallel()
		// Groups.AddGroup: invoke privilege Manage, cluster absent on the
		// root.
		h := newInvokeHost(t, store.PrivilegeOperate)
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: cmdPath(0, groups.ClusterID, groups.CmdAddGroup)}}}),
			invokeEntry{ep: 0, cluster: groups.ClusterID, cmd: groups.CmdAddGroup, status: im.StatusUnsupportedCluster})
	})

	// matter.js adds the OnOff client cluster to the root two ways
	// (withClientClusters, a required ClientBehavior); a Go endpoint
	// declares its client clusters in one, endpoint.Spec.ClientClusters —
	// both cases invoke OnOff.On on an endpoint that serves OnOff as a
	// client only.
	for _, name := range []string{
		"does not invoke a command on a client cluster declared via withClientClusters",
		"does not invoke a command on a client cluster added via require",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newInvokeHost(t, store.PrivilegeOperate, endpointSpec{key: "switch:1", build: func(site *fabrictest.Site) (node.Endpoint, error) {
				return device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch:1"), Name: "switch", Clock: site.Clock()})
			}})
			ep := h.ep("switch:1")
			h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{on(ep)}}),
				invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusUnsupportedCluster})
		})
	}

	t.Run("counts an existing command denied at the actual-privilege ACL pass as existent", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate, lightSpec("light:1"))
		ep := h.ep("light:1")
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: cmdPath(ep, groups.ClusterID, groups.CmdAddGroup)}}}),
			invokeEntry{ep: ep, cluster: groups.ClusterID, cmd: groups.CmdAddGroup, status: im.StatusUnsupportedAccess})
	})

	handlerError := func(name string, err error, want invokeEntry) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newInvokeHost(t, store.PrivilegeOperate, failingLightSpec("light:1", func() error { return err }))
			ep := h.ep("light:1")
			want.ep, want.cluster, want.cmd = ep, onoff.ClusterID, onoff.CmdOn
			h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{on(ep)}}), want)
		})
	}
	// StatusResponseError("boom", ConstraintError, 0x42).
	handlerError("clamps the outer status to FAILURE when a handler reports a cluster-specific status",
		clusterStatusError{status: im.StatusConstraintError, cluster: 0x42},
		invokeEntry{status: im.StatusFailure, clusterStatus: new(uint8(0x42))})
	// MatterFlowError("boom"): an error without a status code.
	handlerError("reports an error with no defined status code as a per-command FAILURE",
		errors.New("matter flow: boom"), invokeEntry{status: im.StatusFailure})
	handlerError("reports a plain Error escaping a handler as a per-command FAILURE",
		errors.New("boom"), invokeEntry{status: im.StatusFailure})

	t.Run("keeps sibling results and continues the batch when one command throws", func(t *testing.T) {
		t.Parallel()
		var thrown atomic.Bool
		h := newInvokeHost(t, store.PrivilegeOperate, failingLightSpec("light:1", func() error {
			if thrown.Swap(true) {
				return nil
			}
			return errors.New("matter flow: boom")
		}))
		ep := h.ep("light:1")
		ref := func(r uint16) *uint16 { return &r }
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{
			{Path: cmdPath(ep, onoff.ClusterID, onoff.CmdOff), CommandRef: 1, HasCommandRef: true},
			{Path: cmdPath(ep, onoff.ClusterID, onoff.CmdOn), CommandRef: 2, HasCommandRef: true},
			{Path: cmdPath(ep, onoff.ClusterID, onoff.CmdToggle), CommandRef: 3, HasCommandRef: true},
		}}),
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOff, status: im.StatusSuccess, ref: ref(1)},
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusFailure, ref: ref(2)},
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdToggle, status: im.StatusSuccess, ref: ref(3)})
		if !h.lights["light:1"].OnOff() {
			t.Fatal("OnOff false after Off, a failed On and Toggle")
		}
	})

	t.Run("reports an unimplemented mandatory command as UNSUPPORTED_COMMAND and omits it from AcceptedCommandList", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate, chimeSpec("chime:1"))
		ep := h.ep("chime:1")
		r, err := fabrictest.Read(parityCtx(t), h.pair, attrPath(ep, chime.ClusterID, 0xFFF9))
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Value.Children) != 0 {
			t.Fatalf("AcceptedCommandList %+v, want empty", r.Value.Children)
		}
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: cmdPath(ep, chime.ClusterID, chime.CmdPlayChimeSound)}}}),
			invokeEntry{ep: ep, cluster: chime.ClusterID, cmd: chime.CmdPlayChimeSound, status: im.StatusUnsupportedCommand})
	})

	t.Run("reports a payload that does not match the command schema as INVALID_COMMAND", func(t *testing.T) {
		t.Parallel()
		h := newInvokeHost(t, store.PrivilegeOperate, lightSpec("light:1"))
		ep := h.ep("light:1")
		// OnWithTimedOff expects a structure; a bare integer cannot decode.
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{
			Path: cmdPath(ep, onoff.ClusterID, onoff.CmdOnWithTimedOff), Fields: interactionRaw(tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 5}),
		}}}),
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOnWithTimedOff, status: im.StatusInvalidCommand})
	})

	announce := func(ep uint16, wildcard bool) im.CommandInvocation {
		p := cmdPath(ep, otareq.ClusterID, otareq.CmdAnnounceOtaProvider)
		if wildcard {
			p = wildcardCmd(otareq.ClusterID, otareq.CmdAnnounceOtaProvider)
		}
		return im.CommandInvocation{Path: p, Fields: otareq.AnnounceOtaProviderRequest{
			ProviderNodeId: 1, VendorId: 1, AnnouncementReason: otareq.AnnouncementReasonSimpleAnnouncement, Endpoint: 0,
		}}
	}

	t.Run("refuses a fabric-scoped command over a PASE session with UNSUPPORTED_ACCESS", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Bool
		h := newPASEHost(t, requestorSpec("ota:1", &ran, true))
		ep := h.ep("ota:1")
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{announce(ep, false)}}),
			invokeEntry{ep: ep, cluster: otareq.ClusterID, cmd: otareq.CmdAnnounceOtaProvider, status: im.StatusUnsupportedAccess})
	})

	t.Run("the framework fabric-scoped guard refuses invocation before the handler runs, even without a handler-level guard", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Bool
		h := newPASEHost(t, requestorSpec("ota:1", &ran, false))
		ep := h.ep("ota:1")
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{announce(ep, false)}}),
			invokeEntry{ep: ep, cluster: otareq.ClusterID, cmd: otareq.CmdAnnounceOtaProvider, status: im.StatusUnsupportedAccess})
		if ran.Load() {
			t.Fatal("the handler ran")
		}
	})

	t.Run("invokes a command that is not fabric-scoped over the same PASE session", func(t *testing.T) {
		t.Parallel()
		h := newPASEHost(t, lightSpec("light:1"))
		ep := h.ep("light:1")
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{on(ep)}}),
			invokeEntry{ep: ep, cluster: onoff.ClusterID, cmd: onoff.CmdOn, status: im.StatusSuccess})
	})

	t.Run("skips a fabric-scoped command on a wildcard path over a PASE session rather than reporting it", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Bool
		h := newPASEHost(t, requestorSpec("ota:1", &ran, false))
		h.assert(t, rawInvoke(t, h.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{announce(0, true)}}))
		if ran.Load() {
			t.Fatal("the handler ran")
		}
	})
}

// endpointSpec builds one bridged endpoint of an invoke host.
type endpointSpec struct {
	key   string
	build func(site *fabrictest.Site) (node.Endpoint, error)
	// onOff is the endpoint's OnOff server when the device's accessor
	// cannot return it (a wrapped server).
	onOff *atomic.Pointer[onoff.Server]
}

func lightSpec(key string, with ...device.Option) endpointSpec {
	return endpointSpec{key: key, build: func(site *fabrictest.Site) (node.Endpoint, error) {
		return device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: site.Clock()}, with...)
	}}
}

// failingOnOff is the OnOff server whose On answers fail() when it returns
// an error — matter.js's OnOffServer subclass whose on() throws.
type failingOnOff struct {
	*onoff.Server
	fail func() error
}

func (f *failingOnOff) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID == onoff.CmdOn {
		if err := f.fail(); err != nil {
			return nil, err
		}
	}
	return f.Server.MatterInvoke(ctx, cmdID, fields)
}

func failingLightSpec(key string, fail func() error) endpointSpec {
	var held atomic.Pointer[onoff.Server]
	return endpointSpec{key: key, onOff: &held, build: func(site *fabrictest.Site) (node.Endpoint, error) {
		srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, TimeSource: site.Clock()})
		if err != nil {
			return nil, err
		}
		held.Store(srv)
		return device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: site.Clock()},
			device.WithServer(&failingOnOff{Server: srv, fail: fail}))
	}}
}

// clusterStatusError is matter.js's StatusResponseError with a cluster
// code.
type clusterStatusError struct {
	status  im.StatusCode
	cluster uint8
}

func (e clusterStatusError) Error() string                   { return "boom" }
func (e clusterStatusError) MatterStatusCode() im.StatusCode { return e.status }
func (e clusterStatusError) MatterClusterStatus() uint8      { return e.cluster }

// chimeSpec is matter.js's ChimeDevice without a PlayChimeSound handler.
// The module's generated server refuses to be built without a handler for
// a mandatory command (cluster/spec/server.go NewServer), so a
// hand-written server that accepts no command stands in: the state the
// case tests — a mandatory command the server does not implement — is the
// one a host's own server can still produce.
func chimeSpec(key string) endpointSpec {
	return endpointSpec{key: key, build: func(*fabrictest.Site) (node.Endpoint, error) {
		return &customEndpoint{key: key, deviceType: chimeDeviceType, servers: []contract.ClusterServer{&fakeServer{
			cluster: chime.ClusterID,
			attrs: map[uint32]any{
				chime.AttrInstalledChimeSounds: []chime.ChimeSoundStruct{{ChimeId: 0, Name: "Ding"}},
				chime.AttrSelectedChime:        uint8(0),
				chime.AttrEnabled:              true,
			},
		}}}, nil
	}}
}

// chimeDeviceType is the Chime device type (0x0146).
const chimeDeviceType uint16 = 0x0146

// requestorSpec is matter.js's OtaRequestorEndpoint; guarded makes its
// AnnounceOTAProvider handler refuse a session without fabric itself, as
// matter.js's OtaSoftwareUpdateRequestorServer does, unguarded is
// NonSelfGuardingOtaRequestorServer. ran records that the handler ran. The
// device layer has no default requestor server (its DefaultOTAProviders is
// fabric-scoped), so a hand-written one serves the cluster.
func requestorSpec(key string, ran *atomic.Bool, guarded bool) endpointSpec {
	return endpointSpec{key: key, build: func(*fabrictest.Site) (node.Endpoint, error) {
		return &customEndpoint{key: key, deviceType: otaRequestorDeviceType, servers: []contract.ClusterServer{&fakeServer{
			cluster:  otareq.ClusterID,
			accepted: []uint32{otareq.CmdAnnounceOtaProvider},
			attrs: map[uint32]any{
				otareq.AttrDefaultOtaProviders: []any{}, otareq.AttrUpdatePossible: true,
				otareq.AttrUpdateState: uint8(1), otareq.AttrUpdateStateProgress: nil,
			},
			invoke: func(ctx context.Context, _ uint32) error {
				ran.Store(true)
				if _, fabric := im.FabricFilterFromContext(ctx); guarded && fabric == 0 {
					return spec.Errorf(im.StatusUnsupportedAccess, "no fabric")
				}
				return nil
			},
		}}}, nil
	}}
}

// otaRequestorDeviceType is the OTA Requestor device type (0x0012).
const otaRequestorDeviceType uint16 = 0x0012

// fakeServer is a hand-written cluster server: fixed attributes, the
// commands it accepts, and an invoke hook.
type fakeServer struct {
	cluster  uint32
	attrs    map[uint32]any
	accepted []uint32
	invoke   func(ctx context.Context, cmd uint32) error
}

func (f *fakeServer) MatterClusterID() uint32 { return f.cluster }

func (f *fakeServer) MatterRead(attrID uint32) (any, bool) {
	v, ok := f.attrs[attrID]
	return v, ok
}

func (f *fakeServer) MatterWrite(context.Context, uint32, any) error {
	return spec.Errorf(im.StatusUnsupportedWrite, "read-only")
}

func (f *fakeServer) MatterInvoke(ctx context.Context, cmdID uint32, _ any) (any, error) {
	if f.invoke == nil {
		return nil, im.UnsupportedCommandf("no command 0x%02x", cmdID)
	}
	return nil, f.invoke(ctx, cmdID)
}

func (f *fakeServer) MatterReportable() []uint32 {
	ids := make([]uint32, 0, len(f.attrs))
	for id := range f.attrs {
		ids = append(ids, id)
	}
	return ids
}

func (f *fakeServer) MatterAcceptedCommands() []uint32 { return append([]uint32{}, f.accepted...) }

func (f *fakeServer) MatterGeneratedCommands() []uint32 { return []uint32{} }

// customEndpoint is a bridged endpoint of the given servers.
type customEndpoint struct {
	key        string
	deviceType uint16
	servers    []contract.ClusterServer
}

func (c *customEndpoint) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(c.key), DeviceAddress: c.key, ChannelAddress: c.key + ":0",
		DeviceType: c.deviceType, FriendlyName: c.key, Source: c,
	}
}
func (c *customEndpoint) MatterDeviceType() uint16                       { return c.deviceType }
func (c *customEndpoint) MatterClusterServers() []contract.ClusterServer { return c.servers }

// invokeHost is a pair whose device serves some bridged endpoints.
type invokeHost struct {
	site   *fabrictest.Site
	pair   *fabrictest.Pair
	lights map[string]*onoff.Server
}

func buildEndpoints(t *testing.T, site *fabrictest.Site, specs []endpointSpec) ([]node.Endpoint, map[string]*onoff.Server) {
	t.Helper()
	var eps []node.Endpoint
	lights := map[string]*onoff.Server{}
	for _, s := range specs {
		ep, err := s.build(site)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case s.onOff != nil:
			lights[s.key] = s.onOff.Load()
		default:
			if l, ok := ep.(*device.OnOffLight); ok {
				lights[s.key] = l.OnOff()
			}
		}
		eps = append(eps, ep)
	}
	return eps, lights
}

// newInvokeHost commissions a device serving specs and grants the
// controller privilege on it.
func newInvokeHost(t *testing.T, privilege store.Privilege, specs ...endpointSpec) *invokeHost {
	t.Helper()
	return newInvokeHostOn(t, quietSite(t), privilege, specs...)
}

func newInvokeHostOn(t *testing.T, site *fabrictest.Site, privilege store.Privilege, specs ...endpointSpec) *invokeHost {
	t.Helper()
	eps, lights := buildEndpoints(t, site, specs)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: eps}})
	if err := p.Grant(parityCtx(t), privilege); err != nil {
		t.Fatal(err)
	}
	return &invokeHost{site: site, pair: p, lights: lights}
}

// newPASEHost opens a PASE session to an uncommissioned device serving
// specs — the session without fabric of matter.js's paseExchange.
func newPASEHost(t *testing.T, specs ...endpointSpec) *invokeHost {
	t.Helper()
	site := quietSite(t)
	eps, lights := buildEndpoints(t, site, specs)
	u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: eps}})
	p, err := u.Controller.PASE(parityCtx(t), u.Device)
	if err != nil {
		t.Fatal(err)
	}
	return &invokeHost{site: site, pair: p, lights: lights}
}

func (h *invokeHost) ep(key string) uint16 {
	ep, ok := h.pair.Device.EndpointOf(key)
	if !ok {
		panic("no endpoint " + key)
	}
	return ep
}

// assert asserts an InvokeResponse of exactly want, in order.
func (h *invokeHost) assert(t *testing.T, a rawAnswer[im.InvokeResponse], want ...invokeEntry) {
	t.Helper()
	if a.status != nil {
		t.Fatalf("StatusResponse 0x%02x, want an InvokeResponse", *a.status)
	}
	got := a.resp.Responses
	if len(got) != len(want) {
		t.Fatalf("responses %+v, want %+v", got, want)
	}
	for i, w := range want {
		g := got[i]
		ok := g.IsStatus && g.Path.Endpoint == w.ep && g.Path.Cluster == w.cluster && g.Path.Command == w.cmd && g.Status.Status == w.status
		ok = ok && g.Status.HasClusterStatus == (w.clusterStatus != nil) && (w.clusterStatus == nil || g.Status.ClusterStatus == *w.clusterStatus)
		ok = ok && g.HasCommandRef == (w.ref != nil) && (w.ref == nil || g.CommandRef == *w.ref)
		if !ok {
			t.Fatalf("response %d: %+v, want %+v", i, g, w)
		}
	}
}

// interactionRaw is a command field of one TLV element.
func interactionRaw(el tlv.Element) spec.Encodable {
	return rawElement{el}
}

type rawElement struct{ el tlv.Element }

func (r rawElement) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.PutUint(tag, r.el.Uint)
}

// TestCommandInvokeResponseDispatchEndpointsParity ports the describe group
// "reports the endpoints it dispatches to before any command runs"
// (CommandInvokeResponseTest.ts:665-806). matter.js reads the set its
// beforeDispatch hook receives; on the wire the set is what an
// InvokeRequest's DelayReportData defers, so each case invokes with a
// 1 s deferral and asserts which subscription's next report is held
// (deferHost, DelayReportDataTest's port).
func TestCommandInvokeResponseDispatchEndpointsParity(t *testing.T) {
	t.Parallel()

	delay := &im.DelayReportData{DelayMinMs: 1000}
	// deferred asserts whether r's next report, caused by change, is held
	// by the invoke that ran before it.
	deferred := func(t *testing.T, d *deferHost, r *reports, start time.Time, change func(), held bool) {
		t.Helper()
		change()
		if held {
			d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
		} else {
			d.expectReportAt(t, r, start, 0, sendDelay)
		}
	}
	invoke := func(t *testing.T, p *fabrictest.Pair, path im.ConcreteCommandPath, fields any) {
		t.Helper()
		if a := rawInvoke(t, p, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path, Fields: fields}}, DelayReportData: delay}); a.status != nil {
			t.Fatalf("invoke: StatusResponse 0x%02x", uint8(*a.status))
		}
	}
	// pase opens a PASE session from a second controller to the
	// commissioned device — matter.js paseExchange (:676-686).
	pase := func(t *testing.T, d *deferHost) *fabrictest.Pair {
		t.Helper()
		ctx := parityCtx(t)
		if err := d.pair.Device.OpenWindow(ctx, 0); err != nil {
			t.Fatal(err)
		}
		p, err := d.site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222}).PASE(ctx, d.pair.Device)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("names the endpoint of a concrete path", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		invoke(t, d.pair, cmdPath(d.lightEP, onoff.ClusterID, onoff.CmdOn), nil)
		deferred(t, d, r, start, func() {}, true)
	})

	t.Run("names every endpoint a wildcard path expands to", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t, lightSpec("light:2"))
		second := d.ep("light:2")
		r1 := d.subscribe(t, d.onOffPath(), 60)
		r2 := d.subscribe(t, attrPath(second, onoff.ClusterID, onoff.AttrOnOff), 60)
		start := d.now()
		invoke(t, d.pair, wildcardCmd(onoff.ClusterID, onoff.CmdOn), nil)
		d.expectReportAt(t, r1, start, time.Second, time.Second+sendDelay)
		if got := r2.times(); len(got) != 1 || got[0].Sub(start) < time.Second {
			t.Fatalf("the second light's reports %v, want one held past 1 s", got)
		}
	})

	t.Run("leaves out a concrete path that fails validation", func(t *testing.T) {
		t.Parallel()
		// OnOff on endpoint 0, which serves no OnOff.
		d := newDeferHost(t)
		r := d.subscribe(t, wildNodeLabel(), 60)
		start := d.now()
		invoke(t, d.pair, cmdPath(0, onoff.ClusterID, onoff.CmdOn), nil)
		deferred(t, d, r, start, func() { d.relabel(t) }, false)
		// Groups.AddGroup on the light, which an Operate subject may not
		// invoke.
		d = newDeferHost(t)
		r = d.subscribe(t, d.onOffPath(), 60)
		if err := d.pair.Grant(parityCtx(t), store.PrivilegeOperate); err != nil {
			t.Fatal(err)
		}
		start = d.now()
		invoke(t, d.pair, cmdPath(d.lightEP, groups.ClusterID, groups.CmdAddGroup), nil)
		deferred(t, d, r, start, func() { d.setOnOff(t, true) }, false)
	})

	t.Run("leaves out a concrete path that needs a timed interaction it lacks", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		p := pase(t, d)
		r := d.subscribe(t, nodeLabelAt(0), 60)
		start := d.now()
		invoke(t, p, cmdPath(0, admincomm.ClusterID, admincomm.CmdRevokeCommissioning), nil)
		deferred(t, d, r, start, func() { d.relabel(t) }, false)
	})

	t.Run("leaves out a fabric-scoped concrete path on a session without fabric", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Bool
		d := newDeferHost(t, requestorSpec("ota:1", &ran, false))
		ota := d.ep("ota:1")
		p := pase(t, d)
		r := d.subscribe(t, wildNodeLabel(), 60)
		start := d.now()
		invoke(t, p, cmdPath(ota, otareq.ClusterID, otareq.CmdAnnounceOtaProvider), otareq.AnnounceOtaProviderRequest{
			ProviderNodeId: 1, VendorId: 1, AnnouncementReason: otareq.AnnouncementReasonSimpleAnnouncement,
		})
		deferred(t, d, r, start, func() { d.relabel(t) }, false)
		if ran.Load() {
			t.Fatal("the handler ran")
		}
	})

	t.Run("leaves out endpoints a wildcard path may not invoke on", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		if err := d.pair.Grant(parityCtx(t), store.PrivilegeOperate); err != nil {
			t.Fatal(err)
		}
		start := d.now()
		invoke(t, d.pair, wildcardCmd(groups.ClusterID, groups.CmdAddGroup), nil)
		deferred(t, d, r, start, func() { d.setOnOff(t, true) }, false)
	})
}
