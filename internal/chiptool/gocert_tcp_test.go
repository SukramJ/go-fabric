// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
)

// The TCP cases: matter.js's TC-SC-8.x run the device as the DUT and a
// helper controller as the TH over TCP (support/chip-testing/test/cert/
// tc-sc-8-support.ts:36-54 at 862ad75f, TCP_ROLES), the direction of the
// Go-TH leg: the reference daemon is the DUT — its --tcp default "true"
// accepts TCP on the Matter port (examples/reference-bridge/main.go,
// tcp.go), so no daemonArgs —, the Go controller the TH, built with
// TCP.Outgoing and TCP preferred (controller.TCPConfig,
// Config.TransportPreference), as matter.js's controller prefers TCP for
// every session once asked (:56-62). matter.js reads the DUT's log for the
// session's TCP tag; here a step's evidence is the TH's own sessions
// (peer.Peer.Sessions: the transport, the payload limit, the peer's
// session id) and the daemon's log of the session — its CASE session
// established, then served over a tcp:// connection.

func init() {
	for _, c := range []func() goCertCase{caseSC81, caseSC82, caseSC83, caseSC84, caseSC85, caseSC86, caseSC87} {
		registerGoCert(c)
	}
}

// tcpPICS is TCP_PICS (tc-sc-8-support.ts:48). The daemon's hand PICS
// state the capability as CHIP names it, MCORE.SC.S.TCP=1
// (testdata/reference-bridge.pics); MCORE.SC.TCP is matter.js's name for
// the same claim (support/chip-testing/patched-test-files/certification/
// ci-pics-values:2061, packages/testing/src/chip/
// matter-js-pics.properties:124), which no case of the CHIP image at
// 6170af84 names. The case declares it from the daemon's own statement
// (tcpDeclared checks that it holds).
var (
	tcpPICS    = []string{"MCORE.SC.TCP"}
	tcpDeclare = map[string]string{"MCORE.SC.TCP": "1"}
)

// largePayloadFloor is LARGE_PAYLOAD_FLOOR (tc-sc-8-support.ts:284): a
// payload above it could not have crossed an MRP session.
const largePayloadFloor = 1280

// tcpFacts are TcpSessionFacts (:501-505): the TH's session over TCP, the
// daemon's id for it, and the connection the daemon served it on.
type tcpFacts struct {
	thSession     uint16
	daemonSession uint16
	channel       string
}

// tcpRun is a TCP case's TH and the session step 1 established.
type tcpRun struct {
	ctrl  *gocontroller.Controller
	n     *client.Node
	facts *tcpFacts
}

// reServedOnTCP is a daemon IM line served on a TCP connection, with its
// connection and session id.
var (
	reServedOnTCP = regexp.MustCompile(`src="(tcp://\S+) \(inbound\)" .*?session_id=(\d+)`)
	reOnTCP       = regexp.MustCompile(`src="(tcp://\S+) \(inbound\)"`)
)

// sessions is heldSessions (:542-544): the TH's live sessions with the
// DUT.
func (r *tcpRun) sessions() []*exchange.Session {
	addr, ok := r.n.PeerAddress()
	if !ok {
		return nil
	}
	var out []*exchange.Session
	for _, p := range r.ctrl.Peers().Peers() {
		if a := p.Address(); a.NodeID != addr.NodeID || a.FabricIndex != addr.FabricIndex {
			continue
		}
		for _, s := range p.Sessions() {
			if !s.IsClosed() && !s.IsPeerLost() {
				out = append(out, s)
			}
		}
	}
	return out
}

func describeSessions(ss []*exchange.Session) string {
	if len(ss) == 0 {
		return "no sessions"
	}
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%v session %d", s.Transport(), s.ID()))
	}
	return strings.Join(parts, ", ")
}

// tcpSessionOf is tcpSessionIdOf (:562-571): the case is about exactly one
// TCP session.
func tcpSessionOf(ss []*exchange.Session) (*exchange.Session, error) {
	var tcp []*exchange.Session
	for _, s := range ss {
		if s.Transport() == exchange.TransportTCP {
			tcp = append(tcp, s)
		}
	}
	if len(tcp) != 1 {
		return nil, fmt.Errorf("the TH holds %d TCP sessions with the DUT, and this case is about exactly one — it holds %s", len(tcp), describeSessions(ss))
	}
	return tcp[0], nil
}

