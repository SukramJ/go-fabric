// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/icd-management/IcdManagementServerTest.ts
// at 862ad75f: the device's ICD Management server on a node configured with
// node.Config.ICD — the attribute defaults, the constraint checks, the
// counter, the MRP pad, subscription persistence, RegisterClient,
// UnregisterClient and StayActiveRequest over the wire, the Manage-privilege
// VerificationKey rule, the LIT feature, the fabric lifecycle, the ICD
// advertisement, the User Active Mode Trigger, the dynamic SIT/LIT switch,
// the idle/active mode and the Check-In pass.
//
// matter.js's MockSite pair is a commissioned fabrictest pair; its
// MockServerNode with a MockExchange of a given access level is the pair
// after Pair.Grant; `device.stateOf(IcdManagementServer)` is a read over the
// wire or the root's server (node.RootClusters.ICDManagement), its
// `fabric.icd` the server's Registrations and its `internal.icdKeys` the
// store's ICD area. matter.js's server variants (`.with(CIP, LITS, UAT,
// DSLS)`) are the facade's ICDConfig: SIT is CIP (with UAT when a trigger is
// stated), LIT is CIP, UAT and LITS, DynamicSITLIT adds DSLS. A combination
// the facade cannot state — a LIT server without its OperatingMode, a LITS
// server without CIP — is checked on the cluster/core server's own
// configuration. The recording Check-In sender replaces the node's as the
// test's RecordingIcdServer.createCheckInSender does, over the real
// icd.Sender. MockTime.advance is the site's Step.
package node_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

var (
	icdKeyA = mustHexBytes("d0d1d2d3d4d5d6d7d8d9dadbdcdddedf")
	icdKeyB = mustHexBytes("a0a1a2a3a4a5a6a7a8a9aaabacadaeaf")
	icdKeyC = mustHexBytes("e0e1e2e3e4e5e6e7e8e9eaebecedeeef")
	icdKeyF = mustHexBytes("f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff")
)

func mustHexBytes(s string) []byte {
	out := make([]byte, len(s)/2)
	for i := range out {
		var b byte
		for _, c := range s[2*i : 2*i+2] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			default:
				b |= byte(c-'a') + 10
			}
		}
		out[i] = b
	}
	return out
}

// icdRecorder records the Check-Ins the real sender encoded and "sent"
// (IcdManagementServerTest.ts:35-63, recordingIcdServer).
type icdRecorder struct {
	mu   sync.Mutex
	sent []icd.CheckInRequest
}

func (r *icdRecorder) install(n *node.Node) {
	s := icd.NewSender(icd.SenderContext{
		ResolveAddress: func(context.Context, uint8, uint64) (*net.UDPAddr, error) {
			return &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, nil
		},
		SendUnsecured: func(context.Context, *net.UDPAddr, uint8, []byte) (bool, error) { return true, nil },
	})
	n.Root().ICDManagement.SetCheckInSender(func(ctx context.Context, req icd.CheckInRequest) (bool, error) {
		ok, err := s.Send(ctx, req)
		if ok {
			r.mu.Lock()
			r.sent = append(r.sent, req)
			r.mu.Unlock()
		}
		return ok, err
	})
}

func (r *icdRecorder) snapshot() []icd.CheckInRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sent)
}

func (r *icdRecorder) reset() {
	r.mu.Lock()
	r.sent = nil
	r.mu.Unlock()
}

// operationalRecorder keeps the operational records the node publishes, on
// a no-op advertiser the site answers DNS-SD from (RecordingAdvertiser,
// IcdManagementServerTest.ts:69-80).
type operationalRecorder struct {
	*mdns.Noop
	mu        sync.Mutex
	published []mdns.Service
}

func newOperationalRecorder() *operationalRecorder {
	return &operationalRecorder{Noop: mdns.NewNoop()}
}

func (r *operationalRecorder) Publish(ctx context.Context, svc mdns.Service) error {
	if svc.ServiceType == mdns.ServiceTypeOperational {
		r.mu.Lock()
		r.published = append(r.published, svc)
		r.mu.Unlock()
	}
	return r.Noop.Publish(ctx, svc)
}

func (r *operationalRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.published)
}

// last is the TXT of the last operational record published.
func (r *operationalRecorder) last(t *testing.T) map[string]string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.published) == 0 {
		t.Fatal("no operational record published")
	}
	out := map[string]string{}
	for _, txt := range r.published[len(r.published)-1].TXT {
		out[txt.Key] = txt.Value
	}
	return out
}

type icdSetup struct {
	cfg      node.ICDConfig
	recorder *icdRecorder
	advert   *operationalRecorder
	prepare  func(*node.Node)
}

// icdPair is a commissioned pair whose device is an ICD.
func icdPair(t *testing.T, s icdSetup) (*fabrictest.Site, *fabrictest.Pair) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	cfg := s.cfg
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(c *node.Config) {
			c.ICD = &cfg
			if s.advert != nil {
				c.Advertiser = s.advert
			}
		},
		Prepare: func(n *node.Node) {
			if s.recorder != nil {
				s.recorder.install(n)
			}
			if s.prepare != nil {
				s.prepare(n)
			}
		},
	}})
	return site, pair
}

// icdDevice is an uncommissioned started ICD device of a site of its own.
func icdDevice(t *testing.T, cfg node.ICDConfig, prepare func(*node.Node)) (*fabrictest.Site, *fabrictest.Device) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	d := site.AddDevice(fabrictest.Topology{
		Configure: func(c *node.Config) { c.ICD = &cfg },
		Prepare:   prepare,
	})
	return site, d
}

