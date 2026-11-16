// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	matterlock "github.com/SukramJ/go-fabric/cluster/lock"
	mattermeasure "github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/reportchunk"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// Errors surfaced by the reply path. They are not returned to the
// UDP handler (which has no error channel); the bridge logs them and
// drops the reply, leaving the commissioner to retry per MRP.
var (
	// ErrReplyEncode wraps any TLV-encoding failure when assembling
	// the response payload.
	ErrReplyEncode = errors.New("bridge: encode")
	// ErrReplyEncrypt wraps any AES-CCM seal failure when sealing the
	// response for an encrypted session.
	ErrReplyEncrypt = errors.New("bridge: encrypt")
	// ErrReplySend wraps any UDP transmit failure.
	ErrReplySend = errors.New("bridge: send")
)

// nextUnsecuredCounter returns a fresh per-bridge monotonic counter
// for SessionID==0 (unsecured) replies. New seeds it at a random 28-bit
// value plus one (see [Bridge.seedCounters]); it wraps freely past 2^32 per
// Matter §4.5.4.4 (the unsecured peer is a transient PASE-only
// commissioner that never reaches counter exhaustion in practice).
func (b *Bridge) nextUnsecuredCounter() uint32 {
	return b.unsecuredCounter.Add(1)
}

// encryptSecureOutbound seals body for sess and refreshes the session's
// Tx activity timestamp on success — the single outbound-activity
// chokepoint shared by every secure send path (IM replies, unsolicited
// subscription reports, standalone acks). Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:562/:814, where
// #notifyActivity(false) accompanies every channel send.
//
// localSessionID is the bridge-local session id (the operational
// manager's map key): at encrypt time hdr.SessionID already carries the
// PEER's view of the session id (PeerSessionID) and must not be used
// for the activity lookup. The graceful CloseSession farewell
// deliberately bypasses this helper — it seals for a session that is
// being torn down, whose entry has already left the manager's table.
func (b *Bridge) encryptSecureOutbound(sess *channel.Session, localSessionID uint16, hdr *message.Header, body []byte) (*channel.EncryptResult, error) {
	enc, err := sess.Encrypt(hdr, securityFlagsByte(hdr), body)
	if err != nil {
		if errors.Is(err, mrp.ErrCounterExhausted) && sess.RolloverDue() {
			b.endSessionAtRollover(localSessionID, sess)
		}
		return nil, err
	}
	b.notifySessionActivity(localSessionID, false)
	if sess.RolloverDue() {
		b.endSessionAtRollover(localSessionID, sess)
	}
	return enc, nil
}

// endSessionAtRollover ends the secure session localSessionID whose
// outbound counter nears exhaustion ([channel.Session.RolloverDue]): the
// peer is told with a CloseSession while the keys still seal, then the
// registry closes the session and runs its close hooks (the subscription
// cascade). Before the CloseSession, what the session's subscriptions owe
// is reported ([subscription.Manager.FlushSession]), bounded by the run's
// handler context, which Stop cancels. Mirrors matter.js
// packages/protocol/src/session/NodeSession.ts:398-412
// #scheduleRolloverClose — initiateClose with `closeSubscriptions(true)`,
// a flush through the session (:406-410, :312-323) — which runs on a
// timer rather than on the stack of the send that consumed the counter;
// here it runs on its own goroutine for the same reason (the sender may
// hold locks the close hooks take). Without a registry the session is
// closed locally.
func (b *Bridge) endSessionAtRollover(localSessionID uint16, sess *channel.Session) {
	b.mu.RLock()
	run := b.started
	reg := b.sessionRegistry
	if run {
		// Added under mu while started: Stop's Wait follows its
		// started=false under the write lock, so no Add races it.
		b.rolloverCloses.Add(1)
	}
	b.mu.RUnlock()
	b.logger.Info("matter.session.rollover_close",
		slog.Int("session_id", int(localSessionID)))
	if !run {
		sess.Close()
		return
	}
	go func() {
		defer b.rolloverCloses.Done()
		b.flushSessionAtRollover(localSessionID)
		b.sendCloseSessionReport(localSessionID, sess)
		if reg == nil {
			sess.Close()
			return
		}
		if err := reg.Close(localSessionID); err != nil {
			b.logger.Debug("matter.session.rollover_close",
				slog.Int("session_id", int(localSessionID)),
				slog.Any("err", err))
		}
	}()
}

// flushSessionAtRollover reports what the subscriptions of the session
// [Bridge.endSessionAtRollover] ends owe, under the run's handler
// context: the close runs on its own goroutine, after the send that
// noticed the rollover returned, so no caller's context bounds it; Stop
// cancels the handler context before it waits for the rollover closes.
//
//nolint:contextcheck // detached by design: the send that noticed the rollover has returned; the run's handler context bounds the flush
func (b *Bridge) flushSessionAtRollover(localSessionID uint16) {
	if m := b.subscriptionManagerLocked(); m != nil {
		m.FlushSession(b.handlerContext(), localSessionID)
	}
}

// sendReply is a thin wrapper around [Bridge.sendReplyOpts] that
// keeps the best-effort default (NeedsAck=false, no MRP
// tracking). Use [Bridge.sendReplyReliable] when an IM response
// must survive a single packet drop — see that method's docstring
// for the obligation contract.
func (b *Bridge) sendReply(
	src net.Addr,
	requestHdr *message.Header,
	requestProto message.ProtocolHeader,
	responseOpcode uint8,
	responsePayload []byte,
) error {
	return b.sendReplyOpts(src, requestHdr, requestProto, responseOpcode, responsePayload, false)
}

// sendReplyReliable is the MRP-tracked variant of [Bridge.sendReply].
// The reply is shipped with NeedsAck=true and registered with the
// outbound-reliable tracker, so the ACK pump retransmits it until
// the peer ACKs or the retransmit cap fires.
//
// Caller contract: the reply must idempotently encode the same
// IM-level result on every retransmit (fresh attribute reads, fresh
// timestamps, etc. are NOT allowed) — otherwise the peer might
// observe two divergent successes for the same exchange. IM
// responses (status / write-response / invoke-response) are pure
// outcome reports and meet the contract trivially.
//
// When the bridge has no AckTracker wired (test paths) the call
// degrades silently to the best-effort path.
func (b *Bridge) sendReplyReliable(
	src net.Addr,
	requestHdr *message.Header,
	requestProto message.ProtocolHeader,
	responseOpcode uint8,
	responsePayload []byte,
) error {
	return b.sendReplyOpts(src, requestHdr, requestProto, responseOpcode, responsePayload, true)
}

// sendReplyOpts assembles + (optionally encrypts +) ships a single
// response datagram. The request's headers drive header assembly:
//
//   - Message: SessionID inherited from request; MessageCounter is a
//     fresh per-session value (Session.Encrypt allocates for
//     encrypted sessions; bridge counter for unsecured).
//   - Protocol: ExchangeID inherited from request; ProtocolID
//     inherited; Opcode is the response opcode; Initiator flipped
//     (we are responding to the peer's exchange-opener); HasAck=true
//     with the request's MessageCounter (acknowledges the request);
//     NeedsAck mirrors `reliable` and only takes effect when the
//     outbound MRP tracker is wired — without a tracker, NeedsAck
//     would block the peer on a reply that nothing rebroadcasts on
//     loss.
//
// When src is nil the function returns ErrReplySend without
// attempting to write — the receive pipeline never invents a src on
// the response path, so a nil src here is a programming error.
func (b *Bridge) sendReplyOpts(
	src net.Addr,
	requestHdr *message.Header,
	requestProto message.ProtocolHeader,
	responseOpcode uint8,
	responsePayload []byte,
	reliable bool,
) error {
	return b.sendReplyTimed(src, requestHdr, requestProto, responseOpcode, responsePayload, reliable, nil)
}