// readVendorName is the read commissionOverTcp (:74-82) and
// recordReestablishedSession (:765-773) send: matter.js marks it
// largeMessage, making TCP a hard requirement; the Go TH prefers TCP for
// the peer (Config.TransportPreference), and the session the read used is
// held to TCP by the step after.
func (r *tcpRun) readVendorName(th *goTH) error {
	_, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorName))
	return err
}

// tcpDeclared holds the case's declared MCORE.SC.TCP to the daemon's own
// statement, MCORE.SC.S.TCP in its hand PICS.
func tcpDeclared(th *goTH) error {
	if th.pics.base["MCORE.SC.S.TCP"] != "1" {
		return errors.New("the daemon's hand PICS do not state MCORE.SC.S.TCP=1, which MCORE.SC.TCP is declared from")
	}
	return nil
}

// establish is tcpSessionStep (:528-539): the TH commissions the DUT and
// reads over the session, which runs over TCP (commissionOverTcp :63-84);
// the DUT's log shows the CASE session established and served on a TCP
// connection (recordTcpSession :94-150).
func (r *tcpRun) establish(th *goTH) error {
	if err := tcpDeclared(th); err != nil {
		return err
	}
	var err error
	r.ctrl, err = th.controllerWith("th", func(c *gocontroller.Config) {
		c.TCP = gocontroller.TCPConfig{Outgoing: true}
		c.TransportPreference = exchange.TransportTCP
	})
	if err != nil {
		return err
	}
	from := mark(th)
	if r.n, err = th.pair(r.ctrl, thQR(th)); err != nil {
		return fmt.Errorf("the TH commissions the DUT: %w", err)
	}
	if err := r.readVendorName(th); err != nil {
		return err
	}
	f, err := r.capture(th, from)
	if err != nil {
		return err
	}
	r.facts = f
	th.note("the TH holds TCP session %d with the DUT; the DUT established CASE session %d and served it on %s", f.thSession, f.daemonSession, f.channel)
	return nil
}

// capture is the TH's one TCP session and the daemon's lines for it since
// from: its CASE session established, then an interaction served on a
// tcp:// connection under that session id.
func (r *tcpRun) capture(th *goTH, from int) (*tcpFacts, error) {
	s, err := tcpSessionOf(r.sessions())
	if err != nil {
		return nil, err
	}
	id := strconv.Itoa(int(s.PeerSessionID()))
	var channel string
	if !waitFor(th.ctx, 10*time.Second, func() bool {
		log := thLogSince(th, from)
		i := strings.Index(log, "msg=case.session_established session_id="+id+" ")
		if i < 0 {
			return false
		}
		// The session's own message on a TCP connection: an interaction
		// under its id, or — for a session a read alone uses — the first
		// message the daemon took on a tcp:// connection after it
		// established the session, the handshake's closing StatusReport.
		for _, m := range reServedOnTCP.FindAllStringSubmatch(log, -1) {
			if m[2] == id {
				channel = m[1]
				return true
			}
		}
		if m := reOnTCP.FindStringSubmatch(log[i:]); m != nil {
			channel = m[1]
			return true
		}
		return false
	}) {
		return nil, fmt.Errorf("the DUT's log shows no CASE session %d established and served over a TCP connection", s.PeerSessionID())
	}
	return &tcpFacts{thSession: s.ID(), daemonSession: s.PeerSessionID(), channel: channel}, nil
}

// largePayload is largePayloadSessionCheck (:591-608): the session the TH
// holds runs over TCP and allows payloads larger than MRP carries.
func (r *tcpRun) largePayload(id uint16) (string, error) {
	ss := r.sessions()
	for _, s := range ss {
		if s.ID() != id {
			continue
		}
		detail := fmt.Sprintf("the TH holds %v session %d with a maximum payload of %d bytes", s.Transport(), s.ID(), s.MaxPayloadSize())
		if s.Transport() != exchange.TransportTCP || s.MaxPayloadSize() <= largePayloadFloor {
			return "", errors.New(detail)
		}
		return detail, nil
	}
	return "", fmt.Errorf("the TH holds no session %d with the DUT, and holds %s", id, describeSessions(ss))
}

