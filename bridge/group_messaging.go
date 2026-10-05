// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

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
func (b *Bridge) reconcileGroupMemberships() {
	b.groupNetMu.Lock()
	defer b.groupNetMu.Unlock()
	member := b.groupMember
	if member == nil {
		return // not started: Start reconciles once the socket exists
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	memberships, err := b.groupMessagingPort().Memberships(ctx)
	if err != nil {
		b.logger.Warn("matter.groups.memberships", slog.String("err", err.Error()))
		return
	}
	wanted := make(map[string]net.IP, len(memberships))
	for _, m := range memberships {
		wanted[m.Address.String()] = m.Address
	}
	if b.groupJoined == nil {
		b.groupJoined = make(map[string]net.IP)
	}
	for key, addr := range b.groupJoined {
		if _, ok := wanted[key]; ok {
			continue
		}
		if err := member.LeaveGroup(addr); err != nil {
			b.logger.Warn("matter.groups.leave", slog.String("address", key), slog.String("err", err.Error()))
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
			b.logger.Warn("matter.groups.join",
				slog.String("address", key), slog.String("err", err.Error()),
				slog.Duration("retry_in", groupJoinRetryInterval))
			continue
		}
		b.groupJoined[key] = addr
		b.logger.Debug("matter.groups.joined", slog.String("address", key))
	}
	if failed && b.groupJoinRetry == nil {
		b.groupJoinRetry = time.AfterFunc(groupJoinRetryInterval, func() {
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
// memberships can hang on close.
func (b *Bridge) stopGroupNetworking() {
	b.groupNetMu.Lock()
	defer b.groupNetMu.Unlock()
	if b.groupJoinRetry != nil {
		b.groupJoinRetry.Stop()
		b.groupJoinRetry = nil
	}
	if b.groupMember != nil {
		for key, addr := range b.groupJoined {
			if err := b.groupMember.LeaveGroup(addr); err != nil {
				b.logger.Debug("matter.groups.leave_on_stop", slog.String("address", key), slog.String("err", err.Error()))
			}
		}
	}
	b.groupJoined = nil
	b.groupMember = nil
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
		b.logger.Debug("matter.rx.group.drop", slog.String("src", srcString(src)), slog.String("err", err.Error()))
		if ev, ok := decodeFailureEvent(buf, srcIP, err); ok {
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
		return fmt.Errorf("group message: protocol header consumed %d > %d bytes", bodyOffset, len(msg.Payload))
	}
	if (proto.HasVendorID && proto.VendorID != 0) || proto.ProtocolID != im.InteractionModelProtocolID || !proto.Initiator {
		b.logger.Debug("matter.rx.group.not_interaction",
			slog.Int("protocol_id", int(proto.ProtocolID)), slog.Int("opcode", int(proto.Opcode)))
		return nil
	}
	if action := classifyIMOpcode(proto.Opcode, message.SessionGroup); action != imGateProceed {
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
		report := im.HandleGroupInvoke(gctx, dispatcher, req, schema.IsTimedInvoke)
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
		status = im.HandleGroupWriteRequest(gctx, dispatcher, req)
	default:
		return nil
	}
	b.logger.Debug("matter.rx.group",
		slog.String("src", srcString(src)), slog.Int("fabric_index", int(msg.FabricIndex)),
		slog.Int("group_id", int(msg.GroupID)), slog.Int("opcode", int(proto.Opcode)),
		slog.Bool("valid_mapping", msg.HasValidMapping), slog.String("status", status.String()))
	return nil
}

// decodeFailureEvent is the report of a group message that did not
// authenticate. NoAvailableKey names the fabric and group an unmapped key
// set recovered, FailedAuth nothing but the group id of an unobfuscated
// header; any other failure (a malformed message) is not reported. Mirrors
// matter.js SessionManager.groupSessionFromPacket's catch block.
func decodeFailureEvent(buf []byte, srcIP net.IP, err error) (groups.GroupMessageEvent, bool) {
	ev := groups.GroupMessageEvent{SourceIP: srcIP}
	// The group id is readable from the wire header only without privacy;
	// it can derive the arrival address but is never reported as the
	// authenticated group.
	if len(buf) >= 4 && buf[3]&0x80 == 0 {
		if hdr, _, herr := message.UnmarshalHeader(buf); herr == nil && hdr.DestSize == message.DestGroup {
			ev.HeaderGroupID, ev.HasHeaderGroupID = hdr.DestGroupID, true
		}
	}
	var noKey *groups.NoKeyError
	switch {
	case errors.As(err, &noKey):
		ev.Result = groups.TestResultNoAvailableKey
		if noKey.Authenticated {
			ev.FabricIndex = noKey.FabricIndex
			if !ev.HasHeaderGroupID {
				ev.HeaderGroupID, ev.HasHeaderGroupID = noKey.GroupID, true
			}
		}
	case errors.Is(err, groups.ErrNoKey):
		ev.Result = groups.TestResultNoAvailableKey
	case errors.Is(err, groups.ErrDecrypt):
		ev.Result = groups.TestResultFailedAuth
	default:
		return groups.GroupMessageEvent{}, false
	}
	return ev, true
}

// invokeOutcomeEvents are the reports of a processed group Invoke: one
// Success with AccessAllowed per endpoint the command ran on; without any,
// FailedAuth for a message whose key is not the one its group is mapped
// to, otherwise one Success per requested path whose AccessAllowed is
// false when the group has member endpoints (access control filtered them
// all) and absent when it has none. Mirrors matter.js
// InteractionServer.handleInvokeRequest for a group session.
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
	if !msg.HasValidMapping {
		return []groups.GroupMessageEvent{{
			Result: groups.TestResultFailedAuth, FabricIndex: msg.FabricIndex, SourceIP: srcIP,
		}}
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
