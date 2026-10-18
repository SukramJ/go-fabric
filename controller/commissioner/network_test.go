// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// The network steps against a scripted Wi-Fi or Thread device — the
// behaviour of matter.js ControllerCommissioningFlow.ts:1527-1998 at
// 0ceb202d, and the device expectations of
// packages/node/test/node/ScanNetworksRejectionCommissioningTest.ts
// (a rejected scan is a hint, not a failure; a failed add is).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// netValue is an attribute value of the scripted network device.
type netValue struct {
	cluster, attr uint32
	value         spec.Encodable
}

// networksValue encodes a Networks list.
type networksValue []netcomm.NetworkInfoStruct

func (v networksValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartArray(tag)
	for _, n := range v {
		n.EncodeTLV(e, tlv.AnonymousTag())
	}
	_ = e.EndContainer()
}

// reportOfMany is a ReportData with several root attributes.
func reportOfMany(vs ...netValue) []byte {
	return encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		for _, v := range vs {
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(0), 1)
			im.ConcreteAttributePath{Cluster: v.cluster, Attribute: v.attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
			v.value.EncodeTLV(e, tlv.ContextTag(2))
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		_ = e.EndContainer()
	})
}

func invokeStatusOf(path im.ConcreteCommandPath, st im.StatusCode) []byte {
	path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
	return encodeTLV(func(e *tlv.Encoder) {
		im.InvokeResponse{Responses: []im.InvokeResponseEntry{{Path: path, IsStatus: true, Status: im.StatusIB{Status: st}}}}.MarshalTLV(e, nil)
	})
}

// netDevice is a scripted device with one NetworkCommissioning instance.
type netDevice struct {
	mu        sync.Mutex
	networks  networksValue
	scan      func(netcomm.ScanNetworksRequest) ([]byte, bool) // nil: Success, no results
	addStatus netcomm.NetworkCommissioningStatusEnum
	noIndex   bool
	connect   netcomm.NetworkCommissioningStatusEnum
	connected bool // a network is connected once added
	calls     []string
	crumbs    []uint64
	ssid      []byte
	creds     []byte
	dataset   []byte
	arms      []uint16
}

func (d *netDevice) record(call string, bc *uint64) {
	d.calls = append(d.calls, call)
	if bc != nil {
		d.crumbs = append(d.crumbs, *bc)
	}
}

