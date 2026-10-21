// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

// The OperationalCredentials surface the registry uses. ADR 0013
// generates no definition for the cluster (a named cluster), so its ids
// and payloads are written here from the matter.js model at 0ceb202d as
// parity/schema.json carries it: OperationalCredentials, cluster 62
// (0x003E) — attribute 1 Fabrics (a list of FabricDescriptorStruct: 1
// RootPublicKey octstr, 2 VendorId, 3 FabricId, 4 NodeId, 5 Label, 6
// VidVerificationStatement, 254 FabricIndex), attribute 5
// CurrentFabricIndex; command 8 NocResponse (0 StatusCode, 1 FabricIndex,
// 2 DebugText), 9 UpdateFabricLabel (0 Label), 10 RemoveFabric (0
// FabricIndex); NodeOperationalCertStatusEnum 0 Ok.
// controller/commissioner/wire.go writes the same shapes for the flow.

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// OperationalCredentials ids (parity/schema.json, OperationalCredentials).
const (
	opCredsCluster                uint32 = 0x003E
	opCredsAttrFabrics            uint32 = 0x0001
	opCredsAttrCurrentFabricIndex uint32 = 0x0005
	opCredsCmdNOCResponse         uint32 = 0x08
	opCredsCmdUpdateFabricLabel   uint32 = 0x09
	opCredsCmdRemoveFabric        uint32 = 0x0A
	nocStatusOk                   uint8  = 0
)

// fabricDescriptor is one FabricDescriptorStruct of the peer's Fabrics.
type fabricDescriptor struct {
	RootPublicKey []byte
	VendorID      uint16
	FabricID      uint64
	NodeID        uint64
	Label         string
	FabricIndex   uint8
}

func decodeFabricDescriptor(n spec.Node) (fabricDescriptor, error) {
	var f fabricDescriptor
	err := spec.DecodeFields(n, "FabricDescriptorStruct",
		spec.Bind(1, "RootPublicKey", true, &f.RootPublicKey, spec.DecodeBytes),
		spec.Bind(2, "VendorId", true, &f.VendorID, spec.DecodeUint[uint16](16)),
		spec.Bind(3, "FabricId", true, &f.FabricID, spec.DecodeUint[uint64](64)),
		spec.Bind(4, "NodeId", true, &f.NodeID, spec.DecodeUint[uint64](64)),
		spec.Bind(5, "Label", true, &f.Label, spec.DecodeString),
		spec.Bind(0xFE, "FabricIndex", false, &f.FabricIndex, spec.DecodeUint[uint8](8)))
	return f, err
}

// opCreds is the image's OperationalCredentials cluster on endpoint 0,
// nil when the image has none — matter.js
// node.behaviors.has(OperationalCredentialsClient).
func (n *Node) opCreds() *Cluster {
	root := n.Endpoint(0)
	if root == nil {
		return nil
	}
	return root.Cluster(opCredsCluster)
}

// currentFabricIndex is the image's CurrentFabricIndex, 0 when absent.
func currentFabricIndex(c *Cluster) uint8 {
	if c == nil {
		return 0
	}
	v, ok := c.Value(opCredsAttrCurrentFabricIndex)
	if !ok {
		return 0
	}
	idx, err := decodeFabricIndex(v)
	if err != nil {
		return 0
	}
	return idx
}

func decodeFabricIndex(v spec.Node) (uint8, error) { return spec.DecodeUint[uint8](8)(v) }

// peerFabrics is the image's Fabrics; an entry that does not decode is
// skipped.
func peerFabrics(c *Cluster) []fabricDescriptor {
	if c == nil {
		return nil
	}
	v, ok := c.Value(opCredsAttrFabrics)
	if !ok || v.El.Type != tlv.TypeArray {
		return nil
	}
	out := make([]fabricDescriptor, 0, len(v.Children))
	for _, e := range v.Children {
		if f, err := decodeFabricDescriptor(e); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// sameFabric is FabricManager.forDescriptor's match (D5 of brief AL): the
// root public key and the fabric id.
func sameFabric(rootPublicKey []byte, fabricID uint64, f fabricDescriptor) bool {
	return f.FabricID == fabricID && bytes.Equal(f.RootPublicKey, rootPublicKey)
}

// removeFabricRequest is RemoveFabric {[0] FabricIndex}.
type removeFabricRequest uint8

func (r removeFabricRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r))
	_ = enc.EndContainer()
}

// updateFabricLabelRequest is UpdateFabricLabel {[0] Label}.
type updateFabricLabelRequest string

func (r updateFabricLabelRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUTF8(tlv.ContextTag(0), string(r))
	_ = enc.EndContainer()
}

// nocResponse is NocResponse {[0] StatusCode, [1] FabricIndex, [2]
// DebugText}.
type nocResponse struct {
	status      uint8
	fabricIndex *uint8
	debugText   *string
}

// errNoNOCResponse: the command was answered without a NocResponse.
var errNoNOCResponse = errors.New("client: no NocResponse")

func nocResponseOf(res interaction.Result) (nocResponse, error) {
	var r nocResponse
	if !res.HasFields || res.Command != opCredsCmdNOCResponse {
		return r, errNoNOCResponse
	}
	err := spec.DecodeFields(res.Fields, "NocResponse",
		spec.Bind(0, "StatusCode", true, &r.status, spec.DecodeUint[uint8](8)),
		spec.Bind(1, "FabricIndex", false, &r.fabricIndex, spec.DecodeOptional(spec.DecodeUintIn[uint8](8, 1, 254))),
		spec.Bind(2, "DebugText", false, &r.debugText, spec.DecodeOptional(spec.DecodeStringIn(0, 128))))
	if err != nil {
		return r, fmt.Errorf("client: NocResponse: %w", err)
	}
	return r, nil
}

func opCredsCommand(cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: 0, Cluster: opCredsCluster, Command: cmd}
}
