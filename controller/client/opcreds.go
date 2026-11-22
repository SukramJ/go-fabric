// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

// The OperationalCredentials surface the registry uses. Its ids are the
// generated definition's (cluster/spec/operationalcredentials, ADR 0013),
// so a regeneration carries them (review II, R2-P6-par-2360); the payloads
// the registry decodes are written here: the Fabrics list's
// FabricDescriptorStruct (1 RootPublicKey octstr, 2 VendorId, 3 FabricId,
// 4 NodeId, 5 Label, 6 VidVerificationStatement, 254 FabricIndex),
// NocResponse (0 StatusCode, 1 FabricIndex, 2 DebugText),
// UpdateFabricLabel (0 Label) and RemoveFabric (0 FabricIndex).
// controller/commissioner/wire.go writes the same shapes for the flow.

import (
	"bytes"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/tlv"
)

// OperationalCredentials ids, the generated definition's.
const (
	opCredsCluster                = operationalcredentials.ClusterID
	opCredsAttrFabrics            = operationalcredentials.AttrFabrics
	opCredsAttrCurrentFabricIndex = operationalcredentials.AttrCurrentFabricIndex
	opCredsCmdNOCResponse         = operationalcredentials.CmdNocResponse
	opCredsCmdUpdateFabricLabel   = operationalcredentials.CmdUpdateFabricLabel
	opCredsCmdRemoveFabric        = operationalcredentials.CmdRemoveFabric
	nocStatusOk                   = uint8(operationalcredentials.NodeOperationalCertStatusOk)
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

// backfillFabricIndexOnPeer recovers a commissioned node's missing
// FabricIndexOnPeer from the image's CurrentFabricIndex — a node stored
// before the field existed, or one whose field was cleared. It runs when
// the node is loaded and after each report, until the field is filled.
// Mirrors matter.js CommissioningClient.#backfillFabricIndexOnPeer and its
// currentFabricIndex$Changed reaction
// (packages/node/src/behavior/system/commissioning/CommissioningClient.ts:605-630
// at db021da0).
func (n *Node) backfillFabricIndexOnPeer() {
	n.mu.RLock()
	missing := n.record.PeerAddress != nil && n.record.FabricIndexOnPeer == 0 && !n.erased
	n.mu.RUnlock()
	if !missing {
		return
	}
	idx := currentFabricIndex(n.opCreds())
	if idx == 0 {
		return
	}
	n.updateRecord(func(r *Record) {
		if r.PeerAddress != nil && r.FabricIndexOnPeer == 0 {
			r.FabricIndexOnPeer = idx
		}
	})
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

func nocResponseOf(res Result) (nocResponse, error) {
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

func opCredsCommand(cmd uint32) CommandPath {
	return CommandPath{Endpoint: 0, Cluster: opCredsCluster, Command: cmd}
}