func (d *netDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch req.opcode {
	case im.OpcodeReadRequest:
		return im.OpcodeReportData, reportOfMany(
			netValue{gencomm.ClusterID, gencomm.AttrBasicCommissioningInfo, gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}},
			netValue{netcomm.ClusterID, netcomm.AttrScanMaxTimeSeconds, uintValue(20)},
			netValue{netcomm.ClusterID, netcomm.AttrConnectMaxTimeSeconds, uintValue(90)},
			netValue{netcomm.ClusterID, netcomm.AttrNetworks, d.networks},
		), true
	case im.OpcodeInvokeRequest:
		path, fields, err := decodeInvoke(req.payload)
		if err != nil {
			return 0, nil, false
		}
		switch {
		case path.Cluster == gencomm.ClusterID && path.Command == gencomm.CmdArmFailSafe:
			var r gencomm.ArmFailSafeRequest
			_ = r.DecodeTLV(fields)
			d.arms = append(d.arms, r.ExpiryLengthSeconds)
			d.record("arm", &r.Breadcrumb)
			path.Command = gencomm.CmdArmFailSafeResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, gencomm.ArmFailSafeResponse{}), true
		case path.Command == netcomm.CmdScanNetworks:
			var r netcomm.ScanNetworksRequest
			_ = r.DecodeTLV(fields)
			d.record("scan", r.Breadcrumb)
			if d.scan != nil {
				if p, ok := d.scan(r); ok {
					return im.OpcodeInvokeResponse, p, true
				}
				return im.OpcodeInvokeResponse, invokeStatusOf(path, im.StatusFailure), true
			}
			path.Command = netcomm.CmdScanNetworksResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, netcomm.ScanNetworksResponse{}), true
		case path.Command == netcomm.CmdAddOrUpdateWiFiNetwork, path.Command == netcomm.CmdAddOrUpdateThreadNetwork:
			var id []byte
			if path.Command == netcomm.CmdAddOrUpdateWiFiNetwork {
				var r netcomm.AddOrUpdateWiFiNetworkRequest
				_ = r.DecodeTLV(fields)
				d.record("addWiFi", r.Breadcrumb)
				d.ssid, d.creds, id = r.Ssid, r.Credentials, r.Ssid
			} else {
				var r netcomm.AddOrUpdateThreadNetworkRequest
				_ = r.DecodeTLV(fields)
				d.record("addThread", r.Breadcrumb)
				d.dataset, id = r.OperationalDataset, []byte{1, 2, 3, 4, 5, 6, 7, 8}
			}
			resp := netcomm.NetworkConfigResponse{NetworkingStatus: d.addStatus}
			if d.addStatus == netcomm.NetworkCommissioningStatusSuccess {
				d.networks = networksValue{{NetworkId: id, Connected: d.connected}}
				if !d.noIndex {
					idx := uint8(0)
					resp.NetworkIndex = &idx
				}
			}
			path.Command = netcomm.CmdNetworkConfigResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, resp), true
		case path.Command == netcomm.CmdConnectNetwork:
			var r netcomm.ConnectNetworkRequest
			_ = r.DecodeTLV(fields)
			d.record("connect", r.Breadcrumb)
			path.Command = netcomm.CmdConnectNetworkResponse
			dbg := "no such network"
			resp := netcomm.ConnectNetworkResponse{NetworkingStatus: d.connect, ErrorValue: spec.Nullable[int32]{Null: true}}
			if d.connect != netcomm.NetworkCommissioningStatusSuccess {
				resp.DebugText = &dbg
			}
			return im.OpcodeInvokeResponse, invokeResponseOf(path, resp), true
		}
	}
	return 0, nil, false
}

func withoutArms(calls []string) []string {
	var out []string
	for _, c := range calls {
		if c != "arm" {
			out = append(out, c)
		}
	}
	return out
}

