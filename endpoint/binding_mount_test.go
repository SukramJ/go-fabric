// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
)

// TestDeclaredClientMountsBinding: an endpoint that declares a bindable
// application client (OnOff) reports it in its Descriptor's ClientList,
// satisfies Base's Client condition and gets the Binding server Base then
// mandates ("Simple & Client") without its source supplying one — matter.js
// BindingServer.ts:54-56, ResolvedEndpoint.ts:120-139,
// ConditionAssertions.ts:600-602. One that declares only an unbindable
// client (WebRtcTransportProvider, effectiveBindable false) does not; nor
// does one that declares none. The mounted server is the endpoint's own:
// the same instance on every dispatch.
//
// It holds three cases of matter.js
// packages/node/test/endpoint/validation/DeviceTypeConformanceTest.ts at
// 862ad75f: "is added to an endpoint of a simple device type with a
// mandatory application client" (:531-539, the bound switch), "is not
// added to an endpoint without an application client" (:568-576, none)
// and "is neither added nor required where every application client is
// one a binding never directs" (:578-586, unbindable; CameraController
// itself in TestBindingMountParityMatterJS).
func TestDeclaredClientMountsBinding(t *testing.T) {
	t.Parallel()
	const switchDT, onOff, webRTCProvider = 0x0103, 0x0006, 0x0553
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src := func() contract.EndpointSource {
		return deviceTypeSource{dt: switchDT, servers: []contract.ClusterServer{dtServer{id: 0x0003}}}
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{
		{StableKey: StringKey("bound"), DeviceType: switchDT, FriendlyName: "Bound", ClientClusters: []uint32{onOff}, Source: src()},
		{StableKey: StringKey("unbindable"), DeviceType: switchDT, FriendlyName: "Unbindable", ClientClusters: []uint32{webRTCProvider}, Source: src()},
		{StableKey: StringKey("none"), DeviceType: switchDT, FriendlyName: "None", Source: src()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*Endpoint{}
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil {
			byKey[ep.SourceKey.String()] = ep
		}
	}
	bindingOf := func(ep *Endpoint) *mattercore.Binding {
		for _, srv := range ClusterServers(ep) {
			if b, ok := srv.(*mattercore.Binding); ok {
				return b
			}
		}
		return nil
	}
	bound := byKey["bound"]
	b := bindingOf(bound)
	if b == nil {
		t.Fatal("the endpoint declaring an OnOff client has no Binding server")
	}
	if again := bindingOf(bound); again != b {
		t.Fatal("the Binding server is rebuilt per dispatch")
	}
	for _, srv := range ClusterServers(bound) {
		if srv.MatterClusterID() == 0x001D {
			v, _ := srv.MatterRead(0x0002)
			if l, _ := v.([]uint32); !slices.Equal(l, []uint32{onOff}) {
				t.Fatalf("ClientList = %v", v)
			}
			v, _ = srv.MatterRead(0x0001)
			if l, _ := v.([]uint32); !slices.Contains(l, bindingClusterID) {
				t.Fatalf("ServerList %v lacks Binding", v)
			}
		}
	}
	for _, key := range []string{"unbindable", "none"} {
		if bindingOf(byKey[key]) != nil {
			t.Errorf("%s: a Binding server was mounted", key)
		}
	}

	// Validation: the bound endpoint carries what Base mandates; the others
	// owe nothing, since Client does not hold for them.
	for _, v := range ValidateDeviceTypes(topo) {
		if v.Requirement == "Binding" {
			t.Errorf("Binding violation: %v", v)
		}
	}
}

// TestBaseMandatesBinding pins the evaluation of Base's "Simple & Client"
// Binding requirement: a simple device type with a bindable application
// client, and nothing else.
func TestBaseMandatesBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dt      uint16
		clients []uint32
		want    bool
	}{
		{"simple with OnOff client", 0x0103, []uint32{0x0006}, true},
		{"unknown primary listing a simple device type", 0xFFF0, []uint32{0x0006}, false},
		{"simple light with an OccupancySensing client", 0x0100, []uint32{0x0406}, true},
		{"simple with an unbindable client", 0x0103, []uint32{0x0553}, false},
		{"simple with a utility client only", 0x0103, []uint32{0x0003}, false},
		{"simple without clients", 0x0103, nil, false},
		{"node device type with OnOff client", 0x0016, []uint32{0x0006}, false},
		{"unknown device type", 0xFFF0, []uint32{0x0006}, false},
	}
	for _, c := range cases {
		if got := baseMandatesBinding([]uint16{c.dt}, c.clients); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// The DeviceTypeList, not the primary device type alone, decides: a
	// manufacturer-specific type that also lists OnOffLightSwitch is simple.
	if !baseMandatesBinding([]uint16{0xFFF0, 0x0103}, []uint32{0x0006}) {
		t.Error("a listed simple device type does not mandate Binding")
	}
}

// bindingOf is the Binding server ep mounts, or nil.
func bindingOf(ep *Endpoint) contract.ClusterServer {
	for _, srv := range ClusterServers(ep) {
		if srv.MatterClusterID() == bindingClusterID {
			return srv
		}
	}
	return nil
}

// assembleOne assembles one bridged endpoint of spec, with the node's group
// state so a light's Groups and ScenesManagement mount, and returns the
// topology and the endpoint.
func assembleOne(t *testing.T, spec Spec) (*Topology, *Endpoint) {
	t.Helper()
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, Groups: mgr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{spec}}})
	if err != nil {
		t.Fatal(err)
	}
	return topo, topo.Bridged()[0]
}