// sendHandshakeReplyReliable is [Bridge.sendReplyReliable] for a
// responder's reply on the unsecured session: peer carries the
// initiator's stated intervals and activity, which the retransmit
// schedule uses instead of the idle default
// ([outboundReliableTracker.trackUnsecured]). The reply is always sent: the
// unsecured session's table never refuses one ([unsecuredEntryLimit]).
func (b *Bridge) sendHandshakeReplyReliable(
	src net.Addr,
	requestHdr *message.Header,
	requestProto message.ProtocolHeader,
	responseOpcode uint8,
	responsePayload []byte,
	peer *handshakePeerTiming,
) error {
	return b.sendReplyTimed(src, requestHdr, requestProto, responseOpcode, responsePayload, true, peer)
}

// sendReplyTimed is [Bridge.sendReplyOpts] with the handshake peer timing
// a session-0 reply's retransmissions follow; nil keeps the idle default.
func (b *Bridge) sendReplyTimed(
	src net.Addr,
	requestHdr *message.Header,
	requestProto message.ProtocolHeader,
	responseOpcode uint8,
	responsePayload []byte,
	reliable bool,
	peer *handshakePeerTiming,
) error {
	if requestProto.ProtocolID == im.InteractionModelProtocolID {
		b.load.imSent.Add(1)
		b.count(contract.MetricIMSent)
	}
	b.mu.RLock()
	listener := b.listener
	sessions := b.sessions
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if listener == nil && !isConnRoute(src) {
		return fmt.Errorf("%w: listener nil", ErrReplySend)
	}
	if isNilRoute(src) {
		return fmt.Errorf("%w: src nil", ErrReplySend)
	}

	// Engage MRP only when both the caller asked for it AND the bridge
	// has a tracker wired — degrading to best-effort on a missing
	// tracker keeps test fixtures (which never wire one) functional.
	// Over a reliable connection MRP is off altogether: no R flag, no
	// piggybacked acknowledgement (matter.js MessageExchange.ts:665,
	// :685-691; transport/mrp UsesMRP).
	usesMRP := routeUsesMRP(src)
	needsAck := reliable && tracker != nil && usesMRP

	respProto := message.ProtocolHeader{
		Initiator:  !requestProto.Initiator,
		HasAck:     usesMRP,
		Opcode:     responseOpcode,
		ExchangeID: requestProto.ExchangeID,
		ProtocolID: requestProto.ProtocolID,
		NeedsAck:   needsAck,
	}
	if usesMRP {
		respProto.AckCounter = requestHdr.MessageCounter
	}
	protoBytes := respProto.Marshal()

	// Body = ProtocolHeader || responsePayload. Encryption (when
	// enabled) seals the body with the request's MessageHeader as
	// AAD; for unsecured the body is shipped verbatim.
	body := append(protoBytes, responsePayload...) //nolint:gocritic // small, single-allocation join

	respHdr := unicastReplyHeader(requestHdr)

	if requestHdr.SessionID == 0 {
		respHdr.SessionID = 0
		counter := b.nextUnsecuredCounter()
		respHdr.MessageCounter = counter
		hdrBytes := respHdr.Marshal()
		datagram := append(hdrBytes, body...) //nolint:gocritic // single-allocation join
		// Tracked before it is sent: the peer's ack can arrive (and be
		// handled on another goroutine) before Send returns, and an ack
		// for an untracked message is lost — the message then goes out
		// again as a spurious retransmission.
		// Always: a full table gives up its oldest entry, never this reply
		// ([unsecuredEntryLimit]).
		if needsAck {
			tracker.trackUnsecured(counter, handshakeKeyOf(requestHdr, requestProto.ExchangeID), datagram, src, b.clk().Now(), peer)
		}
		if err := b.sendRouted(listener, src, 0, datagram); err != nil {
			if needsAck {
				tracker.Untrack(requestHdr.SessionID, counter)
			}
			return fmt.Errorf("%w: %w", ErrReplySend, err)
		}
		return nil
	}

	sess, ok := sessions.Lookup(requestHdr.SessionID)
	if !ok {
		// Session vanished between request and response (rare; usually
		// only happens during a fabric removal that races the
		// in-flight request). Drop the reply.
		return fmt.Errorf("%w: session=%d", ErrReplySend, requestHdr.SessionID)
	}
	// Stamp the peer's view of the SessionID — that's what they
	// expect on inbound. Falls back to requestHdr.SessionID if the
	// peer's id was never captured (legacy CASE path or test
	// fixture); chip-tool will drop in that case but at least the
	// frame is structurally valid.
	respHdr.SessionID = sess.PeerSessionID()
	if respHdr.SessionID == 0 {
		respHdr.SessionID = requestHdr.SessionID
	}
	enc, err := b.encryptSecureOutbound(sess, requestHdr.SessionID, &respHdr, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReplyEncrypt, err)
	}
	// Encrypt allocates the message counter from the session's Tx
	// counter; read it back so the retransmit tracker stores the
	// canonical value the peer will see.
	counter := respHdr.MessageCounter
	hdrBytes := respHdr.Marshal()
	datagram := make([]byte, 0, len(hdrBytes)+len(enc.Ciphertext))
	datagram = append(datagram, hdrBytes...)
	datagram = append(datagram, enc.Ciphertext...)
	// Tracked before it is sent, for the reason above.
	if needsAck {
		tracker.Track(counter, requestHdr.SessionID, requestProto.ExchangeID, datagram, src, b.clk().Now())
	}
	if err := b.sendRouted(listener, src, requestHdr.SessionID, datagram); err != nil {
		if needsAck {
			tracker.Untrack(requestHdr.SessionID, counter)
		}
		return fmt.Errorf("%w: %w", ErrReplySend, err)
	}
	return nil
}

// unicastReplyHeader is the message header of a reply to the unicast
// request requestHdr, before its session id and message counter are set:
// no privacy bit, and on the unsecured session exactly one node id — the
// peer's as the destination, or ours as the source.
func unicastReplyHeader(requestHdr *message.Header) message.Header {
	respHdr := message.Header{
		// SessionID is filled by the caller — for unsecured (==0) it stays 0;
		// for encrypted we look up the session and stamp the peer's
		// view of the SessionID (PeerSessionID) so the peer's table
		// can resolve it. Stamping our own local id (which is what
		// requestHdr.SessionID carries — peer sent that to address
		// us) results in chip-tool dropping the reply silently.
		// No P bit: a unicast message never carries privacy
		// enhancements. Mirrors matter.js MessageExchange.ts:734
		// (`hasPrivacyEnhancements: false, // Not supported on unicast, as
		// in the CHIP SDK`); a request that carried the bit was dropped
		// before it reached here (bridge/privacy.go maybeUnmaskPrivacy, as
		// matter.js refuses hasPrivacyEnhancements off a group session,
		// MessageCodec.ts:250-253).
		Control:      requestHdr.Control,
		HasExtension: false,
		SessionType:  requestHdr.SessionType,
	}
	// Mirror node-id routing: peer is ALWAYS the destination, we
	// (the bridge) are the source. chip-tool's commissioner sets
	// HasSourceNodeID=true on inbound PASE messages with a random
	// non-zero ephemeral NodeID; the bridge's reply MUST echo it as
	// DestNodeID with DestSize=DestNodeID, otherwise chip-tool drops
	// the datagram with "malformed unsecure packet with source
	// 0x...0 destination 0x...0". Per Matter §4.4.1.2.
	//
	// For SECURE unicast (SessionID > 0) Matter §4.4.1.4 says peers
	// resolve identity via the SessionID alone — the Source / Dest
	// NodeID fields are typically NOT set on encrypted traffic, and
	// echoing them confuses chip-tool's secure-receive validator.
	// Restrict the echo to the unsecured pre-PASE path.
	if requestHdr.SessionID == 0 && requestHdr.HasSourceNodeID {
		respHdr.DestSize = message.DestNodeID
		respHdr.DestNodeID = requestHdr.SourceNodeID
	}
	// Bridge's source-node-id is its own ephemeral until CASE
	// installs an operational identity. For SessionID==0 (PASE
	// pre-fabric) HasSourceNodeID stays false while the peer named a
	// source — the reply then carries the destination alone. A peer
	// that addressed us by node id without a source (the responder of a
	// CASE the bridge initiated, bridge/case_initiator.go) gets that id
	// back as our source: an unsecured message carries exactly one of
	// the two node ids (matter.js MessageCodec.ts:234-239 at db021da0).
	if requestHdr.SessionID == 0 && !requestHdr.HasSourceNodeID && requestHdr.DestSize == message.DestNodeID {
		respHdr.HasSourceNodeID = true
		respHdr.SourceNodeID = requestHdr.DestNodeID
	}
	return respHdr
}