func (d *netDevice) callsSoFar() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// netFlow is a flow over d with the root's NetworkCommissioning features
// and Networks collected and the failsafe armed.
func netFlow(t *testing.T, d *netDevice, features netcomm.Feature, opts func(*Options)) *flow {
	t.Helper()
	f, _ := newTestFlow(t, d.handle)
	if opts != nil {
		opts(&f.opts)
	}
	f.data.networkFeatures = []networkFeature{{endpoint: 0, featureMap: uint32(features)}}
	f.data.networkStatus = []networkStatus{{endpoint: 0, networks: d.networks}}
	if _, err := f.armFailsafe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNetworkWiFiConfiguresAndConnects(t *testing.T) {
	t.Parallel()
	d := &netDevice{scan: func(r netcomm.ScanNetworksRequest) ([]byte, bool) {
		path := im.ConcreteCommandPath{Cluster: netcomm.ClusterID, Command: netcomm.CmdScanNetworksResponse}
		res := []netcomm.WiFiInterfaceScanResultStruct{{Ssid: r.Ssid.Value, Bssid: make([]byte, 6), Channel: 6}}
		return invokeResponseOf(path, netcomm.ScanNetworksResponse{WiFiScanResults: &res}), true
	}}
	f := netFlow(t, d, netcomm.FeatureWiFiNetworkInterface, func(o *Options) {
		o.WiFiNetwork = &WiFiNetwork{SSID: "lab", Credentials: "secret"}
	})
	if r, err := f.configureNetworkWifi(context.Background()); r != stepSuccess || err != nil {
		t.Fatalf("configureNetworkWifi = %v, %v", r, err)
	}
	if !f.data.successfullyConnected {
		t.Fatal("not connected")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if string(d.ssid) != "lab" || string(d.creds) != "secret" {
		t.Fatalf("ssid %q credentials %q", d.ssid, d.creds)
	}
	// The scan, the add and the connect each take the next breadcrumb
	// (`this.lastBreadcrumb++`, :1639, :1681, :1063).
	var ops []string
	var crumbs []uint64
	for i, c := range d.calls {
		if c != "arm" {
			ops = append(ops, c)
			crumbs = append(crumbs, d.crumbs[i])
		}
	}
	if fmt.Sprint(ops) != "[scan addWiFi connect]" || fmt.Sprint(crumbs) != "[1 2 3]" {
		t.Fatalf("calls %v breadcrumbs %v", ops, crumbs)
	}
	// The connect and CASE reconnect window was armed: 90 s + 5 min on a
	// concurrent IP device is only the connect time (:963-975), so the
	// default failsafe sufficed.
	if f.connectNetworkFailsafeTime(90*time.Second) != 90*time.Second {
		t.Fatal("IP connect failsafe")
	}
}

func TestNetworkWiFiScanRejectedThenAddFails(t *testing.T) {
	t.Parallel()
	d := &netDevice{
		scan:      func(netcomm.ScanNetworksRequest) ([]byte, bool) { return nil, false },
		addStatus: netcomm.NetworkCommissioningStatusOutOfRange,
	}
	f := netFlow(t, d, netcomm.FeatureWiFiNetworkInterface, func(o *Options) {
		o.WiFiNetwork = &WiFiNetwork{SSID: "lab", Credentials: "secret"}
	})
	_, err := f.configureNetworkWifi(context.Background())
	if !errors.Is(err, ErrWifiNetworkSetupFailed) {
		t.Fatalf("err %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, `Commissionee failed to add WiFi network "lab"`) || !strings.Contains(msg, "(scan failed: ") || !strings.Contains(msg, " - verify network name and availability)") {
		t.Fatalf("message %q", msg)
	}
	if got := fmt.Sprint(withoutArms(d.callsSoFar())); got != "[scan addWiFi]" {
		t.Fatalf("calls %s", got)
	}
}

func TestNetworkWiFiConnectFailsAndMissingIndex(t *testing.T) {
	t.Parallel()
	d := &netDevice{connect: netcomm.NetworkCommissioningStatusNetworkNotFound}
	f := netFlow(t, d, netcomm.FeatureWiFiNetworkInterface, func(o *Options) {
		o.WiFiNetwork = &WiFiNetwork{SSID: "lab", Credentials: "secret"}
	})
	_, err := f.configureNetworkWifi(context.Background())
	if !errors.Is(err, ErrWifiNetworkSetupFailed) || !strings.Contains(err.Error(), `Commissionee failed to connect to WiFi network "lab": no such network (network not found in scan results - verify network name and availability)`) {
		t.Fatalf("err %v", err)
	}
	d2 := &netDevice{noIndex: true}
	f2 := netFlow(t, d2, netcomm.FeatureWiFiNetworkInterface, func(o *Options) {
		o.WiFiNetwork = &WiFiNetwork{SSID: "lab", Credentials: "secret"}
	})
	no := false
	f2.data.supportsConcurrentConnection = &no
	if _, err := f2.configureNetworkWifi(context.Background()); !errors.Is(err, ErrWifiNetworkSetupFailed) || !strings.Contains(err.Error(), "did not return network index") {
		t.Fatalf("missing index: %v", err)
	}
	// A non-concurrent device is not scanned (:1637).
	if got := fmt.Sprint(d2.callsSoFar()); strings.Contains(got, "scan") {
		t.Fatalf("calls %s", got)
	}
}

func TestNetworkWiFiSkips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &netDevice{}
	f := netFlow(t, d, netcomm.FeatureEthernetNetworkInterface, func(o *Options) { o.WiFiNetwork = &WiFiNetwork{SSID: "x", Credentials: "y"} })
	if r, err := f.configureNetworkWifi(ctx); r != stepSkipped || err != nil {
		t.Fatalf("no Wi-Fi interface: %v %v", r, err)
	}
	d2 := &netDevice{networks: networksValue{{NetworkId: []byte("lab"), Connected: true}}}
	f2 := netFlow(t, d2, netcomm.FeatureWiFiNetworkInterface, func(o *Options) { o.WiFiNetwork = &WiFiNetwork{SSID: "x", Credentials: "y"} })
	if r, err := f2.configureNetworkWifi(ctx); r != stepSkipped || err != nil || !f2.data.successfullyConnected {
		t.Fatalf("already connected: %v %v", r, err)
	}
	// Thread is skipped once Wi-Fi connected (:1759-1765).
	f2.opts.ThreadNetwork = &ThreadNetwork{OperationalDataset: []byte{0x03, 0x01, 'x'}}
	if r, err := f2.configureNetworkThread(ctx); r != stepSkipped || err != nil {
		t.Fatalf("thread after Wi-Fi: %v %v", r, err)
	}
	f3 := netFlow(t, &netDevice{}, netcomm.FeatureWiFiNetworkInterface, nil)
	if r, _ := f3.configureNetworkWifi(ctx); r != stepSkipped {
		t.Fatal("no credentials")
	}
	if r, _ := f3.configureNetworkThread(ctx); r != stepSkipped {
		t.Fatal("no dataset")
	}
}

// threadDataset is a MeshCoP dataset with a channel and a network name.
func threadDataset(name string) []byte {
	b := make([]byte, 0, 7+len(name))
	b = append(b, 0x00, 0x03, 0x00, 0x00, 0x0f) // channel 15
	return append(append(b, meshCopNetworkName, byte(len(name))), name...)
}

func TestNetworkThreadConfiguresAndConnects(t *testing.T) {
	t.Parallel()
	d := &netDevice{scan: func(netcomm.ScanNetworksRequest) ([]byte, bool) {
		path := im.ConcreteCommandPath{Cluster: netcomm.ClusterID, Command: netcomm.CmdScanNetworksResponse}
		res := []netcomm.ThreadInterfaceScanResultStruct{{NetworkName: "OtherNet", ExtendedAddress: make([]byte, 8)}}
		return invokeResponseOf(path, netcomm.ScanNetworksResponse{ThreadScanResults: &res}), true
	}}
	ds := threadDataset("MyHome")
	f := netFlow(t, d, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{OperationalDataset: ds}
	})
	if r, err := f.configureNetworkThread(context.Background()); r != stepSuccess || err != nil {
		t.Fatalf("configureNetworkThread = %v, %v", r, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !bytes.Equal(d.dataset, ds) {
		t.Fatalf("dataset %x", d.dataset)
	}
	if got := fmt.Sprint(withoutArms(d.calls)); got != "[scan addThread connect]" {
		t.Fatalf("calls %s", got)
	}
}

func TestNetworkThreadFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// A supplied name that contradicts the dataset's (:276-282).
	f := netFlow(t, &netDevice{}, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{NetworkName: "Other", OperationalDataset: threadDataset("MyHome")}
	})
	if _, err := f.configureNetworkThread(ctx); !errors.Is(err, ErrThreadNetworkNameMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	// No name: no scan; the add refused.
	d := &netDevice{addStatus: netcomm.NetworkCommissioningStatusBoundsExceeded}
	f2 := netFlow(t, d, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{OperationalDataset: []byte{0x00, 0x03, 0x00, 0x00, 0x0f}}
	})
	_, err := f2.configureNetworkThread(ctx)
	if !errors.Is(err, ErrThreadNetworkSetupFailed) || !strings.Contains(err.Error(), "Commissionee failed to add Thread network: status=BoundsExceeded (2)") {
		t.Fatalf("add refused: %v", err)
	}
	if got := fmt.Sprint(d.callsSoFar()); strings.Contains(got, "scan") {
		t.Fatalf("scanned without a name: %s", got)
	}
	// The connect refused.
	d3 := &netDevice{connect: netcomm.NetworkCommissioningStatusAuthFailure}
	f3 := netFlow(t, d3, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{OperationalDataset: threadDataset("MyHome")}
	})
	if _, err := f3.configureNetworkThread(ctx); !errors.Is(err, ErrThreadNetworkSetupFailed) || !strings.Contains(err.Error(), "failed to connect to Thread network: status=AuthFailure (7), no such network (no Thread networks found in scan results") {
		t.Fatalf("connect refused: %v", err)
	}
	// Already connected after the add.
	d4 := &netDevice{connected: true}
	f4 := netFlow(t, d4, netcomm.FeatureThreadNetworkInterface, func(o *Options) {
		o.ThreadNetwork = &ThreadNetwork{OperationalDataset: threadDataset("MyHome")}
	})
	if r, err := f4.configureNetworkThread(ctx); r != stepSuccess || err != nil || strings.Contains(fmt.Sprint(d4.callsSoFar()), "connect") {
		t.Fatalf("connected on add: %v %v %v", r, err, d4.callsSoFar())
	}
}

