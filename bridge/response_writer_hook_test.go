// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
)

// answering is a cluster server of cluster id answering every command with
// resp: a hand-written cluster/core server as the dispatcher sees it.
type answering struct {
	id   uint32
	resp any
}

func (s answering) MatterClusterID() uint32                                { return s.id }
func (answering) MatterRead(uint32) (any, bool)                            { return nil, false }
func (answering) MatterWrite(context.Context, uint32, any) error           { return nil }
func (answering) MatterReportable() []uint32                               { return nil }
func (s answering) MatterInvoke(context.Context, uint32, any) (any, error) { return s.resp, nil }

// answeringSource serves the servers on its endpoint.
type answeringSource struct{ srvs []contract.ClusterServer }

func (answeringSource) MatterDeviceType() uint16                         { return 0x0016 }
func (s answeringSource) MatterClusterServers() []contract.ClusterServer { return s.srvs }

// handResponses are the hand-written cluster/core response forms the
// bridge's response writer (defaultCommandFieldsWriter) encodes, each with
// a request command it answers, in a complete form: a success NOCResponse
// carries its FabricIndex (conformance "StatusCode == Ok"), a successful
// GetSceneMembershipResponse its SceneList ("Status == Success"), as the
// cluster/core servers answer them.
var handResponses = []struct {
	cluster uint32
	request string
	resp    any
}{
	{0x0030, "ArmFailSafe", mattercore.ArmFailSafeResponse{}},
	{0x0030, "SetRegulatoryConfig", mattercore.SetRegulatoryConfigResponse{}},
	{0x0030, "CommissioningComplete", mattercore.CommissioningCompleteResponse{}},
	{0x003E, "AttestationRequest", mattercore.AttestationResponse{}},
	{0x003E, "CertificateChainRequest", mattercore.CertificateChainResponse{}},
	{0x003E, "CsrRequest", mattercore.CSRResponse{}},
	{0x003E, "AddNoc", mattercore.NOCResponse{FabricIndex: 1}},
	{0x003E, "UpdateNoc", mattercore.NOCResponse{FabricIndex: 1}},
	{0x003E, "UpdateFabricLabel", mattercore.NOCResponse{FabricIndex: 1}},
	{0x003E, "RemoveFabric", mattercore.NOCResponse{FabricIndex: 1}},
	{0x003E, "SignVidVerificationRequest", mattercore.SignVidVerificationResponse{}},
	{0x003F, "KeySetRead", mattercore.KeySetReadResponse{}},
	{0x003F, "KeySetReadAllIndices", mattercore.KeySetReadAllIndicesResponse{}},
	{0x0004, "AddGroup", mattercore.AddGroupResponse{}},
	{0x0004, "ViewGroup", mattercore.ViewGroupResponse{}},
	{0x0004, "GetGroupMembership", mattercore.GetGroupMembershipResponse{}},
	{0x0004, "RemoveGroup", mattercore.RemoveGroupResponse{}},
	{0x0062, "AddScene", mattercore.SceneStatusResponse{}},
	{0x0062, "ViewScene", mattercore.ViewSceneResponse{}},
	{0x0062, "RemoveScene", mattercore.SceneStatusResponse{}},
	{0x0062, "RemoveAllScenes", mattercore.RemoveAllScenesResponse{}},
	{0x0062, "StoreScene", mattercore.SceneStatusResponse{}},
	{0x0062, "GetSceneMembership", mattercore.GetSceneMembershipResponse{HasList: true}},
	{0x0062, "CopyScene", mattercore.CopySceneResponse{}},
	{0x0065, "LeaveGroup", mattercore.LeaveGroupResponse{}},
}

// dropFirstMandatory is the bridge's response writer with the first field
// the response definition makes mandatory left out of what it writes.
func dropFirstMandatory(resp *spec.Command) im.CommandFieldsWriter {
	return func(enc *tlv.Encoder, tag tlv.Tag, v any) {
		inner := tlv.NewEncoder()
		defaultCommandFieldsWriter(inner, tlv.AnonymousTag(), v)
		raw, _ := inner.Bytes()
		dec := tlv.NewDecoder(raw)
		open, _ := dec.Next()
		n, _ := spec.ReadContainer(dec, open)
		for fi := range resp.Fields {
			if resp.Fields[fi].Conformance.Op != spec.ConfMandatory {
				continue
			}
			kept := n.Children[:0:0]
			for _, c := range n.Children {
				if c.El.Tag.Kind != tlv.TagKindContext || c.El.Tag.Number != resp.Fields[fi].ID {
					kept = append(kept, c)
				}
			}
			n.Children = kept
			break
		}
		n.EncodeTLV(enc, tag)
	}
}

// TestHandWrittenResponseIsJudgedAsTheBridgeWritesIt (decision 343): the
// bridge sets its response writer on the dispatcher, and the response
// check after the server (spec.CheckResponseAsWritten) judges a
// cluster/core server's hand-written response as the InvokeResponse
// carries it: complete, it passes; lacking a field its definition makes
// mandatory, it is FAILURE (matter.js validates every response in
// completeResponse, packages/node/src/node/integration/ProtocolService.ts:717-733
// at b25ed960). Without a writer — a host's own dispatcher — the hand
// form is not judged, as before.
func TestHandWrittenResponseIsJudgedAsTheBridgeWritesIt(t *testing.T) {
	t.Parallel()
	for _, c := range handResponses {
		def, _ := handReader(im.ConcreteCommandPath{Cluster: c.cluster})
		req := def.CommandByName(c.request)
		if req == nil || req.Direction != spec.Request {
			t.Fatalf("%s has no request %s", def.Name, c.request)
		}
		resp := def.CommandByName(req.Response)
		if resp == nil {
			t.Fatalf("%s.%s has no response command", def.Name, req.Name)
		}
		t.Run(def.Name+"."+req.Name, func(t *testing.T) {
			t.Parallel()
			// A request the gate passes on: the generated form of a
			// payload carrying every mandatory field (synthStruct).
			enc := tlv.NewEncoder()
			synthStruct(enc, tlv.AnonymousTag(), req.Fields)
			raw, _ := enc.Bytes()
			dec := tlv.NewDecoder(raw)
			open, _ := dec.Next()
			n, _ := spec.ReadContainer(dec, open)
			generated, err := req.Decode(n)
			if err != nil {
				t.Fatalf("synthesised request: %v", err)
			}
			invoke := func(write im.CommandFieldsWriter, set bool) im.StatusCode {
				d := endpoint.NewTopologyDispatcher(&endpoint.Topology{Endpoints: []*endpoint.Endpoint{
					{ID: 0}, {ID: 2, Source: answeringSource{srvs: []contract.ClusterServer{answering{c.cluster, c.resp}}}},
				}})
				if set {
					d.SetResponseWriter(write)
				}
				path := im.ConcreteCommandPath{Endpoint: 2, HasEndpoint: true, Cluster: c.cluster, Command: req.ID}
				return d.Invoke(context.Background(), path, spec.HandForm{Generated: generated, Hand: nil}).Status
			}
			if st := invoke(defaultCommandFieldsWriter, true); st != im.StatusSuccess {
				t.Errorf("complete, as the bridge writes it: %v, want Success", st)
			}
			if st := invoke(dropFirstMandatory(resp), true); st != im.StatusFailure {
				t.Errorf("a mandatory field missing from the written response: %v, want FAILURE", st)
			}
			if st := invoke(dropFirstMandatory(resp), false); st != im.StatusSuccess {
				t.Errorf("no writer set: %v, want Success (not judged)", st)
			}
		})
	}
}
