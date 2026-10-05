// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Group messaging end to end, in process: a CASE controller provisions a
// group over the wire exactly as a commissioner does (KeySetWrite, a
// GroupKeyMap write, AddGroup on the member lamps), then sends group
// messages the way matter.js's GroupSession.encode seals them. Each
// message enters through the UDP handler — the path a datagram to the
// group's multicast address takes — and the test observes which lamps it
// reached and that nothing was ever sent back.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/ipv6"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// fakeMulticastMember records the memberships the bridge asks for.
type fakeMulticastMember struct {
	mu      sync.Mutex
	joined  map[string]bool
	failing bool
}

func (f *fakeMulticastMember) JoinGroup(ip net.IP) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errors.New("no interface")
	}
	f.joined[ip.String()] = true
	return nil
}

func (f *fakeMulticastMember) LeaveGroup(ip net.IP) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.joined, ip.String())
	return nil
}

func (f *fakeMulticastMember) addresses() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.joined))
	for a := range f.joined {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// groupSender seals group messages for one operational group key, the way
// matter.js GroupSession.encode does.
type groupSender struct {
	t         *testing.T
	opKey     []byte
	sessionID uint16
	source    uint64
	counter   uint32
	exchange  uint16
}

func newGroupSender(t *testing.T, epochKey []byte) *groupSender {
	t.Helper()
	op, err := groups.OperationalKey(epochKey, harnessCompressedID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := groups.SessionID(op)
	if err != nil {
		t.Fatal(err)
	}
	return &groupSender{t: t, opKey: op, sessionID: sid, source: harnessControllerNodeID, counter: 0x0100_0000, exchange: 0x4000}
}

// seal builds one privacy-protected group message to groupID carrying an
// IM message with opcode and payload.
func (g *groupSender) seal(groupID uint16, opcode uint8, payload []byte) []byte {
	g.t.Helper()
	g.counter++
	g.exchange++
	proto := message.ProtocolHeader{Initiator: true, Opcode: opcode, ExchangeID: g.exchange, ProtocolID: im.InteractionModelProtocolID}
	plain := append(proto.Marshal(), payload...)
	hdr := message.Header{
		SessionID: g.sessionID, MessageCounter: g.counter,
		HasSourceNodeID: true, SourceNodeID: g.source,
		DestSize: message.DestGroup, DestGroupID: groupID,
		SessionType: message.SessionGroup, Privacy: true,
	}
	raw := hdr.Marshal()
	nonce := make([]byte, aesccm.NonceSize)
	nonce[0] = raw[3]
	binary.LittleEndian.PutUint32(nonce[1:5], g.counter)
	binary.LittleEndian.PutUint64(nonce[5:13], g.source)
	ccm, err := aesccm.New(g.opKey)
	if err != nil {
		g.t.Fatal(err)
	}
	sealed, err := ccm.Seal(nil, nonce, plain, raw)
	if err != nil {
		g.t.Fatal(err)
	}
	out := append(slices.Clone(raw), sealed...)
	pk, err := groups.PrivacyKey(g.opKey)
	if err != nil {
		g.t.Fatal(err)
	}
	region := out[4:len(raw)]
	mask, err := channel.PrivacyKeystream(pk, g.sessionID, out[len(out)-aesccm.TagSize:], len(region))
	if err != nil {
		g.t.Fatal(err)
	}
	if err := channel.ApplyPrivacyMask(mask, region); err != nil {
		g.t.Fatal(err)
	}
	return out
}

// groupInvoke encodes an InvokeRequest for a wildcard-endpoint command.
func groupInvoke(t *testing.T, cluster, command uint32, concreteEndpoint *uint16) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), true)  // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	path := im.ConcreteCommandPath{Cluster: cluster, Command: command, HasCluster: true, HasCommand: true}
	if concreteEndpoint != nil {
		path.Endpoint, path.HasEndpoint = *concreteEndpoint, true
	}
	path.MarshalTLV(enc, tlv.ContextTag(0))
	enc.StartStruct(tlv.ContextTag(1))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// groupWriteIdentifyTime encodes a SuppressResponse WriteRequest of
// Identify.IdentifyTime on a wildcard endpoint.
func groupWriteIdentifyTime(t *testing.T, seconds uint64, suppress bool) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), suppress)
	enc.PutBool(tlv.ContextTag(1), false)
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartList(tlv.ContextTag(1))
	enc.PutUint(tlv.ContextTag(3), 0x0003)
	enc.PutUint(tlv.ContextTag(4), 0x0000)
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(2), seconds)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// deliver hands a datagram to the bridge's UDP handler, as the socket does
// for a datagram sent to a joined multicast address, from the
// controller's address.
func (gh *groupsHarness) deliver(datagram []byte) {
	src, _ := gh.conn.LocalAddr().(*net.UDPAddr)
	gh.bridge.handleDatagram(datagram, src)
}