func TestResolveThreadNetworkName(t *testing.T) {
	t.Parallel()
	log := discard()
	for _, c := range []struct {
		given   string
		dataset []byte
		want    string
		err     error
	}{
		{"", threadDataset("MyHome"), "MyHome", nil},
		{"MyHome", threadDataset("MyHome"), "MyHome", nil},
		{"Other", threadDataset("MyHome"), "", ErrThreadNetworkNameMismatch},
		{"Given", []byte{0x03, 0x05, 'a'}, "Given", nil},          // undecodable: the given name
		{"", []byte{0x00}, "", nil},                               // truncated header
		{"", []byte{0x03, 0xff, 0x00}, "", nil},                   // truncated extended length
		{"", []byte{0x03, 0xff, 0x00, 0x02, 'h', 'i'}, "hi", nil}, // extended length
		{"Given", []byte{0x00, 0x01, 0x0b}, "Given", nil},         // no name TLV
	} {
		got, err := resolveThreadNetworkName(&ThreadNetwork{NetworkName: c.given, OperationalDataset: c.dataset}, log)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("resolve(%q, %x) = %q, %v; want %q, %v", c.given, c.dataset, got, err, c.want, c.err)
		}
	}
}

func TestValidateNetwork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, _ := newTestFlow(t, (&netDevice{}).handle)
	if _, err := f.validateNetwork(ctx); !errors.Is(err, ErrCommissioning) {
		t.Fatalf("uncollected: %v", err)
	}
	f.data.networkFeatures = []networkFeature{{featureMap: uint32(netcomm.FeatureWiFiNetworkInterface)}}
	f.data.networkStatus = []networkStatus{{networks: []netcomm.NetworkInfoStruct{{NetworkId: []byte("x")}}}}
	if _, err := f.validateNetwork(ctx); !errors.Is(err, ErrWifiOrThreadNetworkCredentialsNotConfigured) {
		t.Fatalf("Wi-Fi device without credentials: %v", err)
	}
	f.opts.WiFiNetwork = &WiFiNetwork{SSID: "lab", Credentials: "secret"}
	if r, err := f.validateNetwork(ctx); r != stepSuccess || err != nil {
		t.Fatalf("with credentials: %v", err)
	}
	f.opts.WiFiNetwork = nil
	f.data.networkStatus[0].networks[0].Connected = true
	if _, err := f.validateNetwork(ctx); err != nil {
		t.Fatalf("connected: %v", err)
	}
	f.data.networkStatus = nil
	f.data.networkFeatures = []networkFeature{{featureMap: uint32(netcomm.FeatureEthernetNetworkInterface)}}
	f.data.networkStatus = []networkStatus{}
	if _, err := f.validateNetwork(ctx); err != nil {
		t.Fatalf("Ethernet: %v", err)
	}
}