// EncodeReportData serialises rd into the TLV bytes that ride as the
// IM ReportData payload. Uses [defaultAttributeValueWriter] for
// every attribute value, which writes each value by its Go type: the
// scalars and strings, the raw TLV of a list a cluster encodes itself,
// and the struct values it names.
func EncodeReportData(rd im.ReportData) ([]byte, error) {
	enc := tlv.NewEncoder()
	rd.MarshalTLV(enc, defaultAttributeValueWriter)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: report data: %w", ErrReplyEncode, err)
	}
	return buf, nil
}

// reportChunkPayloadBudget is the per-datagram budget for the
// IM ReportData payload alone — the raw TLV bytes that ride inside
// one Matter message. It targets the IPv6 minimum MTU (1280 bytes,
// Matter Core Spec §4.4.4) as a soft budget: subtracting the message
// header (~16 B), protocol header (~8 B), and AES-CCM tag (16 B) plus
// a bit of headroom leaves ~1100 B for the encoded ReportData. This is
// a soft target, not the wire's hard ceiling — [udp.MaxDatagramSize]
// (2048 B) is the hard limit [udp.Listener.Send] enforces; see
// [reportChunkHardCap] for the entries this budget alone cannot
// bound. Apple Home commissioner reads (PartsList, ServerList,
// FeatureMap, …) routinely breach this budget by an order of
// magnitude, so the read handler now splits oversized reports into
// multiple chunks via [chunkReportData].
const reportChunkPayloadBudget = 1100

// reportChunkHardCap is the absolute ceiling a single AttributeReport
// or EventReport may reach once it is alone in its own chunk.
// [udp.MaxDatagramSize] is what [udp.Listener.Send] enforces on the
// wire; the margin subtracted here covers the message header (~16 B),
// protocol header (~8 B), and AES-CCM tag (16 B) that wrap the IM
// payload before it reaches the socket, plus headroom. A value that
// breaches this on its own cannot be sent at all — chunkReportData
// downgrades it to an AttributeStatusIB/EventStatusIB(ResourceExhausted)
// instead of shipping (and Send rejecting) an oversized datagram.
const reportChunkHardCap = udp.MaxDatagramSize - 200

// chunkLimits are the per-message budget and the hard ceiling for the
// chunkers, for one route: the UDP constants, or over a reliable
// connection the message limit of [Bridge.connMessageLimit] less a
// 200-byte margin for the headers and the AES-CCM tag, as for
// [reportChunkHardCap]: matter.js sizes report chunks by the exchange's
// maxPayloadSize, the channel's less MATTER_MESSAGE_OVERHEAD
// (MessageExchange.ts:465-468 at db021da0).
type chunkLimits struct {
	budget, hardCap int
}

var udpChunkLimits = chunkLimits{budget: reportChunkPayloadBudget, hardCap: reportChunkHardCap}

// SessionTransportResolver resolves a session id to the transport
// parameters its peer stated in the handshake (SupportedTransports and
// MaxTcpMessageSize after the Matter 1.5 gate, sessionparams); ok is false
// when the session is unknown or stated none.
type SessionTransportResolver interface {
	TransportParametersFor(sessionID uint16) (transports uint16, maxTCPMessageSize uint32, ok bool)
}

// chunkLimitsFor picks the chunk limits for a reply to sessionID over dst.
// Over a connection both stay within its message limit: a limit above the
// UDP hard cap is the budget and the cap, a smaller one caps the UDP
// budget and is the hard cap.
func (b *Bridge) chunkLimitsFor(dst net.Addr, sessionID uint16) chunkLimits {
	size, ok := b.connMessageLimit(dst, sessionID)
	if !ok {
		return udpChunkLimits
	}
	n := size - 200
	if n > reportChunkHardCap {
		return chunkLimits{budget: n, hardCap: n}
	}
	return chunkLimits{budget: min(reportChunkPayloadBudget, n), hardCap: n}
}

// maxUDPMessageSize is matter.js MAX_UDP_MESSAGE_SIZE, 1280 - 48
// (packages/general/src/net/udp/UdpSocket.ts:12 at db021da0): the floor
// of a peer's TCP message limit.
const maxUDPMessageSize = 1280 - 48

// connMessageLimit is the largest Matter message — headers and MIC
// included — one send on route may carry for sessionID; ok is false for a
// UDP route and for a connection that names no maximum of its own, whose
// replies keep the UDP limits. Over TCP it is the connection's maximum
// bounded by the peer's: matter.js MessageChannel.maxPayloadSize takes
// Math.min(channelMax, session.peerMaxTcpMessageSize) on TCP and the
// channel's maximum otherwise (packages/protocol/src/protocol/MessageChannel.ts:86-92),
// and peerMaxTcpMessageSize is max(maxTcpMessageSize ?? 64000,
// MAX_UDP_MESSAGE_SIZE) (packages/protocol/src/session/Session.ts:237-239,
// SessionParameters.ts:187) at db021da0. The peer's size is the one its
// session states ([SessionTransportResolver]); a session that states none
// takes the 64000 fallback.
func (b *Bridge) connMessageLimit(route net.Addr, sessionID uint16) (int, bool) {
	r, ok := route.(*connRoute)
	if !ok || r == nil || r.conn == nil || !r.conn.Reliable() {
		return 0, false
	}
	m, ok := r.conn.(interface{ MaxMessageSize() int })
	if !ok || m.MaxMessageSize() <= 0 {
		return 0, false
	}
	channelMax := m.MaxMessageSize()
	if connNetwork(r.conn) != "tcp" {
		return channelMax, true
	}
	peer := int(sessionparams.DefaultMaxTCPMessageSize)
	b.mu.RLock()
	lookup := b.sessions
	b.mu.RUnlock()
	if res, ok := lookup.(SessionTransportResolver); ok {
		if _, stated, known := res.TransportParametersFor(sessionID); known && stated > 0 {
			peer = int(stated)
		}
	}
	return min(channelMax, max(peer, maxUDPMessageSize)), true
}

// sendRouted is [sendVia] for a message of the bridge on the session
// sessionID names (this node's id for it; 0 for the unsecured session):
// over TCP a message above the route's limit for that session
// ([Bridge.connMessageLimit]) is refused before the write, with
// [tcp.ErrMessageTooLarge] — the sender splits by the limit, it is never
// cut. matter.js MessageChannel.send throws MessageTooLargeError for an
// encoded message above maxPayloadSize on a TCP channel, before the write
// (packages/protocol/src/protocol/MessageChannel.ts:104-112 at db021da0).
func (b *Bridge) sendRouted(listener *udp.Listener, dst net.Addr, sessionID uint16, datagram []byte) error {
	if r, ok := dst.(*connRoute); ok && r != nil && connNetwork(r.conn) == "tcp" {
		if limit, ok := b.connMessageLimit(dst, sessionID); ok && len(datagram) > limit {
			return fmt.Errorf("%w: %d bytes exceed the limit of %d for session %d", tcp.ErrMessageTooLarge, len(datagram), limit, sessionID)
		}
	}
	return sendVia(listener, dst, datagram)
}

