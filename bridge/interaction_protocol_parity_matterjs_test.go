// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// Commands of scriptedOnOff beyond OnOff's own: one that fails, one that
// answers with a response command, one that answers BUSY.
const (
	cmdThrows       uint32 = 0x40
	cmdData         uint32 = 0x41
	cmdDataResponse uint32 = 0x42
	cmdBusy         uint32 = 0x43
)

// scriptedOnOff is an OnOff server whose extra commands play the
// behaviours InteractionProtocolTest.ts gives EventedOnOffServer through
// its event handlers.
type scriptedOnOff struct {
	mu sync.Mutex
	on bool
}

func (s *scriptedOnOff) MatterClusterID() uint32 { return 0x0006 }

func (s *scriptedOnOff) MatterRead(attrID uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attrID == 0x0000 {
		return s.on, true
	}
	return nil, false
}

func (s *scriptedOnOff) MatterWrite(context.Context, uint32, any) error {
	return errors.New("read-only")
}

// busyError is a StatusResponseError(Busy) (im.StatusCodeError).
type busyError struct{}

func (busyError) Error() string                   { return "Sorry so swamped" }
func (busyError) MatterStatusCode() im.StatusCode { return im.StatusBusy }

// dataResponse is a response command payload (spec.ResponsePayload).
type dataResponse struct{}

func (dataResponse) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	_ = enc.EndContainer()
}

func (dataResponse) ResponseCommand() (clusterID, commandID uint32) { return 0x0006, cmdDataResponse }

func (s *scriptedOnOff) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch cmdID {
	case 0x00:
		s.on = false
	case 0x01:
		s.on = true
	case cmdThrows:
		return nil, errors.New("boom")
	case cmdData:
		return dataResponse{}, nil
	case cmdBusy:
		return nil, busyError{}
	default:
		return nil, errors.New("unknown command")
	}
	return nil, nil
}

func (s *scriptedOnOff) MatterReportable() []uint32 { return []uint32{0x0000} }
func (s *scriptedOnOff) MatterAcceptedCommands() []uint32 {
	return []uint32{0x00, 0x01, cmdThrows, cmdData, cmdBusy}
}
func (s *scriptedOnOff) MatterGeneratedCommands() []uint32 { return []uint32{cmdDataResponse} }

func (s *scriptedOnOff) isOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

type scriptedSource struct{ srv *scriptedOnOff }

func (scriptedSource) MatterDeviceType() uint16 { return 0x0100 }
func (s scriptedSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// newScriptedHarness is a secure harness with one OnOff light of
// scriptedOnOff, the controller holding Administer.
func newScriptedHarness(t *testing.T) (*secureHarness, *scriptedOnOff, uint16) {
	t.Helper()
	srv := &scriptedOnOff{}
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatal(err)
		}
		specs := []endpoint.Spec{{StableKey: endpoint.StringKey("light"), DeviceType: 0x0100, FriendlyName: "Light", Source: scriptedSource{srv}}}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "lights", Endpoints: specs}})
		}, nil
	})
	h.allowAll()
	bridged := h.bridge.Topology().Bridged()
	if len(bridged) != 1 {
		t.Fatalf("%d bridged endpoints, want 1", len(bridged))
	}
	return h, srv, bridged[0].ID
}

// invokeEntry is one CommandDataIB of an InvokeRequest; fields nil omits
// CommandFields.
type invokeEntry struct {
	cmd    uint32
	fields bool
	ref    uint16
	hasRef bool
}