func TestNetworkStepsRegistration(t *testing.T) {
	t.Parallel()
	names := func(f *flow) string {
		var out []string
		for _, s := range f.steps {
			if s.number == 16 {
				out = append(out, fmt.Sprintf("%d.%d %s rearm=%v", s.number, s.sub, s.name, f.reArmBefore[[2]int{s.number, s.sub}]))
			}
		}
		return strings.Join(out, "; ")
	}
	f, _ := newTestFlow(t, (&netDevice{}).handle)
	if got := names(f); got != "" {
		t.Fatalf("IP without credentials: %s", got)
	}
	f.opts.WiFiNetwork = &WiFiNetwork{SSID: "a", Credentials: "b"}
	f.opts.ThreadNetwork = &ThreadNetwork{OperationalDataset: []byte{1}}
	f.steps, f.reArmBefore = nil, map[[2]int]bool{}
	f.initializeSteps()
	if got := names(f); got != "16.1 NetworkCommissioning.Validate rearm=false; 16.2 NetworkCommissioning.Wifi rearm=true; 16.3 NetworkCommissioning.Thread rearm=true" {
		t.Fatalf("with credentials: %s", got)
	}
	f.opts.WiFiNetwork, f.opts.ThreadNetwork = nil, nil
	f.ble = true
	f.steps = nil
	f.initializeSteps()
	if got := names(f); got != "16.1 NetworkCommissioning.Validate rearm=false" {
		t.Fatalf("BLE: %s", got)
	}
}