// chunkReportData splits rd into per-datagram chunks whose encoded
// size stays within budget. All chunks except the last carry
// MoreChunkedMessages=true (Matter §10.6.6); a SuppressResponse=true
// only rides on the final chunk because Matter requires the receiver
// to ACK every intermediate chunk with a StatusResponse before the
// next one can be safely processed.
//
// Greedy fill: each AttributeReport / EventReport is appended one at
// a time; whenever the running encode breaches budget the current
// chunk closes and the entry seeds a fresh chunk. A list attribute that
// does not fit one chunk on its own (e.g. a Descriptor.PartsList with
// 1000+ endpoint IDs, the NOCs of several fabrics) is split by
// [splitListAttributeReport] into a REPLACE-ALL plus ListIndex=null
// appends (Matter §10.6.4.3.1, matter.js chunkAttributePayload). Any
// other single entry that does not fit ships in its own chunk; when that
// chunk alone would still exceed [reportChunkHardCap] (the hard wire
// ceiling [udp.Listener.Send] enforces, so an oversized chunk could never
// be sent regardless), the entry is downgraded to an AttributeStatusIB /
// EventStatusIB carrying StatusResourceExhausted for that one path
// instead — the rest of the report, and the SubscribeResponse that
// depends on it, still go out.
func chunkReportData(rd im.ReportData, budget, hardCap int) ([]im.ReportData, error) {
	return reportchunk.Split(rd, budget, hardCap, defaultAttributeValueWriter)
}

// attributeReportSource yields a report's attribute reports one at a
// time ([reportchunk.Source]).
type attributeReportSource = reportchunk.Source

// sliceAttributeReports is the source over reports already built.
func sliceAttributeReports(reports []im.AttributeReport) attributeReportSource {
	return reportchunk.Slice(reports)
}

// reportChunker is the chunker every report of the bridge goes out
// through ([reportchunk.Chunker]), its values written by
// [defaultAttributeValueWriter].
type reportChunker = reportchunk.Chunker

// newReportChunker chunks the reports of template's subscription drawn
// from attributes and then events ([reportchunk.New]).
func newReportChunker(template im.ReportData, budget, hardCap int, attributes attributeReportSource, events func() []im.EventReport) *reportChunker {
	return reportchunk.New(template, budget, hardCap, defaultAttributeValueWriter, attributes, events)
}

// rawListValue is a list attribute value carried as the TLV encodings of
// its members, rawListMember one member ([reportchunk.List],
// [reportchunk.Member]); [defaultAttributeValueWriter] writes them back
// verbatim.
type (
	rawListValue  = reportchunk.List
	rawListMember = reportchunk.Member
)

// applyFabricScope applies rep's fabric scope to its value before the
// report is chunked ([reportchunk.ApplyFabricScope]).
func applyFabricScope(rep im.AttributeReport) im.AttributeReport {
	return reportchunk.ApplyFabricScope(rep, defaultAttributeValueWriter)
}

// splitListAttributeReport splits a list attribute whose report alone
// does not fit budget ([reportchunk.SplitList]).
func splitListAttributeReport(rd im.ReportData, rep im.AttributeReport, budget int) ([]im.AttributeReport, bool) {
	return reportchunk.SplitList(rd, rep, budget, defaultAttributeValueWriter)
}

// EncodeWriteResponse serialises wr into the TLV bytes that ride as
// the IM WriteResponse payload.
func EncodeWriteResponse(wr im.WriteResponse) ([]byte, error) {
	enc := tlv.NewEncoder()
	wr.MarshalTLV(enc)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: write response: %w", ErrReplyEncode, err)
	}
	return buf, nil
}

// EncodeInvokeResponse serialises ir into the TLV bytes that ride as
// the IM InvokeResponse payload. Uses [defaultCommandFieldsWriter]
// for response payloads: each response type it names is written field
// by field, and any other value as the empty struct of a status-only
// command.
func EncodeInvokeResponse(ir im.InvokeResponse) ([]byte, error) {
	enc := tlv.NewEncoder()
	ir.MarshalTLV(enc, defaultCommandFieldsWriter)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: invoke response: %w", ErrReplyEncode, err)
	}
	return buf, nil
}

// EncodeStatusResponse serialises sr into the TLV bytes that ride
// as the IM StatusResponse payload. The bridge emits this in reply
// to a TimedRequest and wherever it answers an interaction with a status
// alone: a request that does not decode, invalid paths, a batch beyond
// its bound, a Subscribe it cannot establish.
func EncodeStatusResponse(sr im.StatusResponse) ([]byte, error) {
	enc := tlv.NewEncoder()
	sr.MarshalTLV(enc)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: status response: %w", ErrReplyEncode, err)
	}
	return buf, nil
}

// EncodeSubscribeResponse serialises sr into the TLV bytes for an
// IM SubscribeResponse payload.
func EncodeSubscribeResponse(sr im.SubscribeResponse) ([]byte, error) {
	enc := tlv.NewEncoder()
	sr.MarshalTLV(enc)
	buf, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: subscribe response: %w", ErrReplyEncode, err)
	}
	return buf, nil
}

// EncodesAttributeValue reports whether the bridge's attribute writer has an
// encoding for a Go value a cluster server returns from MatterRead. A
// value of a type the writer does not know goes on the wire as TLV null
// (the writer's default arm), which a guard over a fleet's servers must
// catch before a controller does; a nil value encodes as null by design
// and reports false too.
func EncodesAttributeValue(v any) bool {
	if v == nil {
		return false
	}
	out, err := AttributeValueTLV(v)
	if err != nil {
		return false
	}
	return !bytes.Equal(out, []byte{byte(tlv.TypeNull)})
}

// AttributeValueTLV returns v, a Go value a cluster server returns from
// MatterRead or emits as an event payload, as the bridge writes it on the
// wire: one anonymous TLV element — the Data of an AttributeDataIB or an
// EventDataIB. A nil value, and one of a type the writer does not know,
// is TLV null, as on the wire.
func AttributeValueTLV(v any) ([]byte, error) {
	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.AnonymousTag(), im.AttributeValue{Value: v})
	return enc.Bytes()
}

