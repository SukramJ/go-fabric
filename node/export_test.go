// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
)

// RemoveFabricBindings is the node's removal of a fabric's server state —
// its Binding entries among it — what OperationalCredentials' RemoveFabric
// runs first (fabricRemoved → dropFabricState).
func RemoveFabricBindings(n *Node, fabricIndex uint8) {
	n.dropFabricState(context.Background(), fabricIndex)
}

// CommissionerReachSign drives the commissioner reach's
// SignVIDVerification of the node nodeID of fabric fabricIndex over n's
// client stack and returns the attestation challenges of every session the
// node holds to that peer once it returned, beside its own result.
func CommissionerReachSign(ctx context.Context, n *Node, fabricIndex uint8, nodeID uint64, signFabric uint8, clientChallenge []byte) (challenge []byte, sessions [][]byte, err error) {
	r := commissionerReach{n: n}
	_, challenge, err = r.SignVIDVerification(ctx, jointfabric.CommissionerPeer{FabricIndex: fabricIndex, NodeID: nodeID}, signFabric, clientChallenge)
	if pr, perr := n.connectedPeer(ctx, fabricIndex, nodeID); perr == nil {
		for _, s := range pr.Sessions() {
			sessions = append(sessions, s.AttestationChallenge())
		}
	}
	return challenge, sessions, err
}

// Reassemblies is the number of reassemblies the Aggregator completed.
func Reassemblies(a *Aggregator) int64 { return a.reassemblies.Load() }

// CommissionerReachReports drives the commissioner reach's reads of the
// node nodeID of fabric fabricIndex over n's client stack — its
// ReadAdministratorFabricIndex on adminEndpoint, ReadFabrics,
// ReadTrustedRootCertificates and ReadNOCs, in that order — and returns
// the report each received, with each read's error.
func CommissionerReachReports(ctx context.Context, n *Node, fabricIndex uint8, nodeID uint64, adminEndpoint uint16) (reports []*interaction.Report, errs []error) {
	r := commissionerReach{n: n, observe: func(rep *interaction.Report) { reports = append(reports, rep) }}
	p := jointfabric.CommissionerPeer{FabricIndex: fabricIndex, NodeID: nodeID}
	_, _, err := r.ReadAdministratorFabricIndex(ctx, p, adminEndpoint)
	errs = append(errs, err)
	_, err = r.ReadFabrics(ctx, p)
	errs = append(errs, err)
	_, err = r.ReadTrustedRootCertificates(ctx, p)
	errs = append(errs, err)
	_, err = r.ReadNOCs(ctx, p)
	errs = append(errs, err)
	return reports, errs
}

// OnReopenCheck makes fn hear each check of a reopen after the last
// fabric's removal that left the own window closed, with whether a
// fail-safe was armed and how many sessions were active. It is set before
// the removal that starts the reopen.
func OnReopenCheck(n *Node, fn func(armed bool, sessions int)) { n.reopenChecked = fn }

// The fail-safe hooks a test can leave unwired (WithoutFailSafeHooks):
// CommissioningComplete's commit, a fresh fail-safe's start and the
// fail-safe's expiry, the three paths that clear OperationalCredentials'
// pending state.
const (
	FailSafeHookCommit   = failSafeHookCommit
	FailSafeHookStarting = failSafeHookStarting
	FailSafeHookExpiry   = failSafeHookExpiry
)

// WithoutFailSafeHooks makes the node built from cfg leave hooks unwired.
func WithoutFailSafeHooks(cfg *Config, hooks failSafeHooks) { cfg.withoutFailSafeHooks = hooks }

// FailSafeHooksForTest is the set WithoutFailSafeHooks takes.
type FailSafeHooksForTest = failSafeHooks

// ReopenPending reports whether a reopen after the last fabric's removal
// waits for the node to become idle (reopenWhenIdle).
func ReopenPending(n *Node) bool { return n.reopenPending.Load() }

// HasCASEIdentity reports whether the node answers CASE for fabricIndex.
func HasCASEIdentity(n *Node, fabricIndex uint8) bool {
	_, ok := n.caseIDs.forFabric(fabricIndex)
	return ok
}

// FabricUpdated runs the node's UpdateNOC hook for fabricIndex
// (OperationalCredentials' OnFabricUpdated → fabricUpdated).
func FabricUpdated(ctx context.Context, n *Node, fabricIndex uint8) {
	n.fabricUpdated(ctx, fabricIndex)
}