// sever is recordSeveredSession (:705-742): the TH closes the TCP
// connection under the session without telling the DUT anything
// (severTransportConnection), no longer holds the session within
// EVICTION_TIMEOUT (:694), and the DUT evicts the session bound to the
// connection (sessionEvictionCheck :641-667: the bridge's matter.tcp.evict
// and matter.tcp.evict.session lines, matter.js's "TCP connection dropped,
// evicting bound sessions" and "Evicting session due to TCP disconnect").
func (r *tcpRun) sever(th *goTH) error {
	if r.facts == nil {
		return errors.New("no TCP session was captured")
	}
	var s *exchange.Session
	for _, c := range r.sessions() {
		if c.ID() == r.facts.thSession {
			s = c
		}
	}
	if s == nil || s.Channel() == nil {
		return fmt.Errorf("the TH holds no TCP session %d to sever", r.facts.thSession)
	}
	from := mark(th)
	if err := s.Channel().Close(); err != nil {
		return fmt.Errorf("close the TCP connection: %w", err)
	}
	gone := waitFor(th.ctx, 5*time.Second, func() bool {
		for _, c := range r.sessions() {
			if c.ID() == r.facts.thSession {
				return false
			}
		}
		return true
	})
	if !gone {
		return fmt.Errorf("the TH still holds session %d five seconds after the connection closed, and holds %s", r.facts.thSession, describeSessions(r.sessions()))
	}
	evict := fmt.Sprintf("msg=matter.tcp.evict.session subsystem=matter.bridge conn=\"%s (inbound)\" session_id=%d", r.facts.channel, r.facts.daemonSession)
	if !waitFor(th.ctx, 5*time.Second, func() bool {
		log := thLogSince(th, from)
		return strings.Contains(log, "msg=matter.tcp.evict ") && strings.Contains(log, evict)
	}) {
		return fmt.Errorf("the DUT's log shows no eviction of session %d on %s", r.facts.daemonSession, r.facts.channel)
	}
	th.note("the TH no longer holds session %d; the DUT dropped the connection %s and evicted session %d", r.facts.thSession, r.facts.channel, r.facts.daemonSession)
	return nil
}

// timeSnapshot is the InvokeCommandRequest of TC-SC-8.5 and 8.7: the TH
// receives the response (timeSnapshotResponseCheck :466-484), and the DUT
// served the invoke on the session step 1 established (tcpInvokeCheck
// :173-182, tcpInvokeEvidence :195-259: the session's tag, here its id, on a tcp:// connection).
func (r *tcpRun) timeSnapshot(th *goTH, from int) (string, error) {
	if r.facts == nil {
		return "", errors.New("no TCP session was captured")
	}
	res, err := th.invoke(r.n, cmdPath(0, generaldiagnostics.ClusterID, generaldiagnostics.CmdTimeSnapshot), emptyFields, false, client.InvokeOptions{})
	if err != nil {
		return "", err
	}
	var resp generaldiagnostics.TimeSnapshotResponse
	if !res.HasFields || resp.DecodeTLV(res.Fields) != nil {
		return "", fmt.Errorf("the TH received no TimeSnapshotResponse (command 0x%02X, fields %v)", res.Command, res.HasFields)
	}
	pattern := fmt.Sprintf(`src="%s (inbound)" responses=1 session_id=%d invoke_fabric_index=`, r.facts.channel, r.facts.daemonSession)
	path := fmt.Sprintf(`req_paths="ep=0 cl=0x%04X cmd=0x%X"`, generaldiagnostics.ClusterID, generaldiagnostics.CmdTimeSnapshot)
	if !waitFor(th.ctx, 10*time.Second, func() bool {
		for _, l := range strings.Split(thLogSince(th, from), "\n") {
			if strings.Contains(l, "msg=matter.rx.im.invoke ") && strings.Contains(l, pattern) && strings.Contains(l, path) {
				return true
			}
		}
		return false
	}) {
		return "", fmt.Errorf("the DUT's log shows no TimeSnapshot served on session %d over %s", r.facts.daemonSession, r.facts.channel)
	}
	return fmt.Sprintf("the TH received TimeSnapshotResponse SystemTimeMs=%d; the DUT served the invoke on session %d over %s", resp.SystemTimeMs, r.facts.daemonSession, r.facts.channel), nil
}

func (r *tcpRun) finalize(th *goTH) {
	if r.n != nil {
		_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
	}
}

// tcpCase is a TC-SC-8.x registration (TCP_ROLES, TCP_FLAVORS: the device
// is the DUT in matter.js too, so the case is native).
func tcpCase(tc string, line int, r *tcpRun, steps ...goStep) goCertCase {
	return goCertCase{tc: tc, line: line, role: roleTH, native: true, pics: tcpPICS, declare: tcpDeclare, finalize: r.finalize, steps: steps}
}