// defaultAttributeValueWriter is the wire writer the bridge plugs
// into [im.ReportData.MarshalTLV]. It type-switches on the
// cluster-native Go value and emits the matching TLV element. Null
// or unknown types degrade to a TLV null element so the reply still
// round-trips structurally — controllers see `null` rather than a
// silent drop.
//
// Coverage matches the cluster-native types every cluster server in
// cluster/{core,measurement,...} returns from
// MatterRead. Add a case here when a new cluster server starts
// returning a richer type (struct, list, …).
func defaultAttributeValueWriter(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { //nolint:gocognit,gocyclo,funlen // wire/dispatch table over many attribute/opcode cases
	if v.IsNull || v.Value == nil {
		enc.PutNull(tag)
		return
	}
	// Width selection: each Go uint/int type maps to a Matter spec
	// type with a fixed wire width. matter.js's TlvCodec validates the
	// width (per the cluster schema's typeLength), and Apple Home's
	// HAP service mapper rejects an attribute whose declared type is
	// `uint16`/`uint32` if the encoded TLV uses TypeUnsignedInt1.
	// Root cause: BasicInformation / BridgedDeviceBasicInformation
	// HardwareVersion (uint16) and SoftwareVersion (uint32) values of 1
	// were emitted as 1-byte uints by the magnitude-driven `PutUint(uint64)`
	// path; Apple
	// silently rejects the topology and sends RemoveFabric.
	switch x := v.Value.(type) {
	case rawListValue:
		enc.StartArray(tag)
		for _, m := range x {
			_ = enc.PutRawElement(tlv.AnonymousTag(), m)
		}
		_ = enc.EndContainer()
	case rawListMember:
		_ = enc.PutRawElement(tag, x)
	case bool:
		enc.PutBool(tag, x)
	// Integers at the smallest width that holds the value, as matter.js's
	// TlvUInt/TlvInt encode (packages/types/src/tlv/TlvNumber.ts at
	// 0ceb202d); a reader accepts any width (Matter Core §A.11.1). The
	// hand-written encoders above keep their widths where a controller
	// finding is recorded (by_design.md).
	case uint8:
		enc.PutUint(tag, uint64(x))
	case uint16:
		enc.PutUint(tag, uint64(x))
	case uint32:
		enc.PutUint(tag, uint64(x))
	case uint64:
		enc.PutUint(tag, x)
	case int8:
		enc.PutInt(tag, int64(x))
	case int16:
		enc.PutInt(tag, int64(x))
	case int32:
		enc.PutInt(tag, int64(x))
	case int64:
		enc.PutInt(tag, x)
	case float32:
		enc.PutFloat32(tag, x)
	case float64:
		enc.PutFloat64(tag, x)
	case string:
		enc.PutUTF8(tag, x)
	case tlv.BoundedString:
		// Cluster servers that declare a maximum byte length for a UTF-8
		// attribute return BoundedString from MatterRead so the encoder
		// applies the Matter-spec bound at encode time (trim + log on
		// overflow). See Encoder.PutUTF8Bounded.
		enc.PutUTF8Bounded(tag, x.Value, x.MaxBytes)
	case []byte:
		enc.PutOctets(tag, x)
	case mattercore.BasicCommissioningInfoStruct:
		// GeneralCommissioning attribute 0x0001 (Matter §11.10.5.3).
		// Two uint16 fields under context tags 0 / 1 — encode at the
		// declared width so the wire shape matches the spec table.
		// PutUint(auto) would have downsized values < 256 to uint8 and
		// the IM decoder rejects the type/length mismatch.
		enc.StartStruct(tag)
		enc.PutUint16(tlv.ContextTag(0), x.FailSafeExpiryLengthSeconds)
		enc.PutUint16(tlv.ContextTag(1), x.MaxCumulativeFailsafeSeconds)
		_ = enc.EndContainer()
	case []mattercore.AccessControlEntryStruct:
		// AccessControl.acl (Matter §9.10.4.4). TLV array of
		// AccessControlEntryStruct; each entry is a struct with
		// context-tagged fields:
		//   [1] privilege  enum8
		//   [2] auth-mode  enum8
		//   [3] subjects   nullable list of uint64
		//   [4] targets    nullable list of struct{ [0] cluster, [1] endpoint, [2] device-type }
		//   [254] fabric-index uint8
		// Apple Home's first post-CASE read targets this attribute;
		// returning `null` (the previous default-writer fall-through)
		// surfaces as ACCESS_DENIED on every follow-up read and Apple
		// tears the fabric down via RemoveFabric.
		// The dispatcher withholds another fabric's fabric-sensitive
		// fields from a request (im.MaskFabricScoped); an entry with the
		// deprecated Redacted set is still written as its FabricIndex
		// alone.
		enc.StartArray(tag)
		for _, e := range x {
			enc.StartStruct(tlv.AnonymousTag())
			if e.Redacted { //nolint:staticcheck // SA1019: the deprecated Redacted branch is honoured until its removal
				enc.PutUint(tlv.ContextTag(254), uint64(e.FabricIndex))
				_ = enc.EndContainer()
				continue
			}
			encodeACLEntryFields(enc, e)
			if e.AuxiliaryType != nil {
				enc.PutUint(tlv.ContextTag(5), uint64(*e.AuxiliaryType))
			}
			enc.PutUint(tlv.ContextTag(254), uint64(e.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.AccessControlAuxiliaryEntryStruct:
		// AccessControl.AuxiliaryAcl (0x0007): AccessControlEntryStruct
		// with [5] AuxiliaryType. An entry with the deprecated Redacted
		// set is written as its FabricIndex alone; the dispatcher scopes
		// the fields (im.MaskFabricScoped).
		enc.StartArray(tag)
		for _, e := range x {
			enc.StartStruct(tlv.AnonymousTag())
			if !e.Redacted { //nolint:staticcheck // SA1019: the deprecated Redacted branch is honoured until its removal
				encodeACLEntryFields(enc, e.Entry)
				enc.PutUint(tlv.ContextTag(5), uint64(e.AuxiliaryType))
			}
			enc.PutUint(tlv.ContextTag(254), uint64(e.Entry.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.GroupcastMembershipStruct:
		// Groupcast.Membership (0x0065:0x0000), MembershipStruct:
		//   [0] GroupId u16, [1] Endpoints list<u16> (LN),
		//   [2] KeySetId u16 (fabric-sensitive, left out for another
		//   fabric's entry), [3] HasAuxiliaryAcl bool (LN),
		//   [4] McastAddrPolicy enum8, [254] FabricIndex.
		enc.StartArray(tag)
		for _, m := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(0), uint64(m.GroupID))
			enc.StartArray(tlv.ContextTag(1))
			for _, ep := range m.Endpoints {
				enc.PutUint(tlv.AnonymousTag(), uint64(ep))
			}
			_ = enc.EndContainer()
			if m.KeySetID != nil {
				enc.PutUint(tlv.ContextTag(2), uint64(*m.KeySetID))
			}
			enc.PutBool(tlv.ContextTag(3), m.HasAuxiliaryACL)
			enc.PutUint(tlv.ContextTag(4), uint64(m.McastAddrPolicy))
			enc.PutUint(tlv.ContextTag(254), uint64(m.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.NetworkInterfaceStruct:
		// GeneralDiagnostics.NetworkInterfaces, encoded by the generated
		// NetworkInterface codec (core.NetworkInterfaceList). Without it
		// Apple Home logs "No enumeration/topology dictionary found" +
		// "Nil supported link layer types" and tears the fabric down via
		// RemoveFabric ~5 s after Subscribe-Initial.
		mattercore.NetworkInterfaceList(x).EncodeTLV(enc, tag)
	case []mattercore.FabricDescriptorStruct:
		// OperationalCredentials.Fabrics (Matter §11.18.5.6).
		// fabric-sensitive list of struct; each entry:
		//   [1]   RootPublicKey octet_string<65>
		//   [2]   VendorID      vendor_id (uint16)
		//   [3]   FabricID      fabric_id (uint64)
		//   [4]   NodeID        node_id   (uint64)
		//   [5]   Label         char_string<32>
		//   [254] FabricIndex   fabric_idx (uint8)
		// Without this case the writer falls through to `default:` and
		// emits TLV null — Apple Home then sees CommissionedFabrics=1
		// but Fabrics=null, declares the bridge inconsistent, and
		// tears the fabric down via RemoveFabric.
		enc.StartArray(tag)
		for _, f := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutOctets(tlv.ContextTag(1), f.RootPublicKey)
			enc.PutUint16(tlv.ContextTag(2), f.VendorID)
			enc.PutUint64(tlv.ContextTag(3), f.FabricID)
			enc.PutUint64(tlv.ContextTag(4), f.NodeID)
			enc.PutUTF8(tlv.ContextTag(5), f.Label)
			if len(f.VidVerificationStatement) > 0 {
				enc.PutOctets(tlv.ContextTag(6), f.VidVerificationStatement)
			}
			enc.PutUint(tlv.ContextTag(254), uint64(f.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.NOCStruct:
		// OperationalCredentials.NOCs (Matter §11.18.5.7).
		// fabric-sensitive list of struct:
		//   [1]   NOC          octet_string
		//   [2]   ICAC         octet_string (nullable)
		//   [3]   Vvsc         octet_string (optional; omitted when nil)
		//   [254] FabricIndex  fabric_idx (uint8)
		enc.StartArray(tag)
		for _, n := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutOctets(tlv.ContextTag(1), n.NOC)
			if len(n.ICAC) == 0 {
				enc.PutNull(tlv.ContextTag(2))
			} else {
				enc.PutOctets(tlv.ContextTag(2), n.ICAC)
			}
			if len(n.Vvsc) > 0 {
				enc.PutOctets(tlv.ContextTag(3), n.Vvsc)
			}
			enc.PutUint(tlv.ContextTag(254), uint64(n.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case [][]byte:
		// OperationalCredentials.TrustedRootCertificates (Matter
		// §11.18.5.x): list of octet_string<400>. Also covers any
		// other list-of-octets attribute.
		enc.StartArray(tag)
		for _, b := range x {
			enc.PutOctets(tlv.AnonymousTag(), b)
		}
		_ = enc.EndContainer()
	case []mattercore.AccessControlExtensionEntry:
		// AccessControl.Extension (Matter §9.10.4.6). fabric-sensitive
		// list of AccessControlExtensionStruct:
		//   [1]   Data        octet_string<128>
		//   [254] FabricIndex fabric_idx (uint8)
		// Mirrors matter.js access-control.element.ts
		// AccessControlExtensionStruct (Data id 0x1, FabricIndex id
		// 0xfe). A Go type switch matches the exact dynamic type, so
		// this concrete slice never fell into the `[]any` case below —
		// without this case every read/subscribe of 0x001F:0x0001 fell
		// through to `default:` and returned TLV null regardless of
		// what AccessControl.MatterWrite had stored.
		// An entry with the deprecated Redacted set is written without
		// Data; the dispatcher scopes the fields (im.MaskFabricScoped).
		enc.StartArray(tag)
		for _, e := range x {
			enc.StartStruct(tlv.AnonymousTag())
			if !e.Redacted { //nolint:staticcheck // SA1019: the deprecated Redacted branch is honoured until its removal
				enc.PutOctets(tlv.ContextTag(1), e.Data)
			}
			enc.PutUint(tlv.ContextTag(254), uint64(e.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []any:
		// Empty list pass-through for a cluster that surfaces an
		// empty-list attribute as `[]any{}` (e.g. ScenesManagement,
		// OTASoftwareUpdateRequestor). Returning `null` here would
		// parse as "missing", whereas an empty array is a present,
		// empty value.
		enc.StartArray(tag)
		// Arbitrary `any` items are not encoded; the typical use case is
		// the empty-extension surface where the list is always 0-length.
		// A non-empty one is served empty and said once (R2-P6-err-0447).
		if len(x) > 0 {
			warnUnencodableValue(x)
		}
		_ = enc.EndContainer()
	case []uint32:
		// list of cluster / attribute / command / event IDs. Covers the
		// universal global attributes the dispatcher synthesises
		// (Matter §7.13.2) — `AttributeList` (0xFFFB),
		// `AcceptedCommandList` (0xFFF9), `GeneratedCommandList`
		// (0xFFF8), `EventList` (0xFFFA) — plus Descriptor.ServerList
		// (0x001D:0x0001) and Descriptor.ClientList (0x001D:0x0002).
		//
		// Spec types: AttributeId / ClusterId / EventId / CommandId are
		// all `TlvUInt32`. Apple Home's MTRDevice IM-decoder is strict
		// on element width — mixing TypeUnsignedInt1 (cluster IDs ≤
		// 0xFF like 0x28 BasicInformation) with TypeUnsignedInt2
		// (globals like 0xFFFC FeatureMap) inside the same Array makes
		// Apple's decoder reject the whole list. Use the explicit-width
		// helper so every entry rides as uint32.
		enc.StartArray(tag)
		for _, id := range x {
			enc.PutUint32(tlv.AnonymousTag(), id)
		}
		_ = enc.EndContainer()
	case []uint16:
		// list of endpoint IDs. Used by Descriptor.PartsList
		// (0x001D:0x0003) — the root endpoint enumerates every bridged
		// endpoint here. Spec type: `TlvEndpointNumber = TlvUInt16`.
		// Same explicit-width rationale as the []uint32 case above.
		enc.StartArray(tag)
		for _, id := range x {
			enc.PutUint16(tlv.AnonymousTag(), id)
		}
		_ = enc.EndContainer()
	case []mattercore.NetworkInfoStruct:
		// NetworkCommissioning.Networks (Matter §11.9.6.2). list of
		// struct{ NetworkID octet_string<32>, Connected bool }.
		// Apple Home reads this immediately after GeneralCommissioning;
		// without an explicit case the writer emits null and Apple's
		// IM-decoder discards every subsequent cluster on the
		// subscribe-initial stream — surfacing as Code=24 HAP build
		// failure even though Subscribe-initial reports continue
		// arriving on the wire.
		enc.StartArray(tag)
		for _, ni := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutOctets(tlv.ContextTag(0), ni.NetworkID)
			enc.PutBool(tlv.ContextTag(1), ni.Connected)
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.GroupKeyMapStruct:
		// GroupKeyManagement.GroupKeyMap (Matter §11.2.10.5.1).
		// fabric-sensitive list of struct{ GroupId u16, GroupKeySetId
		// u16, FabricIndex u8 }. Every integer at its smallest width,
		// as matter.js's TlvUInt16 writes it (pinned by the
		// group_key_map fixture in testdata/groupcast-wire-fixtures.json).
		enc.StartArray(tag)
		for _, m := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(1), uint64(m.GroupID))
			enc.PutUint(tlv.ContextTag(2), uint64(m.GroupKeySetID))
			enc.PutUint(tlv.ContextTag(254), uint64(m.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.GroupInfoMapStruct:
		// GroupKeyManagement.GroupTable (Matter §11.2.10.5.2).
		// fabric-sensitive list of struct{ GroupId u16, Endpoints
		// list<u16>, GroupName string, FabricIndex u8 }, every integer
		// at its smallest width as matter.js writes it (group_table
		// fixture).
		enc.StartArray(tag)
		for _, m := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(1), uint64(m.GroupID))
			enc.StartArray(tlv.ContextTag(2))
			for _, ep := range m.Endpoints {
				enc.PutUint(tlv.AnonymousTag(), uint64(ep))
			}
			_ = enc.EndContainer()
			enc.PutUTF8(tlv.ContextTag(3), m.GroupName)
			enc.PutUint(tlv.ContextTag(254), uint64(m.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.TargetStruct:
		// Binding.Binding (Matter §9.6.5.1). list of struct{ Node u64,
		// Group u16, Endpoint u16, Cluster u32, FabricIndex u8 }.
		enc.StartArray(tag)
		for _, t := range x {
			enc.StartStruct(tlv.AnonymousTag())
			if t.Node != 0 {
				enc.PutUint64(tlv.ContextTag(1), t.Node)
			}
			if t.Group != 0 {
				enc.PutUint16(tlv.ContextTag(2), t.Group)
			}
			if t.HasEndpoint() {
				enc.PutUint16(tlv.ContextTag(3), t.Endpoint)
			}
			if t.HasCluster() {
				enc.PutUint32(tlv.ContextTag(4), t.Cluster)
			}
			enc.PutUint(tlv.ContextTag(254), uint64(t.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattercore.DeviceTypeStruct:
		// Descriptor.DeviceTypeList (Matter §9.5.5.1). list of struct{
		//   [0] DeviceType uint32 (vendor + device-type ID),
		//   [1] Revision   uint16
		// }. Mandatory on every endpoint; Apple Home's HAP-mapper
		// keys on this list to pick the HomeKit service template
		// (e.g. ContactSensor, Thermostat). DeviceType is spec
		// `TlvUInt32`, Revision is `TlvUInt16` — explicit widths
		// required for Apple's strict decoder.
		enc.StartArray(tag)
		for _, dt := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint32(tlv.ContextTag(0), dt.DeviceType)
			enc.PutUint16(tlv.ContextTag(1), dt.Revision)
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case []mattermeasure.AccuracyStruct:
		// ElectricalPowerMeasurement.Accuracy (0x0090:0x0002) and
		// ElectricalEnergyMeasurement.Accuracy (0x0091:0x0000): a list of
		// MeasurementAccuracyStruct, encoded by the generated codec
		// (measurement.AccuracyList). EnergyMeasurementStruct is the
		// generated struct itself and takes the spec.Encodable case below.
		mattermeasure.AccuracyList(x).EncodeTLV(enc, tag)
	case matterlock.LockOperationEvent:
		// DoorLock §5.2.10.3 LockOperation. Field tags per matter.js
		// door-lock-cluster.element.ts:181-195:
		//   [0] LockOperationType enum8
		//   [1] OperationSource   enum8
		//   [2] UserIndex         uint16 nullable
		//   [3] FabricIndex       fabric-idx nullable
		//   [4] SourceNode        node-id nullable
		// Credentials [5] is USR-gated and absent (no USR feature).
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.LockOperationType))
		enc.PutUint(tlv.ContextTag(1), uint64(x.OperationSource))
		if x.UserIndex != nil {
			enc.PutUint16(tlv.ContextTag(2), *x.UserIndex)
		} else {
			enc.PutNull(tlv.ContextTag(2))
		}
		if x.FabricIndex != nil {
			enc.PutUint(tlv.ContextTag(3), uint64(*x.FabricIndex))
		} else {
			enc.PutNull(tlv.ContextTag(3))
		}
		if x.SourceNode != nil {
			enc.PutUint64(tlv.ContextTag(4), *x.SourceNode)
		} else {
			enc.PutNull(tlv.ContextTag(4))
		}
		_ = enc.EndContainer()
	case mattercore.AccessControlEntryChangedEvent:
		// AccessControl §9.10.7.1. Field tags per matter.js
		// access-control.element.ts:62-74:
		//   [1] AdminNodeID        node-id (uint64) nullable
		//   [2] AdminPasscodeID    uint16          nullable
		//   [3] ChangeType         enum8
		//   [4] LatestValue        AccessControlEntryStruct nullable
		//   [0xFE] FabricIndex     uint8 (auto-added fabric-scoped tag)
		enc.StartStruct(tag)
		if x.AdminNodeID != nil {
			enc.PutUint64(tlv.ContextTag(1), *x.AdminNodeID)
		} else {
			enc.PutNull(tlv.ContextTag(1))
		}
		if x.AdminPasscodeID != nil {
			enc.PutUint16(tlv.ContextTag(2), *x.AdminPasscodeID)
		} else {
			enc.PutNull(tlv.ContextTag(2))
		}
		enc.PutUint(tlv.ContextTag(3), uint64(x.ChangeType))
		if x.LatestValue != nil {
			lv := *x.LatestValue
			enc.StartStruct(tlv.ContextTag(4))
			enc.PutUint(tlv.ContextTag(1), uint64(lv.Privilege))
			enc.PutUint(tlv.ContextTag(2), uint64(lv.AuthMode))
			if lv.Subjects == nil {
				enc.PutNull(tlv.ContextTag(3))
			} else {
				enc.StartArray(tlv.ContextTag(3))
				for _, s := range lv.Subjects {
					enc.PutUint(tlv.AnonymousTag(), s)
				}
				_ = enc.EndContainer()
			}
			if lv.Targets == nil {
				enc.PutNull(tlv.ContextTag(4))
			} else {
				enc.StartArray(tlv.ContextTag(4))
				for _, t := range lv.Targets {
					enc.StartStruct(tlv.AnonymousTag())
					if t.Cluster != nil {
						enc.PutUint(tlv.ContextTag(0), uint64(*t.Cluster))
					} else {
						enc.PutNull(tlv.ContextTag(0))
					}
					if t.Endpoint != nil {
						enc.PutUint(tlv.ContextTag(1), uint64(*t.Endpoint))
					} else {
						enc.PutNull(tlv.ContextTag(1))
					}
					if t.DeviceType != nil {
						enc.PutUint(tlv.ContextTag(2), uint64(*t.DeviceType))
					} else {
						enc.PutNull(tlv.ContextTag(2))
					}
					_ = enc.EndContainer()
				}
				_ = enc.EndContainer()
			}
			enc.PutUint(tlv.ContextTag(254), uint64(lv.FabricIndex))
			_ = enc.EndContainer()
		} else {
			enc.PutNull(tlv.ContextTag(4))
		}
		enc.PutUint(tlv.ContextTag(254), uint64(x.FabricIndex))
		_ = enc.EndContainer()
	case mattercore.AuxiliaryAccessUpdatedEvent:
		// AccessControl AuxiliaryAccessUpdated (0x0003): [0] AdminNodeId
		// node-id nullable, [0xFE] FabricIndex.
		enc.StartStruct(tag)
		if x.AdminNodeID != nil {
			enc.PutUint(tlv.ContextTag(0), *x.AdminNodeID)
		} else {
			enc.PutNull(tlv.ContextTag(0))
		}
		enc.PutUint(tlv.ContextTag(254), uint64(x.FabricIndex))
		_ = enc.EndContainer()
	case mattercore.GroupcastTestingEvent:
		encodeGroupcastTestingEvent(enc, tag, x)
	case mattercore.AccessControlExtensionChangedEvent:
		// AccessControl §9.10.7.2. Same field tags as
		// AccessControlEntryChanged above, per matter.js
		// access-control.element.ts:84-98, but LatestValue is an
		// AccessControlExtensionStruct ([1] Data octstr, [254]
		// FabricIndex) instead of AccessControlEntryStruct:
		//   [1] AdminNodeID        node-id (uint64) nullable
		//   [2] AdminPasscodeID    uint16          nullable
		//   [3] ChangeType         enum8
		//   [4] LatestValue        AccessControlExtensionStruct nullable
		//   [0xFE] FabricIndex     uint8 (auto-added fabric-scoped tag)
		enc.StartStruct(tag)
		if x.AdminNodeID != nil {
			enc.PutUint64(tlv.ContextTag(1), *x.AdminNodeID)
		} else {
			enc.PutNull(tlv.ContextTag(1))
		}
		if x.AdminPasscodeID != nil {
			enc.PutUint16(tlv.ContextTag(2), *x.AdminPasscodeID)
		} else {
			enc.PutNull(tlv.ContextTag(2))
		}
		enc.PutUint(tlv.ContextTag(3), uint64(x.ChangeType))
		if x.LatestValue != nil {
			lv := *x.LatestValue
			enc.StartStruct(tlv.ContextTag(4))
			enc.PutOctets(tlv.ContextTag(1), lv.Data)
			enc.PutUint(tlv.ContextTag(254), uint64(lv.FabricIndex))
			_ = enc.EndContainer()
		} else {
			enc.PutNull(tlv.ContextTag(4))
		}
		enc.PutUint(tlv.ContextTag(254), uint64(x.FabricIndex))
		_ = enc.EndContainer()
	default:
		if encodeApplicationValue(enc, tag, v.Value) {
			return
		}
		if _, isSceneInfo := v.Value.([]mattercore.SceneInfoStruct); isSceneInfo && encodeScenesResponse(enc, tag, v.Value) {
			return
		}
		// Cluster server returned a Go value the writer does not
		// handle — a struct or a list of a type with no EncodeTLV; a
		// plain slice of an Encodable type took encodeApplicationValue's
		// general slice rule above. Emit null so the reply still
		// parses on the controller side; the cluster server should
		// either downcast to a primitive in MatterRead or supply its
		// own writer in a future iteration. Said once per Go type, so
		// a host server's value that serves null is not silent
		// (R2-P6-err-0447).
		warnUnencodableValue(v.Value)
		enc.PutNull(tag)
	}
}

// valueWriterLogger is the logger of the attribute value writer, a free
// function the IM codec calls with no bridge at hand: the process default.
func valueWriterLogger() *slog.Logger { return slog.Default() }

// unencodableTypes holds the Go types [warnUnencodableValue] has logged.
var unencodableTypes sync.Map

// warnUnencodableValue logs, once per Go type for the process, that a
// cluster server returned a value the attribute writer cannot encode —
// served as null, or a non-empty []any served as an empty list. The
// value types a server may return are [contract.ClusterServer]'s
// MatterRead contract.
func warnUnencodableValue(v any) {
	typ := fmt.Sprintf("%T", v)
	if _, seen := unencodableTypes.LoadOrStore(typ, struct{}{}); seen {
		return
	}
	valueWriterLogger().Warn("matter.attribute.unencodable_value",
		slog.String("go_type", typ),
		slog.String("served_as", map[bool]string{true: "empty list", false: "null"}[typ == "[]interface {}"]))
}

// defaultCommandFieldsWriter is the wire writer the bridge plugs
// into [im.InvokeResponse.MarshalTLV]. Type-switches on the
// cluster-native response struct returned by [contract.ClusterServer.MatterInvoke]
// and emits the matching TLV. Commands without response fields (the
// status-only OnOff / LevelControl / etc. clusters) hit the
// `default` arm and emit an empty Structure — chip-tool's
// IM decoder accepts that for status-only command IDs.
//
// Add a case here whenever a new cluster command starts producing a
// rich response struct.
func defaultCommandFieldsWriter(enc *tlv.Encoder, tag tlv.Tag, v any) { //nolint:funlen // a dispatch table: one case per response type
	switch x := v.(type) {
	case mattercore.ArmFailSafeResponse:
		// Matter §11.10.6.3 — [0] enum8 ErrorCode, [1] string DebugText.
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.ErrorCode))
		enc.PutUTF8(tlv.ContextTag(1), x.DebugText)
		_ = enc.EndContainer()
	case mattercore.SetRegulatoryConfigResponse:
		// Matter §11.10.6.5 — same shape as ArmFailSafeResponse.
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.ErrorCode))
		enc.PutUTF8(tlv.ContextTag(1), x.DebugText)
		_ = enc.EndContainer()
	case mattercore.CommissioningCompleteResponse:
		// Matter §11.10.6.7 — same shape.
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.ErrorCode))
		enc.PutUTF8(tlv.ContextTag(1), x.DebugText)
		_ = enc.EndContainer()
	case mattercore.AttestationResponse:
		// Matter §11.18.7.2 — [0] AttestationElements, [1] AttestationSignature.
		enc.StartStruct(tag)
		enc.PutOctets(tlv.ContextTag(0), x.AttestationElements)
		enc.PutOctets(tlv.ContextTag(1), x.AttestationSignature)
		_ = enc.EndContainer()
	case mattercore.CertificateChainResponse:
		// Matter §11.18.7.4 — [0] Certificate.
		enc.StartStruct(tag)
		enc.PutOctets(tlv.ContextTag(0), x.Certificate)
		_ = enc.EndContainer()
	case mattercore.CSRResponse:
		// Matter §11.18.7.6 — [0] NOCSRElements, [1] AttestationSignature.
		enc.StartStruct(tag)
		enc.PutOctets(tlv.ContextTag(0), x.NOCSRElements)
		enc.PutOctets(tlv.ContextTag(1), x.AttestationSignature)
		_ = enc.EndContainer()
	case mattercore.NOCResponse:
		// Matter §11.18.7.9 — [0] StatusCode, [1] FabricIndex (optional),
		// [2] DebugText (optional).
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.StatusCode))
		if x.FabricIndex != 0 {
			enc.PutUint(tlv.ContextTag(1), uint64(x.FabricIndex))
		}
		if x.DebugText != "" {
			enc.PutUTF8(tlv.ContextTag(2), x.DebugText)
		}
		_ = enc.EndContainer()
	case mattercore.SignVidVerificationResponse:
		// SignVIDVerificationResponse (operational-credentials.element.ts
		// 0x0E): [0] FabricIndex, [1] FabricBindingVersion, [2] Signature.
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.FabricIndex))
		enc.PutUint(tlv.ContextTag(1), uint64(x.FabricBindingVersion))
		enc.PutOctets(tlv.ContextTag(2), x.Signature)
		_ = enc.EndContainer()
	case mattercore.KeySetReadResponse:
		// Matter §11.2.7.3 — [0] GroupKeySetStruct GroupKeySet.
		enc.StartStruct(tag)
		encodeGroupKeySetStruct(enc, tlv.ContextTag(0), x.GroupKeySet)
		_ = enc.EndContainer()
	case mattercore.KeySetReadAllIndicesResponse:
		// Matter §11.2.7.6 — [0] list<uint16> GroupKeySetIDs.
		enc.StartStruct(tag)
		enc.StartArray(tlv.ContextTag(0))
		for _, id := range x.GroupKeySetIDs {
			enc.PutUint(tlv.AnonymousTag(), uint64(id))
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	default:
		if encodeGroupsResponse(enc, tag, v) || encodeGroupcastResponse(enc, tag, v) || encodeScenesResponse(enc, tag, v) || encodeApplicationResponse(enc, tag, v) {
			return
		}
		// Status-only command — emit empty struct as the TLV
		// placeholder chip-tool's status-only decoder accepts.
		enc.StartStruct(tag)
		_ = enc.EndContainer()
	}
}

// debugReplyError is a small helper that turns reply-path errors
// into structured slog records. Each error gets its own attribute
// list so operators can grep for the specific failure stage.
func debugReplyError(logger *slog.Logger, stage string, src net.Addr, err error) {
	if err == nil {
		return
	}
	logger.Debug("matter.tx.reply",
		slog.String("stage", stage),
		slog.String("src", srcString(src)),
		slog.Any("err", err))
}

// encodeACLEntryFields writes fields 1-4 of an AccessControlEntryStruct.
func encodeACLEntryFields(enc *tlv.Encoder, e mattercore.AccessControlEntryStruct) {
	enc.PutUint(tlv.ContextTag(1), uint64(e.Privilege))
	enc.PutUint(tlv.ContextTag(2), uint64(e.AuthMode))
	if e.Subjects == nil {
		enc.PutNull(tlv.ContextTag(3))
	} else {
		enc.StartArray(tlv.ContextTag(3))
		for _, s := range e.Subjects {
			enc.PutUint(tlv.AnonymousTag(), s)
		}
		_ = enc.EndContainer()
	}
	if e.Targets == nil {
		enc.PutNull(tlv.ContextTag(4))
		return
	}
	enc.StartArray(tlv.ContextTag(4))
	for _, t := range e.Targets {
		enc.StartStruct(tlv.AnonymousTag())
		if t.Cluster != nil {
			enc.PutUint(tlv.ContextTag(0), uint64(*t.Cluster))
		} else {
			enc.PutNull(tlv.ContextTag(0))
		}
		if t.Endpoint != nil {
			enc.PutUint(tlv.ContextTag(1), uint64(*t.Endpoint))
		} else {
			enc.PutNull(tlv.ContextTag(1))
		}
		if t.DeviceType != nil {
			enc.PutUint(tlv.ContextTag(2), uint64(*t.DeviceType))
		} else {
			enc.PutNull(tlv.ContextTag(2))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// encodeGroupcastTestingEvent writes the Groupcast GroupcastTesting event
// (groupcast.element.ts): [0] SourceIpAddress, [1] DestinationIpAddress
// (ipv6adr), [2] GroupId, [3] EndpointId, [4] ClusterId, [5] ElementId,
// [6] AccessAllowed — each optional and left out when absent —
// [7] GroupcastTestResult and [0xFE] FabricIndex.
func encodeGroupcastTestingEvent(enc *tlv.Encoder, tag tlv.Tag, x mattercore.GroupcastTestingEvent) {
	enc.StartStruct(tag)
	if x.SourceIPAddress != nil {
		enc.PutOctets(tlv.ContextTag(0), x.SourceIPAddress)
	}
	if x.DestinationIPAddress != nil {
		enc.PutOctets(tlv.ContextTag(1), x.DestinationIPAddress)
	}
	if x.GroupID != nil {
		enc.PutUint(tlv.ContextTag(2), uint64(*x.GroupID))
	}
	if x.EndpointID != nil {
		enc.PutUint(tlv.ContextTag(3), uint64(*x.EndpointID))
	}
	if x.ClusterID != nil {
		enc.PutUint(tlv.ContextTag(4), uint64(*x.ClusterID))
	}
	if x.ElementID != nil {
		enc.PutUint(tlv.ContextTag(5), uint64(*x.ElementID))
	}
	if x.AccessAllowed != nil {
		enc.PutBool(tlv.ContextTag(6), *x.AccessAllowed)
	}
	enc.PutUint(tlv.ContextTag(7), uint64(x.GroupcastTestResult))
	enc.PutUint(tlv.ContextTag(254), uint64(x.FabricIndex))
	_ = enc.EndContainer()
}