// expectSilence asserts the bridge sent the controller nothing at all — a
// group message gets no response, no StatusResponse and no MRP ack.
func (gh *groupsHarness) expectSilence(t *testing.T) {
	t.Helper()
	_ = gh.conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	buf := make([]byte, 2048)
	if n, _, err := gh.conn.ReadFromUDP(buf); err == nil {
		t.Fatalf("the bridge answered a group message with a %d-byte datagram", n)
	}
}

func (gh *groupsHarness) counts() map[uint16]int {
	out := make(map[uint16]int, len(gh.lamps))
	for id, l := range gh.lamps {
		out[id] = len(l.received())
	}
	return out
}

func (gh *groupsHarness) identifyTime(t *testing.T, ep uint16) uint16 {
	t.Helper()
	for _, srv := range endpointClusterServers(gh.bridge, ep) {
		if srv.MatterClusterID() == 0x0003 {
			v, _ := srv.MatterRead(0x0000)
			n, _ := v.(uint16)
			return n
		}
	}
	t.Fatalf("endpoint %d has no Identify", ep)
	return 0
}

func TestGroupMessagingEndToEnd(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 3)
	member := &fakeMulticastMember{joined: map[string]bool{}}
	gh.bridge.startGroupNetworking(member)
	gh.bridge.AttachGroupMessaging(gh.groups)

	lamps := gh.lampIDs()
	kitchenKey := bytes.Repeat([]byte{0xA1}, 16)
	gh.provisionGroup(t, 0x01A1, kitchenKey, 0x0101, "Kitchen", lamps[0], lamps[1])

	// Group 0x0101 has members: its multicast address is joined.
	kitchenAddr := groups.MulticastAddress(harnessFabricID, 0x0101).String()
	if got := member.addresses(); !slices.Equal(got, []string{kitchenAddr}) {
		t.Fatalf("joined %v, want [%s]", got, kitchenAddr)
	}

	// Access control: the controller keeps Administer over CASE; group
	// 0x0101 may Operate.
	if err := gh.store.ReplaceACL(context.Background(), gh.fabric, []store.ACLEntry{
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{harnessControllerNodeID}},
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup, Subjects: []uint64{0x0101}},
	}); err != nil {
		t.Fatal(err)
	}

	sender := newGroupSender(t, kitchenKey)
	toggle := groupInvoke(t, 0x0006, 0x02, nil)

	// A group Toggle reaches exactly the two member lamps, and nothing
	// comes back.
	first := sender.seal(0x0101, im.OpcodeInvokeRequest, toggle)
	gh.deliver(first)
	gh.expectSilence(t)
	if got := gh.counts(); got[lamps[0]] != 1 || got[lamps[1]] != 1 || got[lamps[2]] != 0 {
		t.Fatalf("after one group Toggle, commands per lamp = %v, want one each on %d and %d only", got, lamps[0], lamps[1])
	}

	// The same datagram again is a replay: dropped.
	gh.deliver(first)
	gh.expectSilence(t)
	if got := gh.counts(); got[lamps[0]] != 1 || got[lamps[1]] != 1 {
		t.Fatalf("a replayed group message was executed: %v", got)
	}

	// A fresh counter goes through again.
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[0]] != 2 || got[lamps[1]] != 2 || got[lamps[2]] != 0 {
		t.Fatalf("after a second Toggle: %v", got)
	}

	// Group writes: IdentifyTime with SuppressResponse reaches the members.
	gh.deliver(sender.seal(0x0101, im.OpcodeWriteRequest, groupWriteIdentifyTime(t, 30, true)))
	gh.expectSilence(t)
	if gh.identifyTime(t, lamps[0]) == 0 || gh.identifyTime(t, lamps[1]) == 0 || gh.identifyTime(t, lamps[2]) != 0 {
		t.Fatalf("group write: IdentifyTime %d/%d/%d, want members identifying only",
			gh.identifyTime(t, lamps[0]), gh.identifyTime(t, lamps[1]), gh.identifyTime(t, lamps[2]))
	}

	// Forbidden shapes are dropped without effect and without answer.
	concrete := lamps[0]
	for name, datagram := range map[string][]byte{
		"concrete endpoint invoke":       sender.seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, 0x0006, 0x02, &concrete)),
		"read request":                   sender.seal(0x0101, im.OpcodeReadRequest, buildIMReadRequestPayload(t)),
		"write without SuppressResponse": sender.seal(0x0101, im.OpcodeWriteRequest, groupWriteIdentifyTime(t, 0, false)),
		"wrong key":                      newGroupSender(t, bytes.Repeat([]byte{0x55}, 16)).seal(0x0101, im.OpcodeInvokeRequest, toggle),
		"unmapped group, same key":       sender.seal(0x0909, im.OpcodeInvokeRequest, toggle),
		"tampered ciphertext":            tamper(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle)),
		"manage command beyond Operate":  sender.seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, mattercore.GroupsClusterID, 0x04, nil)),
	} {
		before := gh.counts()
		gh.deliver(datagram)
		gh.expectSilence(t)
		if got := gh.counts(); !mapsEqual(got, before) {
			t.Fatalf("%s: lamps changed %v → %v", name, before, got)
		}
	}
	if gh.identifyTime(t, lamps[0]) == 0 {
		t.Fatal("the write without SuppressResponse cleared IdentifyTime")
	}
	// RemoveAllGroups needs Manage; the Group entry grants Operate, so the
	// administer-command case above left the membership in place.
	if table, _ := gh.groups.GroupTable(context.Background(), gh.fabric); len(table) != 1 || len(table[0].Endpoints) != 2 {
		t.Fatalf("a group command beyond the Group privilege changed membership: %+v", table)
	}

	// ACL denial: group 0x0202 holds lamp 3, but no Group entry covers it.
	hallKey := bytes.Repeat([]byte{0xB2}, 16)
	gh.provisionGroupOnly(t, 0x02B2, hallKey, 0x0202, lamps[2])
	hall := newGroupSender(t, hallKey)
	gh.deliver(hall.seal(0x0202, im.OpcodeInvokeRequest, toggle))
	gh.expectSilence(t)
	if got := gh.counts(); got[lamps[2]] != 0 {
		t.Fatalf("a group the ACL does not grant toggled lamp %d", lamps[2])
	}
	if got := member.addresses(); len(got) != 2 {
		t.Fatalf("joined %v, want both group addresses", got)
	}
	// Positive control: once a Group entry covers 0x0202 (an empty
	// Subjects list matches any group), the same message shape reaches it.
	if err := gh.store.ReplaceACL(context.Background(), gh.fabric, []store.ACLEntry{
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{harnessControllerNodeID}},
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup},
	}); err != nil {
		t.Fatal(err)
	}
	gh.deliver(hall.seal(0x0202, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[2]] != 1 || got[lamps[0]] != 2 {
		t.Fatalf("with a wildcard Group entry the hall Toggle reached %v, want lamp %d only", got, lamps[2])
	}

	// Membership follows RemoveGroup: lamp 1 leaves the kitchen.
	if _, fields, _, _ := invokeResult(t, gh.invoke(lamps[0], mattercore.GroupsClusterID, 0x03, putUint16Field(0x0101))); fields.mustChild(t, 0).El.Uint != 0 {
		t.Fatal("RemoveGroup failed")
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[0]] != 2 || got[lamps[1]] != 3 {
		t.Fatalf("after RemoveGroup on lamp %d: %v", lamps[0], got)
	}

	// Removing the fabric forgets its groups and leaves their addresses.
	if err := gh.store.RemoveFabric(context.Background(), gh.fabric); err != nil {
		t.Fatal(err)
	}
	gh.bridge.EmitFabricRemoved(gh.fabric)
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("still joined after fabric removal: %v", got)
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[1]] != 3 {
		t.Fatal("a removed fabric's group key still delivered")
	}
}