func TestChannelDropFallback(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, (&netDevice{}).handle)
	for _, err := range []error{
		fmt.Errorf("wrapped: %w", btp.ErrDisconnected),
		&exchange.PeerUnresponsiveError{},
		&exchange.PeerMessageMissingError{Timeout: time.Second},
	} {
		if !isChannelDrop(err) {
			t.Errorf("%v is no channel drop", err)
		}
	}
	if isChannelDrop(context.Canceled) {
		t.Error("a cancel is a channel drop")
	}
	_, dropped, err := f.withChannelDropFallback(func() (netcomm.NetworkInfoStruct, error) { return netcomm.NetworkInfoStruct{}, btp.ErrDisconnected })
	if !dropped || err != nil || f.data.supportsConcurrentConnection == nil || *f.data.supportsConcurrentConnection {
		t.Fatalf("drop: %v %v", dropped, err)
	}
	if _, dropped, err := f.withChannelDropFallbackConnect(func() (netcomm.ConnectNetworkResponse, error) {
		return netcomm.ConnectNetworkResponse{}, errors.New("spec")
	}); dropped || err == nil {
		t.Fatal("a non-transport error was swallowed")
	}
	// Non-concurrent: the connect failsafe covers the CASE reconnect too.
	if got := f.connectNetworkFailsafeTime(time.Minute); got != time.Minute+CASEReconnectTimeout {
		t.Fatalf("connectNetworkFailsafeTime = %v", got)
	}
}

// TestFlowRearmsBeforeNetworkStep: a step with reArmFailsafe re-arms
// first unless the previous step's bookkeeping just did (:385-393).
func TestFlowRearmsBeforeNetworkStep(t *testing.T) {
	t.Parallel()
	d := &netDevice{}
	f, _ := newTestFlow(t, d.handle)
	ran := 0
	f.steps = []step{
		{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		{16, 2, "NetworkCommissioning.Wifi", func(context.Context) (stepResult, error) { ran++; return stepSuccess, nil }},
	}
	f.reArmBefore = map[[2]int]bool{{16, 2}: true}
	if err := f.execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(d.callsSoFar()); got != "[arm arm]" || ran != 1 {
		t.Fatalf("calls %s, ran %d", got, ran)
	}
}

func TestBLERearmInterval(t *testing.T) {
	t.Parallel()
	d := &netDevice{}
	f, clock := newTestFlow(t, d.handle)
	f.ble = true
	ctx := context.Background()
	if _, err := f.armFailsafe(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.ensureFailsafeTimerFor(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	if !f.armIntervalRunning() {
		t.Fatal("no re-arm timer on BLE")
	}
	before := len(d.callsSoFar())
	clock.Advance(BTPIdleAliveInterval)
	eventually(t, "a periodic ArmFailSafe", func() bool { return len(d.callsSoFar()) > before })
	f.stopArmInterval()
	if f.armIntervalRunning() {
		t.Fatal("re-arm timer still running")
	}
	// Past the cumulative failsafe the timer stops on its own (:930-935).
	f.startArmInterval(ctx)
	clock.Advance(16 * time.Minute)
	eventually(t, "the timer stops", func() bool { return !f.armIntervalRunning() })
}
