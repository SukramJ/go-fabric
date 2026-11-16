// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// GroupMessaging is the port group communication runs through: it
// authenticates a group message, names the multicast addresses the node
// must listen on, and reports the changes to both. *groups.Manager — the
// same instance GroupKeyManagement and the Groups servers use — satisfies
// it; group membership is stack state, so there is nothing for a host to
// implement here, only an instance to hand over.
type GroupMessaging interface {
	// Decode authenticates a group message ([groups.Manager.Decode]).
	Decode(ctx context.Context, datagram []byte) (*groups.Message, error)
	// Memberships lists the groups with member endpoints and their
	// multicast addresses.
	Memberships(ctx context.Context) ([]groups.Membership, error)
	// OnMembershipChanged registers a callback for a change of the
	// membership set.
	OnMembershipChanged(fn func())
	// OnGroupTableChanged registers a callback for a change of a
	// fabric's group table.
	OnGroupTableChanged(fn func(fabricIndex uint8))
	// ForgetFabric drops a removed fabric's group state.
	ForgetFabric(fabricIndex uint8)
	// ReportGroupMessage hands the outcome of a received group message
	// to whoever observes it — the Groupcast server while a fabric is
	// under GroupcastTesting ([groups.Manager.ReportGroupMessage]).
	ReportGroupMessage(ev groups.GroupMessageEvent)
}

var _ GroupMessaging = (*groups.Manager)(nil)

// errGroupMessagingMissing is what the noop port answers every group
// message with.
var errGroupMessagingMissing = errors.New("bridge: no group messaging attached — group message dropped")

// noopGroupMessaging drops every group message and joins nothing.
type noopGroupMessaging struct{}

func (noopGroupMessaging) Decode(context.Context, []byte) (*groups.Message, error) {
	return nil, errGroupMessagingMissing
}

func (noopGroupMessaging) Memberships(context.Context) ([]groups.Membership, error) {
	return nil, nil
}
func (noopGroupMessaging) OnMembershipChanged(func())                  {}
func (noopGroupMessaging) OnGroupTableChanged(func(uint8))             {}
func (noopGroupMessaging) ForgetFabric(uint8)                          {}
func (noopGroupMessaging) ReportGroupMessage(groups.GroupMessageEvent) {}

// AttachGroupMessaging wires group communication: the bridge then
// authenticates group messages through g and routes a group Invoke or
// Write to the member endpoints, joins the multicast address of every
// group with a member endpoint (and leaves it when the group goes),
// reports GroupKeyManagement.GroupTable changes to subscribers, and hands
// a removed fabric to g. Pass the *groups.Manager GroupKeyManagement was
// configured with. nil reverts to the noop, which drops every group
// message — fails closed, and silently, since a group message is never
// answered. Mirrors matter.js ServerNetworkRuntime installing
// ServerGroupNetworking and the session manager's group-session path.
func (b *Bridge) AttachGroupMessaging(g GroupMessaging) {
	if g == nil {
		g = noopGroupMessaging{}
	}
	b.mu.Lock()
	b.groupMessaging = g
	b.mu.Unlock()
	g.OnMembershipChanged(func() {
		if b.groupMessagingPort() == g {
			b.reconcileGroupMemberships()
		}
	})
	g.OnGroupTableChanged(func(uint8) {
		if b.groupMessagingPort() == g {
			b.markGroupTableDirty()
		}
	})
	b.reconcileGroupMemberships()
}

func (b *Bridge) groupMessagingPort() GroupMessaging {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.groupMessaging == nil {
		return noopGroupMessaging{}
	}
	return b.groupMessaging
}