// bindingViolations are the Binding violations of ep in topo.
func bindingViolations(topo *Topology, ep *Endpoint) []DeviceTypeViolation {
	var out []DeviceTypeViolation
	all := ValidateDeviceTypes(topo)
	for i := range all {
		if v := all[i]; v.Endpoint == ep.ID && v.Requirement == "Binding" {
			out = append(out, v)
		}
	}
	return out
}

// TestBindingMountParityMatterJS ports the "a missing Binding server"
// cases of matter.js
// packages/node/test/endpoint/validation/DeviceTypeConformanceTest.ts at
// 862ad75f that TestDeclaredClientMountsBinding does not hold: the
// assembler mounts the Binding server Base mandates as matter.js's
// DeviceTypeConformanceService #addBaseServers injects it
// (DeviceTypeConformanceService.ts:361-369, baseServerOf :670-672), judged
// over the endpoint's DeviceTypeList, whatever the validation mode, and
// never over a server the source supplies with Binding's id.
func TestBindingMountParityMatterJS(t *testing.T) {
	t.Parallel()
	const (
		onOffLight, onOffSwitch, cameraController, powerSource = 0x0100, 0x0103, 0x0147, 0x0011
		onOff, identify, powerSourceCl, webRTCRequestor        = 0x0006, 0x0003, 0x002F, 0x0554
		customSimple                                           = 0xFFF1
	)
	switchSource := func(extra ...contract.ClusterServer) contract.EndpointSource {
		return deviceTypeSource{dt: onOffSwitch, servers: append([]contract.ClusterServer{dtServer{id: identify, cmds: []uint32{0x00, 0x40}}}, extra...)}
	}

	// :541-556: a behavior with the Binding server's id is not replaced. A Go
	// server with cluster id 0x001E is a Binding cluster server — there is
	// no behavior id apart from the cluster — so the source's is kept and no
	// second is mounted; the "missing Binding" verdict over a non-cluster
	// behavior has no Go subject.
	t.Run("is not added over a behavior with the Binding server's id", func(t *testing.T) {
		t.Parallel()
		own := &dtServer{id: bindingClusterID}
		_, ep := assembleOne(t, Spec{StableKey: StringKey("sw"), DeviceType: onOffSwitch, FriendlyName: "sw", ClientClusters: []uint32{identify, onOff}, Source: switchSource(own)})
		n := 0
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() == bindingClusterID {
				n++
				if srv != contract.ClusterServer(own) {
					t.Errorf("Binding server %T replaced the source's", srv)
				}
			}
		}
		if n != 1 {
			t.Errorf("%d Binding servers, want the source's one", n)
		}
	})

	// :558-566: OnOffLightDevice.withClientClusters(OnOffClient).
	t.Run("is added for an application client the developer adds", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{
			StableKey: StringKey("light"), DeviceType: onOffLight, FriendlyName: "light", ClientClusters: []uint32{onOff},
			Source: deviceTypeSource{dt: onOffLight, servers: []contract.ClusterServer{lightingOnOff()}},
		})
		if bindingOf(ep) == nil {
			t.Fatal("no Binding server")
		}
		for _, v := range ValidateDeviceTypes(topo) {
			if v.Endpoint == ep.ID {
				t.Errorf("violation %v", v)
			}
		}
	})

	// :578-586 with matter.js's CameraControllerDevice: its application
	// clients (WebRtcTransportProvider M) are ones a binding never directs.
	t.Run("is neither added nor required where every application client is one a binding never directs", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{
			StableKey: StringKey("cc"), DeviceType: cameraController, FriendlyName: "cc", ClientClusters: []uint32{0x0553},
			Source: deviceTypeSource{dt: cameraController, servers: []contract.ClusterServer{dtServer{id: webRTCRequestor}}},
		})
		if bindingOf(ep) != nil {
			t.Error("Binding server mounted")
		}
		if vs := bindingViolations(topo, ep); len(vs) != 0 {
			t.Errorf("Binding violations %v", vs)
		}
	})

	// :588-597: PowerSourceEndpoint.withClientClusters(OnOffClient), a
	// utility endpoint.
	t.Run("is not added to a utility endpoint with an application client", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{
			StableKey: StringKey("ps"), DeviceType: powerSource, FriendlyName: "ps", ClientClusters: []uint32{onOff},
			Source: deviceTypeSource{dt: powerSource, servers: []contract.ClusterServer{dtServer{id: powerSourceCl}}},
		})
		if bindingOf(ep) != nil {
			t.Error("Binding server mounted")
		}
		if vs := bindingViolations(topo, ep); len(vs) != 0 {
			t.Errorf("Binding violations %v", vs)
		}
	})

	// :599-609: the node endpoint with an OnOff client. The root is the
	// assembler's and declares no clients; the predicate is RootNode's.
	t.Run("is not added to a node endpoint with an application client", func(t *testing.T) {
		t.Parallel()
		if baseMandatesBinding([]uint16{0x0016}, []uint32{onOff}) {
			t.Error("Base mandates Binding of the node endpoint")
		}
		topo, _ := assembleOne(t, Spec{StableKey: StringKey("sw"), DeviceType: onOffSwitch, FriendlyName: "sw", ClientClusters: []uint32{onOff}, Source: switchSource()})
		for _, ep := range topo.Endpoints {
			if ep.IsRoot() && bindingOf(ep) != nil {
				t.Error("the root mounts Binding")
			}
		}
	})

	// :611-622: a Binding server the source declares is kept.
	t.Run("keeps the Binding server the developer declares", func(t *testing.T) {
		t.Parallel()
		own := mattercore.NewBinding()
		_, ep := assembleOne(t, Spec{StableKey: StringKey("sw"), DeviceType: onOffSwitch, FriendlyName: "sw", ClientClusters: []uint32{identify, onOff}, Source: switchSource(own)})
		if got := bindingOf(ep); got != contract.ClusterServer(own) {
			t.Errorf("Binding server = %p, want the source's %p", got, own)
		}
	})

	// :624-633: a simple device type the model does not define is not
	// judged and gets no Binding.
	t.Run("is not added to an endpoint of a device type the model does not define, which is not judged", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{
			StableKey: StringKey("custom"), DeviceType: customSimple, FriendlyName: "custom", ClientClusters: []uint32{onOff},
			Source: deviceTypeSource{dt: customSimple},
		})
		if bindingOf(ep) != nil {
			t.Error("Binding server mounted")
		}
		for _, v := range ValidateDeviceTypes(topo) {
			if v.Endpoint == ep.ID {
				t.Errorf("violation %v", v)
			}
		}
	})

	// :635-646: the DeviceTypeList states OnOffLightSwitch beside a
	// manufacturer-specific primary type.
	t.Run("is added where the DeviceTypeList states a simple device type the endpoint type does not", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{
			StableKey: StringKey("custom"), DeviceType: customSimple, DeviceTypes: []uint16{onOffSwitch}, FriendlyName: "custom", ClientClusters: []uint32{onOff},
			Source: deviceTypeSource{dt: customSimple},
		})
		if bindingOf(ep) == nil {
			t.Fatal("no Binding server")
		}
		if vs := bindingViolations(topo, ep); len(vs) != 0 {
			t.Errorf("Binding violations %v", vs)
		}
	})

	// :648-658: the mount does not consult the validation mode — the
	// assembler has none — and an off-mode validator over the topology
	// changes nothing.
	t.Run("is added with validation off", func(t *testing.T) {
		t.Parallel()
		topo, ep := assembleOne(t, Spec{StableKey: StringKey("sw"), DeviceType: onOffSwitch, FriendlyName: "sw", ClientClusters: []uint32{identify, onOff}, Source: switchSource()})
		if _, err := NewDeviceTypeValidator(DeviceTypeValidationOff).Validate(topo); err != nil {
			t.Fatal(err)
		}
		if bindingOf(ep) == nil {
			t.Fatal("no Binding server")
		}
	})
}