// provisionGroupOnly provisions a group but writes the GroupKeyMap with
// both the existing kitchen binding and the new one, so neither is lost.
func (gh *groupsHarness) provisionGroupOnly(t *testing.T, keySetID uint16, epochKey []byte, groupID, lamp uint16) {
	t.Helper()
	if _, _, status, _ := invokeResult(t, gh.invoke(0, gkmCluster, gkmCmdKeySetWrite, putKeySet(keySetID, epochKey, 1, 0))); status != 0 {
		t.Fatalf("KeySetWrite: %v", status)
	}
	if status := gh.writeAttribute(0, gkmCluster, 0x0000, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		for _, m := range [][2]uint16{{0x0101, 0x01A1}, {groupID, keySetID}} {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(1), uint64(m[0]))
			enc.PutUint(tlv.ContextTag(2), uint64(m[1]))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	}); status != 0 {
		t.Fatalf("GroupKeyMap write: 0x%02X", status)
	}
	if _, fields, _, _ := invokeResult(t, gh.invoke(lamp, mattercore.GroupsClusterID, 0x00, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(groupID))
		enc.PutUTF8(tlv.ContextTag(1), "")
	})); fields.mustChild(t, 0).El.Uint != 0 {
		t.Fatal("AddGroup failed")
	}
}

// endpointClusterServers returns the live cluster set of one endpoint.
func endpointClusterServers(b *Bridge, id uint16) []contract.ClusterServer {
	topo := b.Topology()
	if topo == nil {
		return nil
	}
	ep := topo.FindByID(id)
	if ep == nil {
		return nil
	}
	return endpoint.ClusterServers(ep)
}