func encodeInvokeEntries(t *testing.T, suppress bool, ep uint16, entries ...invokeEntry) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), suppress)
	enc.PutBool(tlv.ContextTag(1), false)
	enc.StartArray(tlv.ContextTag(2))
	for _, e := range entries {
		enc.StartStruct(tlv.AnonymousTag())
		im.ConcreteCommandPath{Endpoint: ep, Cluster: 0x0006, Command: e.cmd, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
		if e.fields {
			enc.StartStruct(tlv.ContextTag(1))
			_ = enc.EndContainer()
		}
		if e.hasRef {
			enc.PutUint(tlv.ContextTag(2), uint64(e.ref))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// invokeResponses decodes an InvokeResponseMessage into its InvokeResponseIBs.
func invokeResponses(t *testing.T, payload []byte) []tlvNode {
	t.Helper()
	return decodeTLVTree(t, payload).mustChild(t, 1).Children
}

// TestInteractionProtocolBridgeParityMatterJS ports the invoke cases of
// matter.js packages/node/test/endpoint/server/InteractionProtocolTest.ts
// at 862ad75f that need a scripted command server or a group message.
// matter.js scripts EventedOnOffServer through its onCalled/onOff$Changing
// events; scriptedOnOff answers the same way through its extra commands.
func TestInteractionProtocolBridgeParityMatterJS(t *testing.T) {
	t.Parallel()

	// InteractionProtocolTest.ts:2126-2138: a CommandDataIB without
	// CommandFields runs the command.
	t.Run("invoke command with no args", func(t *testing.T) {
		t.Parallel()
		h, srv, ep := newScriptedHarness(t)
		op, payload, ok := h.exchange(im.OpcodeInvokeRequest, encodeInvokeEntries(t, false, ep, invokeEntry{cmd: 0x01}))
		if !ok || op != im.OpcodeInvokeResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want InvokeResponse", op, ok)
		}
		st := invokeResponses(t, payload)[0].mustChild(t, 1).mustChild(t, 1).mustChild(t, 0).El.Uint
		if im.StatusCode(st) != im.StatusSuccess || !srv.isOn() {
			t.Fatalf("status %d, on %v, want SUCCESS and on", st, srv.isOn())
		}
	})

	// :2317-2334: under SuppressResponse a command that fails sends
	// nothing and changes nothing.
	t.Run("sends no response under SuppressResponse when a command throws", func(t *testing.T) {
		t.Parallel()
		h, srv, ep := newScriptedHarness(t)
		if op, _, ok := h.exchange(im.OpcodeInvokeRequest, encodeInvokeEntries(t, true, ep, invokeEntry{cmd: cmdThrows, fields: true})); ok {
			t.Fatalf("reply opcode 0x%02X, want none", op)
		}
		if srv.isOn() {
			t.Fatal("state changed")
		}
	})

	// :2351-2370: a command that generates a CommandDataIB is answered
	// under SuppressResponse with that one entry.
	t.Run("sends the response under SuppressResponse when a CommandDataIB is generated", func(t *testing.T) {
		t.Parallel()
		h, _, ep := newScriptedHarness(t)
		op, payload, ok := h.exchange(im.OpcodeInvokeRequest, encodeInvokeEntries(t, true, ep, invokeEntry{cmd: cmdData, fields: true}))
		if !ok || op != im.OpcodeInvokeResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want InvokeResponse", op, ok)
		}
		entries := invokeResponses(t, payload)
		if len(entries) != 1 {
			t.Fatalf("%d entries, want 1", len(entries))
		}
		path := entries[0].mustChild(t, 0).mustChild(t, 0)
		if path.mustChild(t, 1).El.Uint != 0x0006 || path.mustChild(t, 2).El.Uint != uint64(cmdDataResponse) {
			t.Fatalf("command path %+v, want the response command", path)
		}
	})

	// :2372-2386: a held status goes out with the CommandDataIB that forces
	// the response: status ref 0, command ref 1.
	t.Run("under SuppressResponse, flushes a held status alongside the forcing CommandDataIB", func(t *testing.T) {
		t.Parallel()
		h, _, ep := newScriptedHarness(t)
		op, payload, ok := h.exchange(im.OpcodeInvokeRequest, encodeInvokeEntries(t, true, ep,
			invokeEntry{cmd: 0x01, fields: true, ref: 0, hasRef: true},
			invokeEntry{cmd: cmdData, fields: true, ref: 1, hasRef: true}))
		if !ok || op != im.OpcodeInvokeResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want InvokeResponse", op, ok)
		}
		entries := invokeResponses(t, payload)
		if len(entries) != 2 {
			t.Fatalf("%d entries, want 2", len(entries))
		}
		status, ok1 := entries[0].child(1)
		command, ok2 := entries[1].child(0)
		if !ok1 || !ok2 || status.mustChild(t, 2).El.Uint != 0 || command.mustChild(t, 2).El.Uint != 1 {
			t.Fatalf("entries %+v, want status ref 0 then command ref 1", entries)
		}
	})

	// :2401-2416: a StatusResponseError(Busy) is the command's BUSY status.
	t.Run("handles StatusResponseError gracefully", func(t *testing.T) {
		t.Parallel()
		h, _, ep := newScriptedHarness(t)
		op, payload, ok := h.exchange(im.OpcodeInvokeRequest, encodeInvokeEntries(t, false, ep, invokeEntry{cmd: cmdBusy, fields: true}))
		if !ok || op != im.OpcodeInvokeResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want InvokeResponse", op, ok)
		}
		st := invokeResponses(t, payload)[0].mustChild(t, 1).mustChild(t, 1).mustChild(t, 0).El.Uint
		if im.StatusCode(st) != im.StatusBusy {
			t.Fatalf("status 0x%02X, want BUSY", st)
		}
	})

	// :2570-2595 and :2597-2614: a group invoke that ran but whose command
	// answered a non-success status reports AccessAllowed true, and the
	// source address of its own message (Groupcast §11.27.7.6.3).
	t.Run("group invoke reports accessAllowed:true when the dispatched command returns a non-Success status", func(t *testing.T) {
		t.Parallel()
		src := net.ParseIP("fd00::7")
		path := im.ConcreteCommandPath{Cluster: 6, Command: 1, HasCluster: true, HasCommand: true}
		ran := im.InvokeResult{Path: im.ConcreteCommandPath{Endpoint: 3, Cluster: 6, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}, Status: im.StatusBusy}
		evs := invokeOutcomeEvents(&groups.Message{FabricIndex: 1, GroupID: 7, HasValidMapping: true, Endpoints: []uint16{3}}, src,
			im.GroupInvokeReport{Processed: true, Requested: []im.ConcreteCommandPath{path}, Dispatched: []im.InvokeResult{ran}})
		if len(evs) != 1 || evs[0].AccessAllowed == nil || !*evs[0].AccessAllowed {
			t.Fatalf("events %+v, want one with AccessAllowed true", evs)
		}
		if !evs[0].SourceIP.Equal(src) {
			t.Fatalf("source %v, want %v", evs[0].SourceIP, src)
		}
	})
}
