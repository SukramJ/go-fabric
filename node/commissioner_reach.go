// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
)

// commissionerReach is the node's [jointfabric.CommissionerReach]: it
// reaches the commissioner that announced itself over the node's client
// stack — a CASE session to the invoking peer ([Node.Peers]), the reads
// of its AdministratorFabricIndex and of OperationalCredentials' Fabrics,
// TrustedRootCertificates and NOCs unfiltered, and the
// SignVIDVerificationRequest with that session's attestation challenge.
// connectedhomeip's JCMCommissionee reads over the invoking session itself
// (JCMCommissionee.cpp:46-69, :297-322 at 6170af84) and its
// VendorIdVerificationClient takes the challenge of the session it sends
// on (VendorIdVerificationClient.cpp:145-162); here the node opens its own
// CASE session to the peer, whose challenge the signature covers. The
// node mounts it on every Administrator whose declaration names no reach
// ([jointfabric.Administrator.AttachCommissionerReach]) while
// [Config].Client is on.
type commissionerReach struct{ n *Node }

var _ jointfabric.CommissionerReach = commissionerReach{}

// errNoClientStack is a reach without the node's client stack.
var errNoClientStack = errors.New("node: the commissioner reach needs Config.Client")

// interaction opens the interaction with the peer, connecting first.
func (r commissionerReach) interaction(ctx context.Context, p jointfabric.CommissionerPeer) (*interaction.Interaction, error) {
	set := r.n.Peers()
	if set == nil {
		return nil, errNoClientStack
	}
	pr, err := set.For(handshake.PeerAddress{FabricIndex: p.FabricIndex, NodeID: p.NodeID})
	if err != nil {
		return nil, fmt.Errorf("node: commissioner %d:%#x: %w", p.FabricIndex, p.NodeID, err)
	}
	if _, err := pr.Connect(ctx, peer.ConnectOptions{}); err != nil {
		return nil, fmt.Errorf("node: commissioner %d:%#x: %w", p.FabricIndex, p.NodeID, err)
	}
	return interaction.ForPeer(pr, interaction.Config{Logger: r.n.logger, Clock: r.n.cfg.Clock}), nil
}

// read reads one attribute unfiltered and returns its value; a status in
// its place is an error.
func (r commissionerReach) read(ctx context.Context, p jointfabric.CommissionerPeer, path im.ConcreteAttributePath) (spec.Node, error) {
	ia, err := r.interaction(ctx, p)
	if err != nil {
		return spec.Node{}, err
	}
	rep, err := ia.ReadWith(ctx, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}, NoFabricFilter: true})
	if err != nil {
		return spec.Node{}, err
	}
	if v, ok := rep.Value(path.Endpoint, path.Cluster, path.Attribute); ok {
		return v, nil
	}
	for _, s := range rep.Statuses {
		if s.Path.Attribute == path.Attribute {
			return spec.Node{}, fmt.Errorf("node: commissioner read of 0x%04X/0x%04X: %v", path.Cluster, path.Attribute, s.Status)
		}
	}
	return spec.Node{}, fmt.Errorf("node: commissioner read of 0x%04X/0x%04X carried no value", path.Cluster, path.Attribute)
}

func opCredsPath(attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, Cluster: opcreds.ClusterID, Attribute: attr}
}

// ReadAdministratorFabricIndex reads the commissioner's
// AdministratorFabricIndex on endpointID (JCMCommissionee.cpp:183-224).
func (r commissionerReach) ReadAdministratorFabricIndex(ctx context.Context, p jointfabric.CommissionerPeer, endpointID uint16) (uint8, bool, error) {
	v, err := r.read(ctx, p, im.ConcreteAttributePath{Endpoint: endpointID, Cluster: jfadef.ClusterID, Attribute: jfadef.AttrAdministratorFabricIndex})
	if err != nil {
		return 0, false, err
	}
	idx, err := spec.DecodeNullable(spec.DecodeUint[uint8](8))(v)
	if err != nil {
		return 0, false, err
	}
	return idx.Value, idx.Null, nil
}

// ReadFabrics reads the commissioner's Fabrics (:226-261).
func (r commissionerReach) ReadFabrics(ctx context.Context, p jointfabric.CommissionerPeer) ([]opcreds.FabricDescriptorStruct, error) {
	v, err := r.read(ctx, p, opCredsPath(opcreds.AttrFabrics))
	if err != nil {
		return nil, err
	}
	return spec.DecodeList(spec.DecodeStruct[opcreds.FabricDescriptorStruct])(v)
}

// ReadTrustedRootCertificates reads the commissioner's
// TrustedRootCertificates (:342-408).
func (r commissionerReach) ReadTrustedRootCertificates(ctx context.Context, p jointfabric.CommissionerPeer) ([][]byte, error) {
	v, err := r.read(ctx, p, opCredsPath(opcreds.AttrTrustedRootCertificates))
	if err != nil {
		return nil, err
	}
	return spec.DecodeList(spec.DecodeBytes)(v)
}

// ReadNOCs reads the commissioner's NOCs (:410-479).
func (r commissionerReach) ReadNOCs(ctx context.Context, p jointfabric.CommissionerPeer) ([]opcreds.NOCStruct, error) {
	v, err := r.read(ctx, p, opCredsPath(opcreds.AttrNocs))
	if err != nil {
		return nil, err
	}
	return spec.DecodeList(spec.DecodeStruct[opcreds.NOCStruct])(v)
}

// SignVIDVerification sends SignVIDVerificationRequest to the commissioner
// and returns its response with the attestation challenge of the session
// it went over (VendorIdVerificationClient.cpp:127-187).
func (r commissionerReach) SignVIDVerification(ctx context.Context, p jointfabric.CommissionerPeer, fabricIndex uint8, clientChallenge []byte) (opcreds.SignVidVerificationResponse, []byte, error) {
	ia, err := r.interaction(ctx, p)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	path := im.ConcreteCommandPath{Endpoint: 0, Cluster: opcreds.ClusterID, Command: opcreds.CmdSignVidVerificationRequest}
	res, err := ia.Invoke(ctx, path, opcreds.SignVidVerificationRequestRequest{FabricIndex: fabricIndex, ClientChallenge: clientChallenge}, interaction.InvokeOptions{})
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	if res.Status != im.StatusSuccess || !res.HasFields {
		return opcreds.SignVidVerificationResponse{}, nil, fmt.Errorf("node: SignVIDVerificationRequest answered %v", res.Status)
	}
	resp, err := spec.DecodeStruct[opcreds.SignVidVerificationResponse](res.Fields)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	sess := ia.Session()
	if sess == nil {
		return opcreds.SignVidVerificationResponse{}, nil, errors.New("node: the commissioner's session closed")
	}
	return resp, sess.AttestationChallenge(), nil
}

// commissionerReachParticipant is a server that takes the node's reach
// when it has none of its own (the Joint Fabric Administrator).
type commissionerReachParticipant interface {
	AttachCommissionerReach(r jointfabric.CommissionerReach)
}