func tamper(b []byte) []byte {
	b[len(b)-1] ^= 0x01
	return b
}

func mapsEqual(a, b map[uint16]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestGroupMessagesAreDroppedWithoutGroupMessaging pins the noop port: a
// bridge with no group messaging attached drops a group message without
// answering, and joins nothing.
func TestGroupMessagesAreDroppedWithoutGroupMessaging(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 1)
	member := &fakeMulticastMember{joined: map[string]bool{}}
	gh.bridge.startGroupNetworking(member)
	key := bytes.Repeat([]byte{0xA1}, 16)
	gh.provisionGroup(t, 0x01A1, key, 0x0101, "", gh.lampIDs()...)
	gh.deliver(newGroupSender(t, key).seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, 0x0006, 0x02, nil)))
	gh.expectSilence(t)
	if got := gh.counts(); got[gh.lampIDs()[0]] != 0 {
		t.Fatal("a group message was executed with no group messaging attached")
	}
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("joined %v with no group messaging attached", got)
	}
	gh.bridge.AttachGroupMessaging(nil) // reverts to the noop, no panic
}

// TestGroupJoinFailureIsRetried pins the reconciler's retry: a join that
// fails is attempted again on the next reconcile and the address stays
// wanted until it succeeds.
func TestGroupJoinFailureIsRetried(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 1)
	member := &fakeMulticastMember{joined: map[string]bool{}, failing: true}
	gh.bridge.startGroupNetworking(member)
	gh.bridge.AttachGroupMessaging(gh.groups)
	gh.provisionGroup(t, 0x01A1, bytes.Repeat([]byte{0xA1}, 16), 0x0101, "", gh.lampIDs()...)
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("joined %v through a failing member", got)
	}
	gh.bridge.groupNetMu.Lock()
	retry := gh.bridge.groupJoinRetry != nil
	gh.bridge.groupNetMu.Unlock()
	if !retry {
		t.Fatal("no retry scheduled after a failed join")
	}
	member.mu.Lock()
	member.failing = false
	member.mu.Unlock()
	gh.bridge.reconcileGroupMemberships()
	if got := gh.bridge.joinedGroupAddresses(); len(got) != 1 {
		t.Fatalf("joined %v after the retry, want the kitchen address", got)
	}
	gh.bridge.stopGroupNetworking()
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("stop left %v joined", got)
	}
}

// TestGroupMessageArrivesOverRealMulticast sends a group Toggle to the
// group's multicast address on the bridge's port through the operating
// system: the socket must have joined the group for the datagram to
// arrive at all. Skips where the host offers no IPv6 multicast route.
func TestGroupMessageArrivesOverRealMulticast(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 1)
	gh.bridge.AttachGroupMessaging(gh.groups)
	key := bytes.Repeat([]byte{0xA1}, 16)
	gh.provisionGroup(t, 0x01A1, key, 0x0101, "", gh.lampIDs()...)
	if err := gh.store.ReplaceACL(context.Background(), gh.fabric, []store.ACLEntry{
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup},
	}); err != nil {
		t.Fatal(err)
	}
	if len(gh.bridge.joinedGroupAddresses()) != 1 {
		t.Skip("the host's interfaces did not join the IPv6 group")
	}
	_, portStr, err := net.SplitHostPort(gh.bridge.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("udp", portStr)
	if err != nil {
		t.Fatal(err)
	}
	datagram := newGroupSender(t, key).seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, 0x0006, 0x02, nil))
	group := groups.MulticastAddress(harnessFabricID, 0x0101)
	lamp := gh.lamps[gh.lampIDs()[0]]
	out, err := net.ListenUDP("udp6", &net.UDPAddr{})
	if err != nil {
		t.Skipf("no IPv6 socket: %v", err)
	}
	defer func() { _ = out.Close() }()
	pc := ipv6.NewPacketConn(out)
	_ = pc.SetMulticastLoopback(true)
	sent := false
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if err := pc.SetMulticastInterface(&ifi); err != nil {
			continue
		}
		if _, err := pc.WriteTo(datagram, nil, &net.UDPAddr{IP: group, Port: port}); err != nil {
			continue
		}
		sent = true
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && len(lamp.received()) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		if len(lamp.received()) > 0 {
			break
		}
	}
	if !sent {
		t.Skip("no interface could send to the IPv6 group")
	}
	if len(lamp.received()) == 0 {
		t.Skip("the multicast datagram did not loop back on this host")
	}
	if got := lamp.received(); len(got) != 1 || got[0] != 0x02 {
		t.Fatalf("lamp received %v over multicast, want one Toggle", got)
	}
}