func icdServer(p *fabrictest.Pair) *mattercore.ICDManagement {
	return p.Device.Node().Root().ICDManagement
}

func icdCommand(cmd uint32) fabrictest.CommandPath {
	return client.CommandAt(0, icddef.ClusterID, cmd)
}

func registerClient(ctx context.Context, p *fabrictest.Pair, req icddef.RegisterClientRequest) (icddef.RegisterClientResponse, error) {
	res, err := fabrictest.Invoke(ctx, p, icdCommand(icddef.CmdRegisterClient), req)
	if err != nil {
		return icddef.RegisterClientResponse{}, err
	}
	var resp icddef.RegisterClientResponse
	return resp, resp.DecodeTLV(res.Fields)
}

func unregisterClient(ctx context.Context, p *fabrictest.Pair, req icddef.UnregisterClientRequest) error {
	_, err := fabrictest.Invoke(ctx, p, icdCommand(icddef.CmdUnregisterClient), req)
	return err
}

func stayActive(ctx context.Context, t *testing.T, p *fabrictest.Pair, ms uint32) uint32 {
	t.Helper()
	res, err := fabrictest.Invoke(ctx, p, icdCommand(icddef.CmdStayActiveRequest), icddef.StayActiveRequestRequest{StayActiveDuration: ms})
	if err != nil {
		t.Fatal(err)
	}
	var resp icddef.StayActiveResponse
	if err := resp.DecodeTLV(res.Fields); err != nil {
		t.Fatal(err)
	}
	return resp.PromisedActiveDuration
}

func permanent(node uint64, key []byte) icddef.RegisterClientRequest {
	return icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: key, ClientType: icddef.ClientTypePermanent}
}

func icdState(ctx context.Context, t *testing.T, p *fabrictest.Pair) icddef.State {
	t.Helper()
	values, err := fabrictest.ReadCluster(ctx, p, 0, icddef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := icddef.StateFrom(values)
	return st
}

func registeredClients(ctx context.Context, t *testing.T, p *fabrictest.Pair) []icddef.MonitoringRegistrationStruct {
	t.Helper()
	data, err := fabrictest.Read(ctx, p, client.AttributeAt(0, icddef.ClusterID, icddef.AttrRegisteredClients))
	if err != nil {
		t.Fatal(err)
	}
	regs, err := spec.DecodeList(func(n spec.Node) (icddef.MonitoringRegistrationStruct, error) {
		var r icddef.MonitoringRegistrationStruct
		err := r.DecodeTLV(n)
		return r, err
	})(data.Value)
	if err != nil {
		t.Fatal(err)
	}
	return regs
}

func pairFabric(ctx context.Context, t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	fabrics, err := p.Device.Node().FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v %v", fabrics, err)
	}
	return fabrics[0].FabricIndex
}

func wantStatus(t *testing.T, err error, want im.StatusCode) {
	t.Helper()
	var se *fabrictest.StatusError
	if !errors.As(err, &se) || se.Status != want {
		t.Fatalf("error %v, want status %v", err, want)
	}
}

// The configurations of the test (IcdManagementServerTest.ts:696-702,
// :856-865, :1114-1121, :1214-1220).
var (
	uatHint   = &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerPowerCycle}
	litConfig = node.ICDConfig{
		Mode: node.ICDModeLIT, ActiveModeThreshold: 5000 * time.Millisecond, IdleModeDuration: 3600 * time.Second,
		ActiveModeDuration: time.Second, MaximumCheckInBackoff: 3600 * time.Second, UserActiveModeTrigger: uatHint,
	}
	idleActiveConfig = node.ICDConfig{
		IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
		ActiveModeDuration: 2000 * time.Millisecond, ActiveModeThreshold: 1000 * time.Millisecond,
	}
	checkInConfig = node.ICDConfig{
		IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
		ActiveModeDuration: 2000 * time.Millisecond, ActiveModeThreshold: 1000 * time.Millisecond,
		ClientsSupportedPerFabric: 2,
	}
)

// icdConfigInvalid reports a minimal node configuration with ICD set
// refused as matter.js refuses the node's creation ("Behaviors have
// errors").
func icdConfigInvalid(t *testing.T, c node.ICDConfig) bool {
	t.Helper()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.ICD = &c
	err := cfg.Validate()
	if err != nil && !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("Validate = %v, not ErrInvalidConfig", err)
	}
	if err != nil {
		if _, nerr := node.New(cfg); nerr == nil {
			t.Fatal("New accepted what Validate refused")
		}
	}
	return err != nil
}