const tcpStep1 = "TH initiates a CASE session establishment with DUT, requesting a session supporting large payloads"

// caseSC81 is TC-SC-8.1: a session over TCP.
func caseSC81() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.1", 16, r, goStep{n: "1", line: 24, title: tcpStep1, run: r.establish})
}

// caseSC82 is TC-SC-8.2: the session allows large payloads.
func caseSC82() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.2", 39, r, goStep{n: "1", line: 47, title: tcpStep1, run: func(th *goTH) error {
		if err := r.establish(th); err != nil {
			return err
		}
		// establishLargePayloadSession (:28-37): recordLargePayloadSession.
		detail, err := r.largePayload(r.facts.thSession)
		if err != nil {
			return err
		}
		th.note("%s", detail)
		return nil
	}})
}

// caseSC83 is TC-SC-8.3: the TH closes the connection, the secure session
// is gone.
func caseSC83() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.3", 32, r,
		goStep{n: "1", line: 40, title: tcpStep1, run: r.establish},
		goStep{n: "2", line: 48, title: "TH closes the TCP connection with DUT", run: r.sever},
	)
}

// caseSC84 is TC-SC-8.4: closed, then re-established over TCP.
func caseSC84() goCertCase {
	r := &tcpRun{}
	var severed *tcpFacts
	var beforeSever int
	return tcpCase("TC-SC-8.4", 54, r,
		goStep{n: "1", line: 62, title: tcpStep1, run: r.establish},
		goStep{n: "2", line: 70, title: "TH closes the TCP connection with DUT", run: func(th *goTH) error {
			severed, beforeSever = r.facts, mark(th)
			return r.sever(th)
		}},
		// recordReestablishedSession (:760-791): the read re-establishes a
		// session over TCP, not the severed one, allowing large payloads, and
		// the DUT accepted it (furtherSessionCheck :800-828).
		goStep{n: "3", line: 73, title: "TH re-initiates CASE session establishment over TCP with DUT", run: func(th *goTH) error {
			if severed == nil {
				return errors.New("step 2 severed no session")
			}
			if err := r.readVendorName(th); err != nil {
				return err
			}
			f, err := r.capture(th, beforeSever)
			if err != nil {
				return err
			}
			if f.thSession == severed.thSession || f.daemonSession == severed.daemonSession {
				return fmt.Errorf("the session the TH holds is the one it severed (%d)", severed.thSession)
			}
			detail, err := r.largePayload(f.thSession)
			if err != nil {
				return err
			}
			r.facts = f
			th.note("a further session: %s; the DUT established CASE session %d and served it on %s", detail, f.daemonSession, f.channel)
			return nil
		}},
	)
}

// caseSC85 is TC-SC-8.5: an InvokeCommandRequest over the session.
func caseSC85() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.5", 65, r,
		goStep{n: "1", line: 73, title: tcpStep1, run: r.establish},
		goStep{n: "2", line: 81, title: "TH initiates an InvokeCommandRequest with DUT over the established session", run: func(th *goTH) error {
			detail, err := r.timeSnapshot(th, mark(th))
			if err != nil {
				return err
			}
			th.note("%s", detail)
			return nil
		}},
	)
}

// The bounds of readEverything (TC-SC-8.6.test.ts:30-31).
const (
	tcpMinAttributes = 100
	tcpMinClusters   = 10
)

// reReadServed is the daemon's summary of a read it answered, and the
// chunks of it (bridge's matter.rx.im.read and matter.rx.im.read.chunk).
var (
	reReadServed = regexp.MustCompile(`msg=matter\.rx\.im\.read subsystem=\S+ src="(tcp://\S+) \(inbound\)" attribute_reports=(\d+) chunks=(\d+)`)
	reReadChunk  = regexp.MustCompile(`msg=matter\.rx\.im\.read\.chunk subsystem=\S+ src="(tcp://\S+) \(inbound\)" chunk=(\d+) bytes=(\d+) more=(?:true|false)`)
)