// markGroupTableDirty tells subscribers GroupKeyManagement.GroupTable
// (endpoint 0, 0x003F/0x0001) changed. matter.js's reactive state does
// this for the attribute; here the change signal comes from the group
// state.
func (b *Bridge) markGroupTableDirty() {
	if m := b.subscriptionManagerLocked(); m != nil {
		m.OnAttributeChanged(im.ConcreteAttributePath{
			Endpoint: 0, Cluster: 0x003F, Attribute: 0x0001,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
	}
}

// multicastMember is the socket side of a group membership —
// *udp.Listener in production.
type multicastMember interface {
	JoinGroup(group net.IP) error
	LeaveGroup(group net.IP) error
}

// groupJoinRetryInterval is how long a failed join waits before the next
// attempt. Mirrors matter.js ServerGroupNetworking JOIN_RETRY_INTERVAL.
const groupJoinRetryInterval = 30 * time.Second

// reconcileGroupMemberships brings the socket's multicast memberships in
// line with the groups that have member endpoints: it leaves addresses no
// group uses any more and joins the missing ones, and schedules a retry
// when a join failed. An address stays joined while any fabric's group
// uses it. Runs serialized. Mirrors matter.js ServerGroupNetworking
// #reconcileUntilSettled / #join / #leave.
//
// A failed join logs at info the first time and at debug while the same
// address keeps failing; the streak ends when the join succeeds or the
// address is no longer wanted, so the next failure logs at info again.
// Mirrors matter.js packages/node/src/behavior/system/network/
// ServerGroupNetworking.ts:39-40 (#failedJoins), :136-140 (an address no
// longer wanted leaves the set) and :159-176 (#join: info on the first
// failure, debug on its repeats, the set cleared on success).
func (b *Bridge) reconcileGroupMemberships() {
	b.groupNetMu.Lock()
	defer b.groupNetMu.Unlock()
	member := b.groupMember
	if member == nil {
		return // not started: Start reconciles once the socket exists
	}
	ctx, cancel := clock.WithTimeout(context.Background(), b.clk(), 5*time.Second)
	defer cancel()
	memberships, err := b.groupMessagingPort().Memberships(ctx)
	if err != nil {
		b.logger.Warn("matter.groups.memberships", slog.Any("err", err))
		return
	}
	wanted := make(map[string]net.IP, len(memberships))
	for _, m := range memberships {
		wanted[m.Address.String()] = m.Address
	}
	if b.groupJoined == nil {
		b.groupJoined = make(map[string]net.IP)
	}
	if b.groupJoinFailed == nil {
		b.groupJoinFailed = make(map[string]struct{})
	}
	for key := range b.groupJoinFailed {
		if _, ok := wanted[key]; !ok {
			delete(b.groupJoinFailed, key)
		}
	}
	for key, addr := range b.groupJoined {
		if _, ok := wanted[key]; ok {
			continue
		}
		if err := member.LeaveGroup(addr); err != nil {
			b.logger.Warn("matter.groups.leave", slog.String("address", key), slog.Any("err", err))
			continue
		}
		delete(b.groupJoined, key)
		b.logger.Debug("matter.groups.left", slog.String("address", key))
	}
	failed := false
	for key, addr := range wanted {
		if _, ok := b.groupJoined[key]; ok {
			continue
		}
		if err := member.JoinGroup(addr); err != nil {
			failed = true
			level := slog.LevelInfo
			if _, streak := b.groupJoinFailed[key]; streak {
				level = slog.LevelDebug
			}
			b.groupJoinFailed[key] = struct{}{}
			b.logger.Log(ctx, level, "matter.groups.join_failed",
				slog.String("address", key), slog.Any("err", err),
				slog.Duration("retry_in", groupJoinRetryInterval))
			continue
		}
		delete(b.groupJoinFailed, key)
		b.groupJoined[key] = addr
		b.logger.Debug("matter.groups.joined", slog.String("address", key))
	}
	if failed && b.groupJoinRetry == nil {
		b.groupJoinRetry = b.clk().AfterFunc(groupJoinRetryInterval, func() {
			b.groupNetMu.Lock()
			b.groupJoinRetry = nil
			b.groupNetMu.Unlock()
			b.reconcileGroupMemberships()
		})
	}
}

// startGroupNetworking hands the started socket to the reconciler and
// joins the current memberships.
func (b *Bridge) startGroupNetworking(member multicastMember) {
	b.groupNetMu.Lock()
	b.groupMember = member
	b.groupNetMu.Unlock()
	b.reconcileGroupMemberships()
}

// stopGroupNetworking leaves every joined address and detaches the
// socket. Mirrors ServerGroupNetworking.close: a socket left with active
// memberships can hang on close. The socket is detached and the joined
// set taken under groupNetMu; the leaves — calls on the network — run
// after it is released (ADR 0026 P6: snapshot, release, call).
func (b *Bridge) stopGroupNetworking() {
	b.groupNetMu.Lock()
	if b.groupJoinRetry != nil {
		b.groupJoinRetry.Stop()
		b.groupJoinRetry = nil
	}
	member, joined := b.groupMember, b.groupJoined
	b.groupJoined = nil
	b.groupJoinFailed = nil
	b.groupMember = nil
	b.groupNetMu.Unlock()
	if member == nil {
		return
	}
	for key, addr := range joined {
		if err := member.LeaveGroup(addr); err != nil {
			b.logger.Debug("matter.groups.leave_on_stop", slog.String("address", key), slog.Any("err", err))
		}
	}
}

// joinedGroupAddresses returns the addresses currently joined (tests).
func (b *Bridge) joinedGroupAddresses() []string {
	b.groupNetMu.Lock()
	defer b.groupNetMu.Unlock()
	out := make([]string, 0, len(b.groupJoined))
	for k := range b.groupJoined {
		out = append(out, k)
	}
	return out
}

// isGroupDatagram reports whether a datagram's Security Flags name a group
// session. The Security Flags byte is never obfuscated.
func isGroupDatagram(buf []byte) bool {
	return len(buf) >= 4 && message.SessionType(buf[3]&secFlagSessionTypeBits) == message.SessionGroup
}

// dispatchGroupMessage is the receive path of a group message. Nothing on
// it ever answers: no response, no StatusResponse and no MRP
// acknowledgement — group sessions do not support MRP (matter.js
// GroupSession.supportsMRP = false) and InteractionMessenger.handleRequest
// sends no status on a group session. A message that fails to
// authenticate, repeats a counter, or is not a group Invoke / Write is
// dropped. The returned error is for logging.
//
// Mirrors matter.js ExchangeManager #receiveMessage (group branch),
// SessionManager.groupSessionFromPacket, InteractionMessenger
// .handleRequest and InteractionServer.handleInvokeRequest /
// handleWriteRequest for a group session.
func (b *Bridge) dispatchGroupMessage(ctx context.Context, buf []byte, src *net.UDPAddr) error {
	port := b.groupMessagingPort()
	var srcIP net.IP
	if src != nil {
		srcIP = src.IP
	}
	msg, err := port.Decode(ctx, buf)
	if err != nil {
		b.logger.Debug("matter.rx.group.drop", slog.String("src", srcString(src)), slog.Any("err", err))
		if ev, ok := decodeFailureEvent(srcIP, err); ok {
			port.ReportGroupMessage(ev)
		}
		return err
	}
	if msg.Duplicate {
		b.logger.Debug("matter.rx.group.duplicate",
			slog.String("src", srcString(src)), slog.Int("group_id", int(msg.GroupID)),
			slog.Uint64("source_node_id", msg.SourceNodeID), slog.Uint64("counter", uint64(msg.Header.MessageCounter)))
		// Mirrors the ExchangeManager DuplicateMessageError branch for a
		// group session: reported as MessageReplay, then dropped.
		port.ReportGroupMessage(groups.GroupMessageEvent{
			Result: groups.TestResultMessageReplay, FabricIndex: msg.FabricIndex,
			GroupID: msg.GroupID, HasGroupID: true, SourceIP: srcIP,
		})
		return nil
	}
	proto, _, err := message.UnmarshalProtocolHeader(msg.Payload)
	if err != nil {
		return fmt.Errorf("group message: %w", err)
	}
	bodyOffset := protocolHeaderSize(proto)
	if bodyOffset > len(msg.Payload) {
		return fmt.Errorf("bridge: group message: protocol header consumed %d > %d bytes", bodyOffset, len(msg.Payload))
	}
	if (proto.HasVendorID && proto.VendorID != 0) || proto.ProtocolID != im.InteractionModelProtocolID || !proto.Initiator {
		b.logger.Debug("matter.rx.group.not_interaction",
			slog.Int("protocol_id", int(proto.ProtocolID)), slog.Int("opcode", int(proto.Opcode)))
		return nil
	}
	if action := classifyIMOpcode(proto.Opcode, message.SessionGroup, 0); action != imGateProceed {
		// Read, Subscribe and Timed are unicast-only (§8.5.7): matter.js
		// throws InvalidAction and, on a group session, sends nothing.
		b.logger.Debug("matter.rx.group.rejected_opcode", slog.Int("opcode", int(proto.Opcode)))
		return nil
	}
	dispatcher := b.Dispatcher()
	if dispatcher == nil {
		return nil
	}
	subject := im.GroupSubject{GroupID: msg.GroupID, HasValidMapping: msg.HasValidMapping, Endpoints: msg.Endpoints}
	dec := tlv.NewDecoder(msg.Payload[bodyOffset:])
	var status im.StatusCode
	switch proto.Opcode {
	case im.OpcodeInvokeRequest:
		req, err := im.UnmarshalInvokeRequestTLV(dec, commandFieldsReader)
		if err != nil {
			return fmt.Errorf("group invoke: %w", err)
		}
		gctx := im.WithGroupSubject(im.WithFabricFilter(ctx, false, msg.FabricIndex), subject)
		report := b.handleGroupInvoke(gctx, dispatcher, req)
		status = report.Status
		for _, ev := range invokeOutcomeEvents(msg, srcIP, report) {
			port.ReportGroupMessage(ev)
		}
	case im.OpcodeWriteRequest:
		req, err := im.UnmarshalWriteRequestTLV(dec, attributeValueReader)
		if err != nil {
			return fmt.Errorf("group write: %w", err)
		}
		gctx := im.WithGroupSubject(im.WithFabricFilter(ctx, true, msg.FabricIndex), subject)
		gctx = im.WithWriteTransaction(gctx, im.NewWriteTransaction(&b.routing.writeHolds))
		// A path naming another node writes nothing: this node's id on
		// the group's fabric, as the unicast write stamps it (review II,
		// R2-P4-imwrite-02).
		gctx = im.WithLocalNodeID(gctx, msg.LocalNodeID)
		// A group write is a write: a host's notifications of the
		// endpoints it reaches are held with it, and the attributes its
		// commits name are reported to subscribers as a unicast write's
		// are — matter.js writes a group path through the same commit
		// (AttributeWriteResponse.ts:278-296, :326-361 → :433-435 at
		// db021da0; Datasource.ts:1456-1457, :1319 →
		// BehaviorBacking.ts:434-437 → ProtocolService.ts:120-130).
		var outcome im.GroupWriteOutcome
		b.holdWrite(writeScopeOf(req), func() []im.WriteResult {
			outcome = im.HandleGroupWrite(gctx, dispatcher, req)
			return outcome.Results
		})
		b.reportWrittenAttributes(outcome.Results)
		status = outcome.Status
	default:
		return nil
	}
	b.logger.Debug("matter.rx.group",
		slog.String("src", srcString(src)), slog.Int("fabric_index", int(msg.FabricIndex)),
		slog.Int("group_id", int(msg.GroupID)), slog.Int("opcode", int(proto.Opcode)),
		slog.Bool("valid_mapping", msg.HasValidMapping), slog.String("status", status.String()))
	return nil
}

// decodeFailureEvent is the report of a group message that no candidate
// key passed: it names no fabric and carries the candidate fabrics and
// the group the message names with its owner, for the listener to report
// on its own fabric ([groups.DecodeError.Event]). Any other failure (a
// malformed message, an unattached port) is not reported. Mirrors matter.js
// SessionManager.groupSessionFromPacket's catch block
// (packages/protocol/src/session/SessionManager.ts:811-820 at db021da0),
// which reports a GroupSessionDecodeError only.
func decodeFailureEvent(srcIP net.IP, err error) (groups.GroupMessageEvent, bool) {
	var rejected *groups.DecodeError
	if !errors.As(err, &rejected) {
		return groups.GroupMessageEvent{}, false
	}
	return rejected.Event(srcIP), true
}

// invokeOutcomeEvents are the reports of a processed group Invoke: one
// Success with AccessAllowed per endpoint the command ran on; without any,
// one Success per requested path whose AccessAllowed is false when the
// group has member endpoints (access control filtered them all) and
// absent when it has none. Mirrors matter.js
// InteractionServer.handleInvokeRequest for a group session. A message
// whose key its group is not mapped to never gets here: it fails in
// [groups.Manager.Decode] and is reported by decodeFailureEvent (matter.js
// GroupSession.ts:431-433 at db021da0). matter.js's FailedAuth branch
// after dispatch covers a GroupKeyMap that changed between decode and
// dispatch (packages/node/src/node/server/InteractionServer.ts:1071-1081
// at db021da0); here the mapping Decode checked is the one the dispatcher
// evaluates (groups.Message.HasValidMapping), so the branch has no case.
func invokeOutcomeEvents(msg *groups.Message, srcIP net.IP, report im.GroupInvokeReport) []groups.GroupMessageEvent {
	if !report.Processed {
		return nil
	}
	base := groups.GroupMessageEvent{
		Result: groups.TestResultSuccess, FabricIndex: msg.FabricIndex,
		GroupID: msg.GroupID, HasGroupID: true, SourceIP: srcIP,
	}
	var out []groups.GroupMessageEvent
	if len(report.Dispatched) > 0 {
		allowed := true
		for _, res := range report.Dispatched {
			ev := base
			ev.HasPath, ev.HasEndpoint = true, res.Path.HasEndpoint
			ev.EndpointID, ev.ClusterID, ev.ElementID = res.Path.Endpoint, res.Path.Cluster, res.Path.Command
			if len(report.Requested) > 0 {
				ev.ClusterID, ev.ElementID = report.Requested[0].Cluster, report.Requested[0].Command
			}
			ev.AccessAllowed = &allowed
			out = append(out, ev)
		}
		return out
	}
	var accessAllowed *bool
	if len(msg.Endpoints) > 0 {
		denied := false
		accessAllowed = &denied
	}
	for _, p := range report.Requested {
		ev := base
		ev.HasPath, ev.HasEndpoint = true, p.HasEndpoint
		ev.EndpointID, ev.ClusterID, ev.ElementID = p.Endpoint, p.Cluster, p.Command
		ev.AccessAllowed = accessAllowed
		out = append(out, ev)
	}
	return out
}

// handleGroupInvoke runs a group command as one commit per cluster it
// reaches, as a unicast invoke runs ([Bridge.snapshotInvokedClusters],
// [Bridge.reportInvokeChanges]): a change its servers make without a
// notification of their own advances the cluster's DataVersion and reaches
// the subscribers (R2-P6-par-0401) — matter.js runs a group invoke per
// endpoint through the same command and commit as a concrete one
// (CommandInvokeResponse.ts:330-358, :493 at db021da0).
func (b *Bridge) handleGroupInvoke(ctx context.Context, d im.Dispatcher, req im.InvokeRequest) im.GroupInvokeReport {
	before := b.snapshotInvokedClusters(ctx, d, req)
	defer b.reportInvokeChanges(ctx, d, before)
	return im.HandleGroupInvoke(ctx, d, req, schema.IsTimedInvoke)
}