func TestIcdManagementServer(t *testing.T) {
	t.Parallel()

	t.Run("attribute defaults and CIP feature", func(t *testing.T) {
		t.Parallel()
		t.Run("installs with CIP and exposes correct attribute values when configured", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: node.ICDConfig{
				IdleModeDuration: 3600 * time.Second, ActiveModeDuration: 1000 * time.Millisecond,
				ActiveModeThreshold: 500 * time.Millisecond, ClientsSupportedPerFabric: 2, MaximumCheckInBackoff: 3600 * time.Second,
			}})
			st := icdState(t.Context(), t, pair)
			if st.IdleModeDuration != 3600 || st.ActiveModeDuration != 1000 || st.ActiveModeThreshold != 500 ||
				st.ClientsSupportedPerFabric != 2 || st.MaximumCheckInBackoff != 3600 {
				t.Fatalf("state %+v", st)
			}
			if got := registeredClients(t.Context(), t, pair); len(got) != 0 {
				t.Fatalf("registered %+v", got)
			}
		})
		t.Run("accepts spec default attribute values without config override", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			st := icdState(t.Context(), t, pair)
			if st.IdleModeDuration != 1 || st.ActiveModeDuration != 300 || st.MaximumCheckInBackoff != 1 {
				t.Fatalf("state %+v", st)
			}
		})
	})

	t.Run("constraint validation", func(t *testing.T) {
		t.Parallel()
		t.Run("rejects when idleModeDuration * 1000 < activeModeDuration", func(t *testing.T) {
			t.Parallel()
			if !icdConfigInvalid(t, node.ICDConfig{IdleModeDuration: time.Second, ActiveModeDuration: 5000 * time.Millisecond}) {
				t.Fatal("accepted")
			}
		})
		t.Run("rejects when maximumCheckInBackoff < idleModeDuration", func(t *testing.T) {
			t.Parallel()
			if !icdConfigInvalid(t, node.ICDConfig{IdleModeDuration: 3600 * time.Second, ActiveModeDuration: 300 * time.Millisecond, MaximumCheckInBackoff: 1800 * time.Second}) {
				t.Fatal("accepted")
			}
		})
	})

	t.Run("ICD counter", func(t *testing.T) {
		t.Parallel()
		t.Run("applies boot bump to icdCounter attribute on node online", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if st := icdState(t.Context(), t, pair); st.IcdCounter == 0 {
				t.Fatalf("counter %d", st.IcdCounter)
			}
		})
		t.Run("persists icdCounter attribute when the internal counter is incremented", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			before := icdState(t.Context(), t, pair).IcdCounter
			icdServer(pair).Counter().Increment()
			if after := icdState(t.Context(), t, pair).IcdCounter; after != before+1 {
				t.Fatalf("counter %d after %d", after, before)
			}
			if v, ok, err := pair.Device.Store().LoadICDCounter(t.Context()); err != nil || !ok || v != before+1 {
				t.Fatalf("persisted %d %v %v", v, ok, err)
			}
		})
	})

	t.Run("MRP fast-poll backoff", func(t *testing.T) {
		t.Parallel()
		t.Run("pads localFixedMrpBackoff by the ICD fast-poll interval while operating as an ICD", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if got := pair.Device.Bridge().LocalFixedMRPBackoff(); got != 200*time.Millisecond {
				t.Fatalf("pad %v", got)
			}
		})
	})

	t.Run("subscription persistence", func(t *testing.T) {
		t.Parallel()
		t.Run("disables subscription persistence on an ICD node", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if pair.Device.Bridge().SubscriptionPersistenceEnabled() {
				t.Fatal("persistence enabled")
			}
		})
	})

	t.Run("RegisterClient command", func(t *testing.T) {
		t.Parallel()
		t.Run("registers a client and returns the ICD counter", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			ctx := t.Context()
			node := pair.Address.NodeID
			resp, err := registerClient(ctx, pair, permanent(node, icdKeyA))
			if err != nil {
				t.Fatal(err)
			}
			if resp.IcdCounter != icdServer(pair).Counter().Value() {
				t.Fatalf("response counter %d", resp.IcdCounter)
			}
			got := registeredClients(ctx, t, pair)
			if len(got) != 1 || got[0].CheckInNodeId != node || got[0].ClientType != icddef.ClientTypePermanent || got[0].Key != nil {
				t.Fatalf("registered %+v", got)
			}
			regs := icdServer(pair).Registrations(pairFabric(ctx, t, pair))
			if len(regs) != 1 || !bytes.Equal(regs[0].Key, icdKeyA) {
				t.Fatalf("fabric registrations %+v", regs)
			}
		})
		t.Run("rejects a second checkInNodeId when fabric slots are exhausted", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: node.ICDConfig{ClientsSupportedPerFabric: 1}})
			ctx := t.Context()
			if _, err := registerClient(ctx, pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			_, err := registerClient(ctx, pair, permanent(pair.Address.NodeID+1, icdKeyC))
			wantStatus(t, err, im.StatusResourceExhausted)
		})
		t.Run("updates an existing entry as Administrator without requiring verificationKey", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			ctx := t.Context()
			node := pair.Address.NodeID
			if _, err := registerClient(ctx, pair, permanent(node, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			if _, err := registerClient(ctx, pair, icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: icdKeyB, ClientType: icddef.ClientTypeEphemeral}); err != nil {
				t.Fatal(err)
			}
			got := registeredClients(ctx, t, pair)
			if len(got) != 1 || got[0].ClientType != icddef.ClientTypeEphemeral {
				t.Fatalf("registered %+v", got)
			}
			regs := icdServer(pair).Registrations(pairFabric(ctx, t, pair))
			if len(regs) != 1 || !bytes.Equal(regs[0].Key, icdKeyB) {
				t.Fatalf("fabric registrations %+v", regs)
			}
		})
	})

	t.Run("UnregisterClient command", func(t *testing.T) {
		t.Parallel()
		t.Run("unregisters a client", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			ctx := t.Context()
			node := pair.Address.NodeID
			if _, err := registerClient(ctx, pair, permanent(node, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			if err := unregisterClient(ctx, pair, icddef.UnregisterClientRequest{CheckInNodeId: node}); err != nil {
				t.Fatal(err)
			}
			if got := registeredClients(ctx, t, pair); len(got) != 0 {
				t.Fatalf("registered %+v", got)
			}
			if regs := icdServer(pair).Registrations(pairFabric(ctx, t, pair)); len(regs) != 0 {
				t.Fatalf("fabric registrations %+v", regs)
			}
			// The persisted ICDToken is dropped too.
			if regs, err := pair.Device.Store().ListICDRegistrations(ctx); err != nil || len(regs) != 0 {
				t.Fatalf("persisted %+v %v", regs, err)
			}
		})
		t.Run("returns NOT_FOUND for unknown client", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			wantStatus(t, unregisterClient(t.Context(), pair, icddef.UnregisterClientRequest{CheckInNodeId: pair.Address.NodeID + 99}), im.StatusNotFound)
		})
	})

	t.Run("StayActiveRequest", func(t *testing.T) {
		t.Parallel()
		t.Run("promises at least the spec floor", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if got := stayActive(t.Context(), t, pair, 5000); got < 5000 {
				t.Fatalf("promised %d", got)
			}
			if got := stayActive(t.Context(), t, pair, 60000); got < 30000 {
				t.Fatalf("promised %d", got)
			}
		})
		t.Run("small request promises at least min(30000, requested)", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if got := stayActive(t.Context(), t, pair, 20); got < 20 {
				t.Fatalf("promised %d", got)
			}
		})
	})

	t.Run("Manage-privilege verificationKey", func(t *testing.T) {
		t.Parallel()
		// withRegisteredNode (IcdManagementServerTest.ts:403-416): the
		// controller registers as Administrator, then holds Manage alone.
		withRegisteredNode := func(t *testing.T) (*fabrictest.Pair, uint64) {
			t.Helper()
			_, pair := icdPair(t, icdSetup{})
			node := uint64(0x0101)
			if _, err := registerClient(t.Context(), pair, permanent(node, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			if err := pair.Grant(t.Context(), store.PrivilegeManage); err != nil {
				t.Fatal(err)
			}
			return pair, node
		}
		vk := func(b []byte) *[]byte { return &b }
		t.Run("registerClient update as Manage with correct verificationKey succeeds and updates the entry", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			ctx := t.Context()
			if _, err := registerClient(ctx, pair, icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: icdKeyB, VerificationKey: vk(icdKeyA), ClientType: icddef.ClientTypeEphemeral}); err != nil {
				t.Fatal(err)
			}
			regs := icdServer(pair).Registrations(pairFabric(ctx, t, pair))
			if len(regs) != 1 || regs[0].ClientType != icd.ClientEphemeral || !bytes.Equal(regs[0].Key, icdKeyB) {
				t.Fatalf("registrations %+v", regs)
			}
		})
		t.Run("registerClient update as Manage with wrong verificationKey rejects with Failure", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			_, err := registerClient(t.Context(), pair, icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: icdKeyB, VerificationKey: vk(icdKeyF), ClientType: icddef.ClientTypeEphemeral})
			wantStatus(t, err, im.StatusFailure)
		})
		t.Run("registerClient update as Manage with missing verificationKey rejects with Failure", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			_, err := registerClient(t.Context(), pair, icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: icdKeyB, ClientType: icddef.ClientTypeEphemeral})
			wantStatus(t, err, im.StatusFailure)
		})
		t.Run("unregisterClient as Manage with correct verificationKey succeeds and removes the entry", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			ctx := t.Context()
			if err := unregisterClient(ctx, pair, icddef.UnregisterClientRequest{CheckInNodeId: node, VerificationKey: vk(icdKeyA)}); err != nil {
				t.Fatal(err)
			}
			if regs := icdServer(pair).Registrations(pairFabric(ctx, t, pair)); len(regs) != 0 {
				t.Fatalf("registrations %+v", regs)
			}
		})
		t.Run("unregisterClient as Manage with wrong verificationKey rejects with Failure", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			wantStatus(t, unregisterClient(t.Context(), pair, icddef.UnregisterClientRequest{CheckInNodeId: node, VerificationKey: vk(icdKeyF)}), im.StatusFailure)
		})
		t.Run("unregisterClient as Manage with missing verificationKey rejects with Failure", func(t *testing.T) {
			t.Parallel()
			pair, node := withRegisteredNode(t)
			wantStatus(t, unregisterClient(t.Context(), pair, icddef.UnregisterClientRequest{CheckInNodeId: node}), im.StatusFailure)
		})
	})

	t.Run("LongIdleTime feature", func(t *testing.T) {
		t.Parallel()
		t.Run("exposes the configured operatingMode", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			if st := icdState(t.Context(), t, pair); st.OperatingMode != icddef.OperatingModeSit {
				t.Fatalf("mode %v", st.OperatingMode)
			}
		})
		t.Run("rejects a LIT device that omits the mandatory operatingMode", func(t *testing.T) {
			t.Parallel()
			// The facade states the mode itself; the server's own configuration
			// refuses one without it.
			_, err := mattercore.NewICDManagement(mattercore.ICDManagementConfig{
				Features:            icddef.FeatureCheckInProtocolSupport | icddef.FeatureLongIdleTimeSupport | icddef.FeatureUserActiveModeTrigger,
				ActiveModeThreshold: 5000,
			})
			if !errors.Is(err, mattercore.ErrICDManagementConfig) {
				t.Fatalf("New = %v", err)
			}
		})
		t.Run("rejects a LIT device with activeModeThreshold < 5000 ms", func(t *testing.T) {
			t.Parallel()
			c := litConfig
			c.ActiveModeThreshold = time.Second
			if !icdConfigInvalid(t, c) {
				t.Fatal("accepted")
			}
		})
		t.Run("accepts a LIT device with activeModeThreshold exactly 5000 ms", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			if st := icdState(t.Context(), t, pair); st.ActiveModeThreshold != 5000 {
				t.Fatalf("threshold %d", st.ActiveModeThreshold)
			}
		})
		t.Run("does not apply the LIT activeModeThreshold constraint to a plain (non-LITS) server", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: node.ICDConfig{ActiveModeThreshold: time.Second}})
			if st := icdState(t.Context(), t, pair); st.ActiveModeThreshold != 1000 {
				t.Fatalf("threshold %d", st.ActiveModeThreshold)
			}
		})
	})

	t.Run("fabric lifecycle", func(t *testing.T) {
		t.Parallel()
		t.Run("restores registrations into fabric.icd after node stop and restart", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			ctx := t.Context()
			node := pair.Address.NodeID
			if _, err := registerClient(ctx, pair, permanent(node, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			fabric := pairFabric(ctx, t, pair)
			if regs := icdServer(pair).Registrations(fabric); len(regs) != 1 {
				t.Fatalf("registrations %+v", regs)
			}
			if err := pair.Device.Restart(ctx); err != nil {
				t.Fatal(err)
			}
			regs := icdServer(pair).Registrations(fabric)
			if len(regs) != 1 || regs[0].CheckInNodeID != node || !bytes.Equal(regs[0].Key, icdKeyA) {
				t.Fatalf("restored %+v", regs)
			}
		})
		t.Run("drops icdKeys when the fabric is removed", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			ctx := t.Context()
			if _, err := registerClient(ctx, pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			if regs, _ := pair.Device.Store().ListICDRegistrations(ctx); len(regs) != 1 {
				t.Fatalf("persisted %+v", regs)
			}
			fabric := pairFabric(ctx, t, pair)
			if err := pair.Device.Node().RemoveFabric(ctx, fabric); err != nil {
				t.Fatal(err)
			}
			if regs, _ := pair.Device.Store().ListICDRegistrations(ctx); len(regs) != 0 {
				t.Fatalf("persisted after removal %+v", regs)
			}
			if regs := icdServer(pair).Registrations(fabric); len(regs) != 0 {
				t.Fatalf("registrations after removal %+v", regs)
			}
		})
		t.Run("prunes persisted keys for fabrics that no longer exist on online", func(t *testing.T) {
			t.Parallel()
			// A store holding a key of fabric 7, which it does not list: the
			// module's stores cascade a fabric's registrations away, so the
			// stale entry is planted under the server directly.
			st := &staleICDStore{Store: memstore.New(), stale: store.ICDRegistration{FabricIndex: 7, CheckInNodeID: 0x1234, MonitoredSubject: 0x1234, Key: icdKeyA}}
			srv, err := mattercore.NewICDManagement(mattercore.ICDManagementConfig{Store: st})
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Stop()
			if err := srv.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !st.pruned || len(srv.Registrations(7)) != 0 {
				t.Fatalf("pruned %v, registrations %+v", st.pruned, srv.Registrations(7))
			}
		})
	})

	t.Run("ICD advertisement", func(t *testing.T) {
		t.Parallel()
		t.Run("LITS device with no registrations starts in SIT mode", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			if st := icdState(t.Context(), t, pair); st.OperatingMode != icddef.OperatingModeSit {
				t.Fatalf("mode %v", st.OperatingMode)
			}
		})
		t.Run("LITS device provider yields SIT advertisement with idleInterval when no registrations", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: litConfig, advert: rec})
			_ = pair
			txt := rec.last(t)
			if txt["ICD"] != "0" || txt["SII"] == "" || txt["SAI"] == "" || txt["SAT"] != "5000" {
				t.Fatalf("TXT %v", txt)
			}
		})
		t.Run("operatingMode transitions to LIT after registerClient and provider omits idleInterval", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: litConfig, advert: rec})
			before := rec.count()
			if _, err := registerClient(t.Context(), pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			if st := icdState(t.Context(), t, pair); st.OperatingMode != icddef.OperatingModeLit {
				t.Fatalf("mode %v", st.OperatingMode)
			}
			if rec.count() <= before {
				t.Fatal("not re-advertised")
			}
			txt := rec.last(t)
			if _, sii := txt["SII"]; txt["ICD"] != "1" || sii || txt["SAT"] != "5000" {
				t.Fatalf("TXT %v", txt)
			}
		})
		t.Run("operatingMode falls back to SIT after unregisterClient removes the last registration", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: litConfig, advert: rec})
			ctx := t.Context()
			if _, err := registerClient(ctx, pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			before := rec.count()
			if err := unregisterClient(ctx, pair, icddef.UnregisterClientRequest{CheckInNodeId: pair.Address.NodeID}); err != nil {
				t.Fatal(err)
			}
			if st := icdState(ctx, t, pair); st.OperatingMode != icddef.OperatingModeSit {
				t.Fatalf("mode %v", st.OperatingMode)
			}
			if rec.count() <= before {
				t.Fatal("not re-advertised")
			}
		})
		t.Run("non-LITS default IcdManagementServer provider yields SIT and mode never changes with registrations", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{advert: rec})
			if txt := rec.last(t); txt["ICD"] != "0" || txt["SII"] == "" {
				t.Fatalf("TXT %v", txt)
			}
			if _, err := registerClient(t.Context(), pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			pair.Device.Node().TriggerReannounce(t.Context())
			if a := icdServer(pair).Advertisement(); a.Mode != icddef.OperatingModeSit || a.OmitIdleInterval {
				t.Fatalf("advertisement %+v", a)
			}
		})
	})

	t.Run("UserActiveModeTrigger (UAT)", func(t *testing.T) {
		t.Parallel()
		uatConfig := node.ICDConfig{
			IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
			ActiveModeDuration: 2000 * time.Millisecond, ActiveModeThreshold: 1000 * time.Millisecond,
			UserActiveModeTrigger: &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerCustomInstruction, Instruction: "Press and hold the button for 5s"},
		}
		t.Run("triggerUserActiveMode wakes an idle device", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			_, d := icdDevice(t, uatConfig, nil)
			ev.observe(d.Node())
			d.Node().ICD().EnterIdleMode()
			d.Node().ICD().TriggerUserActiveMode()
			// matter.js observes activeModeEntered alone; the idle entered
			// before it is the case's own EnterIdleMode.
			ev.want(t, "idleModeEntered", "activeModeEntered")
		})
		t.Run("rejects a CustomInstruction hint without an instruction string", func(t *testing.T) {
			t.Parallel()
			c := uatConfig
			c.UserActiveModeTrigger = &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerCustomInstruction}
			if !icdConfigInvalid(t, c) {
				t.Fatal("accepted")
			}
		})
		t.Run("rejects any instruction-dependent hint without an instruction string", func(t *testing.T) {
			t.Parallel()
			c := uatConfig
			c.UserActiveModeTrigger = &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerResetButtonSeconds}
			if !icdConfigInvalid(t, c) {
				t.Fatal("accepted")
			}
		})
		t.Run("accepts a hint that does not depend on the instruction with an empty instruction", func(t *testing.T) {
			t.Parallel()
			c := uatConfig
			c.UserActiveModeTrigger = &node.ICDUserActiveModeTrigger{Hint: icddef.UserActiveModeTriggerPowerCycle}
			if icdConfigInvalid(t, c) {
				t.Fatal("refused")
			}
		})
	})

	t.Run("DynamicSitLitSupport (DSLS) setOperatingMode", func(t *testing.T) {
		t.Parallel()
		dsls := litConfig
		dsls.DynamicSITLIT = true
		t.Run("setOperatingMode(Lit) with no registrations forces LIT and refreshes advertisement", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: dsls, advert: rec})
			before := rec.count()
			if err := pair.Device.Node().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			if st := icdState(t.Context(), t, pair); st.OperatingMode != icddef.OperatingModeLit || rec.count() <= before {
				t.Fatalf("mode %v, %d records after %d", st.OperatingMode, rec.count(), before)
			}
			if txt := rec.last(t); txt["ICD"] != "1" || txt["SII"] != "" || txt["SAT"] != "5000" {
				t.Fatalf("TXT %v", txt)
			}
		})
		t.Run("setOperatingMode(Sit) after forced LIT switches back to SIT and refreshes advertisement", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: dsls, advert: rec})
			h := pair.Device.Node().ICD()
			if err := h.SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			before := rec.count()
			if err := h.SetOperatingMode(node.ICDModeSIT); err != nil {
				t.Fatal(err)
			}
			if h.Mode() != node.ICDModeSIT || rec.count() <= before {
				t.Fatalf("mode %v, %d records after %d", h.Mode(), rec.count(), before)
			}
			if txt := rec.last(t); txt["ICD"] != "0" || txt["SII"] == "" {
				t.Fatalf("TXT %v", txt)
			}
		})
		t.Run("withdrawForcedOperatingMode reverts a forced LIT to the registration-driven mode and refreshes", func(t *testing.T) {
			t.Parallel()
			rec := newOperationalRecorder()
			_, pair := icdPair(t, icdSetup{cfg: dsls, advert: rec})
			h := pair.Device.Node().ICD()
			if err := h.SetOperatingMode(node.ICDModeLIT); err != nil {
				t.Fatal(err)
			}
			before := rec.count()
			if err := h.WithdrawForcedOperatingMode(); err != nil {
				t.Fatal(err)
			}
			if got := registeredClients(t.Context(), t, pair); len(got) != 0 || h.Mode() != node.ICDModeSIT || rec.count() <= before {
				t.Fatalf("registered %+v, mode %v, %d records after %d", got, h.Mode(), rec.count(), before)
			}
		})
		t.Run("non-DSLS LITS device withdrawForcedOperatingMode throws", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			if err := pair.Device.Node().ICD().WithdrawForcedOperatingMode(); !errors.Is(err, mattercore.ErrICDOperatingMode) {
				t.Fatalf("withdraw = %v", err)
			}
		})
		t.Run("non-DSLS LITS device setOperatingMode(Lit) with no registrations throws", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			if err := pair.Device.Node().ICD().SetOperatingMode(node.ICDModeLIT); !errors.Is(err, mattercore.ErrICDOperatingMode) {
				t.Fatalf("set = %v", err)
			}
		})
		t.Run("non-DSLS LITS device setOperatingMode matching registration state succeeds", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: litConfig})
			h := pair.Device.Node().ICD()
			if err := h.SetOperatingMode(node.ICDModeSIT); err != nil || h.Mode() != node.ICDModeSIT {
				t.Fatalf("set = %v, mode %v", err, h.Mode())
			}
		})
		t.Run("rejects a LITS-only server (CIP dropped by single-feature .with)", func(t *testing.T) {
			t.Parallel()
			sit := icddef.OperatingModeSit
			_, err := mattercore.NewICDManagement(mattercore.ICDManagementConfig{
				Features: icddef.FeatureLongIdleTimeSupport, ActiveModeThreshold: 5000, OperatingMode: &sit,
				IdleModeDuration: 3600, ActiveModeDuration: 1000, MaximumCheckInBackoff: 3600,
			})
			if !errors.Is(err, mattercore.ErrICDManagementConfig) {
				t.Fatalf("New = %v", err)
			}
		})
		t.Run("CIP-only IcdManagementServer setOperatingMode throws for missing LITS feature", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{})
			if err := pair.Device.Node().ICD().SetOperatingMode(node.ICDModeLIT); !errors.Is(err, mattercore.ErrICDOperatingMode) {
				t.Fatalf("set = %v", err)
			}
		})
	})

	t.Run("idle/active mode", func(t *testing.T) {
		t.Parallel()
		t.Run("starts in active mode and emits activeModeEntered on online", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			_, d := icdDevice(t, idleActiveConfig, ev.observe)
			ev.want(t, "activeModeEntered")
			if !d.Node().ICD().Active() {
				t.Fatal("not active")
			}
		})
		t.Run("signals mayEnterIdleMode after the active window without transitioning", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			site, d := icdDevice(t, idleActiveConfig, nil)
			ev.observe(d.Node())
			if err := site.Step(t.Context(), 2000*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			ev.want(t, "mayEnterIdleMode")
			if !d.Node().ICD().Active() {
				t.Fatal("transitioned")
			}
		})
		t.Run("inbound node activity extends the active window", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			site, d := icdDevice(t, idleActiveConfig, nil)
			ev.observe(d.Node())
			if err := site.Step(t.Context(), 1500*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			d.Node().Root().ICDManagement.NoteActivity() // the window extends to t+1000 = 2500 ms
			if err := site.Step(t.Context(), 900*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			ev.want(t)
			if err := site.Step(t.Context(), 200*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			ev.want(t, "mayEnterIdleMode")
		})
		t.Run("requestActiveMode wakes an idle device", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			_, d := icdDevice(t, idleActiveConfig, nil)
			ev.observe(d.Node())
			d.Node().ICD().EnterIdleMode()
			d.Node().ICD().RequestActiveMode()
			ev.want(t, "idleModeEntered", "activeModeEntered")
		})
		t.Run("enterIdleMode forces idle unconditionally", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			_, d := icdDevice(t, idleActiveConfig, nil)
			ev.observe(d.Node())
			d.Node().ICD().EnterIdleMode()
			ev.want(t, "idleModeEntered")
		})
		t.Run("stayActive extends the active window and promises the real remaining duration", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: idleActiveConfig})
			pair.Device.Node().ICD().EnterIdleMode()
			if got := stayActive(t.Context(), t, pair, 45000); got != 45000 {
				t.Fatalf("promised %d", got)
			}
		})
		t.Run("stayActive honors the 30s floor for a short request", func(t *testing.T) {
			t.Parallel()
			_, pair := icdPair(t, icdSetup{cfg: idleActiveConfig})
			pair.Device.Node().ICD().EnterIdleMode()
			// While idle: the window floor is the 2 s ActiveModeDuration;
			// max(2000, min(30000, 10)) = 2000.
			if got := stayActive(t.Context(), t, pair, 10); got != 2000 {
				t.Fatalf("promised %d", got)
			}
		})
		t.Run("stayActive extends an already-active window without re-emitting activeModeEntered", func(t *testing.T) {
			t.Parallel()
			ev := &icdEvents{}
			_, pair := icdPair(t, icdSetup{cfg: idleActiveConfig})
			ev.observe(pair.Device.Node())
			if got := stayActive(t.Context(), t, pair, 45000); got != 45000 {
				t.Fatalf("promised %d", got)
			}
			ev.wantNone(t, "activeModeEntered")
		})
	})

	t.Run("Check-In sending", func(t *testing.T) {
		t.Parallel()
		// wake forces one idle→active wake and lets its Check-In pass run
		// to completion (IcdManagementServerTest.ts:1224-1230).
		wake := func(t *testing.T, site *fabrictest.Site, p *fabrictest.Pair) {
			t.Helper()
			h := p.Device.Node().ICD()
			h.EnterIdleMode()
			h.RequestActiveMode()
			if err := site.Settle(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		t.Run("sends one Check-In per wake for a Permanent client with no covering subscription", func(t *testing.T) {
			t.Parallel()
			rec := &icdRecorder{}
			site, pair := icdPair(t, icdSetup{cfg: checkInConfig, recorder: rec})
			node := pair.Address.NodeID
			if _, err := registerClient(t.Context(), pair, permanent(node, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			before := icdServer(pair).Counter().Value()
			rec.reset()
			wake(t, site, pair)
			sent := rec.snapshot()
			if len(sent) != 1 || sent[0].PeerNodeID != node || icdServer(pair).Counter().Value() != before+1 {
				t.Fatalf("sent %+v, counter %d after %d", sent, icdServer(pair).Counter().Value(), before)
			}
		})
		t.Run("advances the ICDCounter once per Check-In sent across multiple registered clients", func(t *testing.T) {
			t.Parallel()
			rec := &icdRecorder{}
			site, pair := icdPair(t, icdSetup{cfg: checkInConfig, recorder: rec})
			nodeA := pair.Address.NodeID
			for _, r := range []icddef.RegisterClientRequest{permanent(nodeA, icdKeyA), permanent(nodeA+1, icdKeyC)} {
				if _, err := registerClient(t.Context(), pair, r); err != nil {
					t.Fatal(err)
				}
			}
			before := icdServer(pair).Counter().Value()
			rec.reset()
			wake(t, site, pair)
			sent := rec.snapshot()
			if len(sent) != 2 {
				t.Fatalf("sent %+v", sent)
			}
			counters := []uint32{sent[0].Counter, sent[1].Counter}
			slices.Sort(counters)
			if counters[1] != counters[0]+1 || icdServer(pair).Counter().Value() != before+2 {
				t.Fatalf("counters %v, counter %d after %d", counters, icdServer(pair).Counter().Value(), before)
			}
		})
		t.Run("suppresses the Check-In for a client covered by an active matching subscription", func(t *testing.T) {
			t.Parallel()
			rec := &icdRecorder{}
			site, pair := icdPair(t, icdSetup{cfg: checkInConfig, recorder: rec})
			ctx := t.Context()
			sub, err := fabrictest.Subscribe(ctx, pair, fabrictest.SubscribeRequest{
				Attributes: []fabrictest.AttributePath{client.AttributeAt(0, icddef.ClusterID, icddef.AttrIdleModeDuration)},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			subjects := pair.Device.Bridge().ActiveSubscriptionSubjects(pairFabric(ctx, t, pair))
			if len(subjects) == 0 {
				t.Fatal("no subscription subject")
			}
			if _, err := registerClient(ctx, pair, icddef.RegisterClientRequest{
				CheckInNodeId: pair.Address.NodeID, MonitoredSubject: subjects[0].NodeID, Key: icdKeyA, ClientType: icddef.ClientTypePermanent,
			}); err != nil {
				t.Fatal(err)
			}
			rec.reset()
			wake(t, site, pair)
			if sent := rec.snapshot(); len(sent) != 0 {
				t.Fatalf("sent %+v", sent)
			}
		})
		t.Run("never sends to an Ephemeral client", func(t *testing.T) {
			t.Parallel()
			rec := &icdRecorder{}
			site, pair := icdPair(t, icdSetup{cfg: checkInConfig, recorder: rec})
			node := pair.Address.NodeID
			if _, err := registerClient(t.Context(), pair, icddef.RegisterClientRequest{CheckInNodeId: node, MonitoredSubject: node, Key: icdKeyA, ClientType: icddef.ClientTypeEphemeral}); err != nil {
				t.Fatal(err)
			}
			rec.reset()
			wake(t, site, pair)
			if sent := rec.snapshot(); len(sent) != 0 {
				t.Fatalf("sent %+v", sent)
			}
		})
		t.Run("follows the back-off schedule across no-response wakes", func(t *testing.T) {
			t.Parallel()
			// MaximumCheckInBackoff 16 s is four idle durations: gaps 1, 2, 4
			// (capped), sends at wakes 0, 1, 3, 7, 11.
			rec := &icdRecorder{}
			c := checkInConfig
			c.MaximumCheckInBackoff = 16 * time.Second
			site, pair := icdPair(t, icdSetup{cfg: c, recorder: rec})
			if _, err := registerClient(t.Context(), pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			rec.reset()
			var at []int
			for w := range 12 {
				before := len(rec.snapshot())
				wake(t, site, pair)
				if len(rec.snapshot()) > before {
					at = append(at, w)
				}
			}
			if !slices.Equal(at, []int{0, 1, 3, 7, 11}) {
				t.Fatalf("sent at wakes %v", at)
			}
		})
		t.Run("triggerUserActiveMode resets the back-off so the next wake sends again", func(t *testing.T) {
			t.Parallel()
			rec := &icdRecorder{}
			c := checkInConfig
			c.MaximumCheckInBackoff = 16 * time.Second
			site, pair := icdPair(t, icdSetup{cfg: c, recorder: rec})
			if _, err := registerClient(t.Context(), pair, permanent(pair.Address.NodeID, icdKeyA)); err != nil {
				t.Fatal(err)
			}
			rec.reset()
			wake(t, site, pair)
			wake(t, site, pair)
			suppressed := len(rec.snapshot())
			wake(t, site, pair)
			if got := len(rec.snapshot()); got != suppressed {
				t.Fatalf("wake 2 sent: %d after %d", got, suppressed)
			}
			h := pair.Device.Node().ICD()
			h.EnterIdleMode()
			h.TriggerUserActiveMode()
			if err := site.Settle(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := len(rec.snapshot()); got != suppressed+1 {
				t.Fatalf("after the trigger %d, want %d", got, suppressed+1)
			}
		})
	})
}

// icdEvents records a node's mode events.
type icdEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *icdEvents) observe(n *node.Node) {
	n.ICD().OnModeChange(func(ev node.ICDModeEvent) {
		e.mu.Lock()
		e.events = append(e.events, ev.String())
		e.mu.Unlock()
	})
}

func (e *icdEvents) want(t *testing.T, want ...string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Equal(e.events, want) && !(len(want) == 0 && len(e.events) == 0) {
		t.Fatalf("events %v, want %v", e.events, want)
	}
}

func (e *icdEvents) wantNone(t *testing.T, name string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if slices.Contains(e.events, name) {
		t.Fatalf("events %v contain %s", e.events, name)
	}
}

// staleICDStore lists a registration of a fabric it does not hold and
// records the prune.
type staleICDStore struct {
	*memstore.Store
	stale  store.ICDRegistration
	pruned bool
}

func (s *staleICDStore) ListICDRegistrations(ctx context.Context) ([]store.ICDRegistration, error) {
	if s.pruned {
		return s.Store.ListICDRegistrations(ctx)
	}
	return []store.ICDRegistration{s.stale}, nil
}

func (s *staleICDStore) ReplaceICDRegistrations(ctx context.Context, fabricIndex uint8, regs []store.ICDRegistration) error {
	if fabricIndex == s.stale.FabricIndex && len(regs) == 0 {
		s.pruned = true
		return nil
	}
	return s.Store.ReplaceICDRegistrations(ctx, fabricIndex, regs)
}