// caseSC86 is TC-SC-8.6: a read of every attribute of every cluster.
func caseSC86() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.6", 64, r,
		goStep{n: "1", line: 72, title: tcpStep1, run: r.establish},
		// readEverything (:38-62). The plan has the DUT send all the attribute
		// data in one ReportData (wildcardReadInOneReportCheck,
		// tc-sc-8-support.ts:299-361); the reference daemon's attributes are
		// more than one TCP message carries — 64000 bytes, the maximum both
		// sides state (matter.js DEFAULT_MAX_TCP_MESSAGE_SIZE,
		// packages/general/src/net/tcp/TcpConnection.ts:11) —, so its report
		// comes in as few ReportData as that allows: one when it fits, and
		// otherwise every ReportData but the last fills the TCP message far
		// beyond what MRP could have carried (LARGE_PAYLOAD_FLOOR).
		goStep{n: "2", line: 80, title: "TH initiates a Read of all attributes of all clusters of DUT", run: func(th *goTH) error {
			if r.facts == nil {
				return errors.New("no TCP session was captured")
			}
			from := mark(th)
			rep, err := th.read(r.n, im.ConcreteAttributePath{})
			if err != nil {
				return err
			}
			endpoints, clusters := map[uint16]bool{}, map[[2]uint32]bool{}
			for i := range rep.Attributes {
				p := &rep.Attributes[i].Path
				endpoints[p.Endpoint] = true
				clusters[[2]uint32{uint32(p.Endpoint), p.Cluster}] = true
			}
			if len(rep.Attributes) <= tcpMinAttributes || len(clusters) <= tcpMinClusters {
				return fmt.Errorf("the TH received %d attributes of %d clusters on %d endpoints", len(rep.Attributes), len(clusters), len(endpoints))
			}
			var served []string
			if !waitFor(th.ctx, 10*time.Second, func() bool {
				served = nil
				for _, m := range reReadServed.FindAllStringSubmatch(thLogSince(th, from), -1) {
					if m[1] == r.facts.channel {
						served = m
					}
				}
				return served != nil
			}) {
				return fmt.Errorf("the DUT's log shows no read answered on %s", r.facts.channel)
			}
			// A chunk line names its index and no total (the response is
			// generated chunk by chunk): the read's chunks are the channel's
			// lines from its last chunk 0 on.
			var sizes []int
			for _, m := range reReadChunk.FindAllStringSubmatch(thLogSince(th, from), -1) {
				if m[1] != r.facts.channel {
					continue
				}
				if m[2] == "0" {
					sizes = nil
				}
				n, _ := strconv.Atoi(m[3])
				sizes = append(sizes, n)
			}
			chunks, _ := strconv.Atoi(served[3])
			if len(sizes) != chunks || chunks == 0 {
				return fmt.Errorf("the DUT answered in %d ReportData, its log lists %v", chunks, sizes)
			}
			total := 0
			for i, n := range sizes {
				total += n
				if i < len(sizes)-1 && n <= largePayloadFloor {
					return fmt.Errorf("ReportData %d of %d carried %d bytes, no more than MRP carries (%v)", i+1, chunks, n, sizes)
				}
			}
			if chunks > 1 && total <= 64000 {
				return fmt.Errorf("the DUT split %d bytes into %d ReportData, which one TCP message carries", total, chunks)
			}
			if chunks == 1 && sizes[0] <= largePayloadFloor {
				return fmt.Errorf("the one ReportData carried %d bytes, which MRP carries too", sizes[0])
			}
			th.note("the TH received %d attributes of %d clusters on %d endpoints; the DUT answered on %s in %d ReportData of %v bytes",
				len(rep.Attributes), len(clusters), len(endpoints), r.facts.channel, chunks, sizes)
			return nil
		}},
	)
}

// caseSC87 is TC-SC-8.7: a regularly sized invoke on the existing TCP
// session, no further session for it.
func caseSC87() goCertCase {
	r := &tcpRun{}
	return tcpCase("TC-SC-8.7", 70, r,
		goStep{n: "1", line: 78, title: tcpStep1, run: r.establish},
		// invokeOverExistingSession (:40-68): the response, the DUT's invoke
		// on step 1's session, a request MRP could have carried
		// (regularSizedRequestCheck :387-416 — TimeSnapshot carries no
		// fields, its InvokeRequest is a few dozen bytes), and no further
		// session (noFurtherSessionCheck :430-449).
		goStep{n: "2", line: 86, title: "TH initiates a regularly-sized InvokeCommandRequest with DUT over the existing TCP-based session", run: func(th *goTH) error {
			from := mark(th)
			detail, err := r.timeSnapshot(th, from)
			if err != nil {
				return err
			}
			if strings.Contains(thLogSince(th, from), "msg=case.session_established ") {
				return errors.New("the DUT established a further session for the invoke")
			}
			th.note("%s; the request carries no fields, under the MRP limit of %d bytes; no further session was established", detail, exchange.MaxPayloadSize)
			return nil
		}},
	)
}
