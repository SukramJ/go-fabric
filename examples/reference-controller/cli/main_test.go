// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	harnessPasscode      uint32 = 20202021
	harnessDiscriminator uint16 = 3840
	harnessNodeID        uint64 = 0x4321
)

// plug is a bridged OnOffPlugInUnit, as node/groupsend_live_test.go's.
type plug struct {
	key string
	srv *onoff.Server
}

func (p *plug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(p.key), DeviceAddress: p.key, ChannelAddress: p.key + ":0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: p.key, Source: p,
	}
}
func (p *plug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }
func (p *plug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

func onOff(srv *onoff.Server) bool {
	v, _ := srv.MatterRead(onoff.AttrOnOff)
	b, _ := v.(bool)
	return b
}

// harness is the node the tool pairs: a plug and a light switch on the
// Aggregator, on loopback, its DNS-SD answered by the responder.
type harness struct {
	n      *node.Node
	plug   *onoff.Server
	sw     *device.OnOffLightSwitch
	addr   *net.UDPAddr
	plugEP uint16
	swEP   uint16
}

func startHarness(t *testing.T, edit func(*node.Config)) *harness {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "harness.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: harnessPasscode, Discriminator: harnessDiscriminator, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "reference controller test", NodeLabel: "harness", SerialNumber: "RC-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: mdns.NewNoop(),
		Logger:     slog.New(slog.DiscardHandler),
	}
	if edit != nil {
		edit(&cfg)
	}
	n, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(context.Background(), &plug{key: "plug", srv: srv}); err != nil {
		t.Fatal(err)
	}
	sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch"), Name: "switch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(context.Background(), sw); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	h := &harness{n: n, plug: srv, sw: sw}
	if h.addr, err = net.ResolveUDPAddr("udp", n.Bridge().LocalAddr()); err != nil {
		t.Fatal(err)
	}
	for _, ep := range n.Bridge().Topology().Bridged() {
		switch {
		case ep.SourceKey == nil:
		case ep.SourceKey.String() == "plug":
			h.plugEP = ep.ID
		case ep.SourceKey.String() == "switch":
			h.swEP = ep.ID
		}
	}
	if h.plugEP == 0 || h.swEP == 0 {
		t.Fatal("the harness has no plug or switch endpoint")
	}
	return h
}

// responder answers the tool's DNS-SD queries for the harness: its
// commissionable record, and the operational record of every node id it
// knows.
type responder struct {
	port        int
	mu          sync.Mutex
	operational map[uint64]int
	// window, when set, is the commissionable record's D= value in place
	// of the harness's own: an opened window advertises its discriminator.
	window *uint16
}

func (r *responder) commissionableDiscriminator() uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.window != nil {
		return *r.window
	}
	return harnessDiscriminator
}

func (r *responder) commissionablePort() uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint16(r.port) //nolint:gosec // a test port
}

type fakeConn struct {
	r   *responder
	out chan mdns.QueryResponse
}

func (c *fakeConn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c *fakeConn) Close() error                         { return nil }

func hdr(name string, typ uint16) dns.RR_Header {
	return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
}

func (c *fakeConn) Send(b []byte) error {
	var q dns.Msg
	if err := q.Unpack(b); err != nil {
		return nil //nolint:nilerr // not a query this responder answers
	}
	const inst = "C0FFEE0123456789._matterc._udp.local."
	const host = "harness.local."
	a := &dns.A{Hdr: hdr(host, dns.TypeA), A: net.ParseIP("127.0.0.1").To4()}
	var answers []dns.RR
	for _, qq := range q.Question {
		name := strings.ToLower(qq.Name)
		switch {
		case strings.HasSuffix(name, "_matterc._udp.local."):
			answers = append(answers,
				&dns.PTR{Hdr: dns.RR_Header{Name: "_matterc._udp.local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 4500}, Ptr: inst},
				&dns.SRV{Hdr: hdr(inst, dns.TypeSRV), Target: host, Port: c.r.commissionablePort()}, //nolint:gosec // a test port
				&dns.TXT{Hdr: hdr(inst, dns.TypeTXT), Txt: []string{"D=" + strconv.Itoa(int(c.r.commissionableDiscriminator())), "CM=1", "VP=65521+32769", "SII=500", "SAI=300"}},
				a)
			if name != "_matterc._udp.local." {
				answers = append(answers, &dns.PTR{Hdr: dns.RR_Header{Name: qq.Name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 4500}, Ptr: inst})
			}
		case strings.HasSuffix(name, "._matter._tcp.local."):
			label := strings.SplitN(strings.TrimSuffix(name, "._matter._tcp.local."), "-", 2)
			if len(label) != 2 {
				continue
			}
			id, err := strconv.ParseUint(label[1], 16, 64)
			if err != nil {
				continue
			}
			c.r.mu.Lock()
			port, ok := c.r.operational[id]
			c.r.mu.Unlock()
			if ok {
				answers = append(answers,
					&dns.SRV{Hdr: hdr(qq.Name, dns.TypeSRV), Target: host, Port: uint16(port)}, //nolint:gosec // a test port
					&dns.TXT{Hdr: hdr(qq.Name, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}}, a)
			}
		case name == host:
			answers = append(answers, a)
		}
	}
	if len(answers) == 0 {
		return nil
	}
	m := new(dns.Msg)
	m.Response = true
	m.Answer = answers
	out, err := m.Pack()
	if err != nil {
		return err
	}
	select {
	case c.out <- mdns.QueryResponse{Msg: out}:
	default:
	}
	return nil
}

// unicastRedirect delivers the own node's group datagrams to the harness
// on loopback — node/groupsend_live_test.go's unicastRedirect.
type unicastRedirect struct {
	l  *udp.Listener
	tl *tool
}

func (r *unicastRedirect) SendMulticast(_ net.IP, _ int, payload []byte) error {
	r.tl.mu.Lock()
	dst := r.tl.groupDst
	r.tl.mu.Unlock()
	return r.l.Send(dst, payload)
}

// tool runs the CLI against one database with the test's seams.
type tool struct {
	t  *testing.T
	db string
	r  *responder

	mu       sync.Mutex
	groupDst *net.UDPAddr // where the own node's group datagrams go
}

// target points the responder's commissionable record and the group
// datagrams at another harness, known under nodeID once paired.
func (tl *tool) target(h *harness, nodeID uint64) {
	tl.r.mu.Lock()
	tl.r.port = h.addr.Port
	tl.r.operational[nodeID] = h.addr.Port
	tl.r.mu.Unlock()
	tl.mu.Lock()
	tl.groupDst = h.addr
	tl.mu.Unlock()
}

func newTool(t *testing.T, h *harness) *tool {
	t.Helper()
	r := &responder{port: h.addr.Port, operational: map[uint64]int{harnessNodeID: h.addr.Port}}
	hooks.names = func(log *slog.Logger) (*discovery.Names, error) {
		return discovery.NewNames(&fakeConn{r: r, out: make(chan mdns.QueryResponse, 256)}, log), nil
	}
	tx, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	selfHooks.advertiser = func() mdns.Advertiser { return mdns.NewNoop() }
	tl := &tool{t: t, db: filepath.Join(t.TempDir(), "controller.db"), r: r, groupDst: h.addr}
	selfHooks.transport = func() groups.MulticastTransport { return &unicastRedirect{l: tx, tl: tl} }
	selfHooks.started = func(id uint64, a *net.UDPAddr) {
		r.mu.Lock()
		r.operational[id] = a.Port
		r.mu.Unlock()
	}
	stderr = &bytes.Buffer{}
	t.Cleanup(func() {
		_ = tx.Close()
		hooks.names, selfHooks.advertiser, selfHooks.transport, selfHooks.started = nil, nil, nil, nil
	})
	return tl
}

func (tl *tool) args(args ...string) []string {
	return append([]string{"--db", tl.db, "--listen", "127.0.0.1:0", "--node-listen", "127.0.0.1:0", "--attestation", "accept", "--log-level", "error", "--timeout", "60s"}, args...)
}

// run runs one command and returns its output; stdin is the console.
func (tl *tool) runIn(stdin string, args ...string) (string, error) {
	var out bytes.Buffer
	err := run(tl.args(args...), strings.NewReader(stdin), &out)
	return out.String(), err
}

func (tl *tool) run(args ...string) string {
	tl.t.Helper()
	out, err := tl.runIn("", args...)
	if err != nil {
		tl.t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestReferenceController drives the tool through a node's life: pair by
// manual code, nodes, read, write, invoke, subscribe, events, node, group
// create and send, bind, unpair — each command a separate run on the same
// database, so every one reopens the persisted fabric. Not parallel.
func TestReferenceController(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	nid := "0x4321"
	start := time.Now()
	if out := tl.run("discover", "--code", manual, "--first"); !strings.Contains(out, "discriminator "+strconv.Itoa(int(harnessDiscriminator))) || strings.Count(out, "\n") != 1 {
		t.Fatalf("discover --first:\n%s", out)
	}
	if took := time.Since(start); took >= 60*time.Second {
		t.Fatalf("discover --first took %v, the whole --timeout", took)
	}
	out := tl.run("pair", "--code", manual, "--node-id", nid, "--label", "kitchen")
	if !strings.Contains(out, "paired node 17185") || !strings.Contains(out, "endpoint 0:") {
		t.Fatalf("pair:\n%s", out)
	}
	fabrics, err := h.n.FabricInfos(context.Background())
	if err != nil || len(fabrics) != 1 || fabrics[0].NodeID != harnessNodeID {
		t.Fatalf("harness fabrics %+v %v", fabrics, err)
	}

	if out := tl.run("nodes"); !strings.Contains(out, "17185") || !strings.Contains(out, " reachable") || !strings.Contains(out, `label="kitchen"`) {
		t.Fatalf("nodes:\n%s", out)
	}

	out = tl.run("read", nid, "0/BasicInformation/NodeLabel", "0/BasicInformation/VendorID")
	var line attributeLine
	if err := json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &line); err != nil || string(line.Value) != `"kitchen"` {
		t.Fatalf("read: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"value":65521`) {
		t.Fatalf("read VendorID:\n%s", out)
	}

	if out := tl.run("write", nid, "0/BasicInformation/NodeLabel", `"pantry"`); strings.TrimSpace(out) != "ok" {
		t.Fatalf("write:\n%s", out)
	}
	if out := tl.run("read", nid, "0/0x0028/5"); !strings.Contains(out, `"pantry"`) {
		t.Fatalf("read after write:\n%s", out)
	}

	plugPath := fmt.Sprintf("%d/OnOff/", h.plugEP)
	if out := tl.run("invoke", nid, plugPath+"On"); !strings.Contains(out, `"status":"Success"`) || !onOff(h.plug) {
		t.Fatalf("invoke On:\n%s (plug on %v)", out, onOff(h.plug))
	}
	if out := tl.run("invoke", nid, plugPath+"Off", "{}", "--timed", "2000"); !strings.Contains(out, "Success") || onOff(h.plug) {
		t.Fatalf("timed invoke Off:\n%s", out)
	}

	out = tl.run("subscribe", nid, plugPath+"OnOff", "--count", "1", "--max", "10")
	if !strings.Contains(out, "subscription ") || !strings.Contains(out, `"attribute":"OnOff"`) {
		t.Fatalf("subscribe:\n%s", out)
	}

	if out := tl.run("events", nid, "0/BasicInformation/StartUp"); !strings.Contains(out, `"event":"StartUp"`) {
		t.Fatalf("events:\n%s", out)
	}

	if out := tl.run("node", nid); !strings.Contains(out, "endpoint 0:") || !strings.Contains(out, "NodeLabel = \"pantry\"") {
		t.Fatalf("node:\n%s", out)
	}

	// Group: the harness's plug joins group 257; the own node sends Toggle.
	out = tl.run("group", "create", "257", "--key", "d0d1d2d3d4d5d6d7d8d9dadbdcdddedf", "--nodes", nid, "--endpoints", strconv.Itoa(int(h.plugEP)))
	if !strings.Contains(out, "group 257 created") {
		t.Fatalf("group create:\n%s", out)
	}
	before := onOff(h.plug)
	if out := tl.run("group", "send", "257", "OnOff/Toggle"); !strings.Contains(out, "sent to group 257") {
		t.Fatalf("group send:\n%s", out)
	}
	waitFor(t, "the group Toggle", func() bool { return onOff(h.plug) != before })

	// Bind the switch to the plug on the same node, then to the group.
	if out := tl.run("bind", nid, strconv.Itoa(int(h.swEP)), "--to", fmt.Sprintf("%s/%d/OnOff", nid, h.plugEP)); !strings.Contains(out, "bound node 17185") {
		t.Fatalf("bind:\n%s", out)
	}
	if out := tl.run("bind", nid, strconv.Itoa(int(h.swEP)), "--group", "257"); !strings.Contains(out, "to group 257") {
		t.Fatalf("bind --group:\n%s", out)
	}
	entries := h.sw.Binding().Underlying().(*core.Binding).Entries()
	if len(entries) != 2 || entries[0].Node != harnessNodeID || entries[0].Endpoint != h.plugEP || entries[1].Group != 257 {
		t.Fatalf("switch Binding %+v", entries)
	}

	if out := tl.run("unpair", nid); !strings.Contains(out, "unpaired node 17185") {
		t.Fatalf("unpair:\n%s", out)
	}
	if fabrics, _ := h.n.FabricInfos(context.Background()); len(fabrics) != 0 {
		t.Fatalf("the harness still has fabrics %+v", fabrics)
	}
	if out := tl.run("nodes"); !strings.Contains(out, "no paired nodes") {
		t.Fatalf("nodes after unpair:\n%s", out)
	}

	// §5: a node whose root does not list Groupcast gets the pre-1.5 group
	// provisioning (GroupKeyMap, Groups AddGroup, a Group ACL entry), and
	// a group Toggle from the own node reaches its plug.
	legacy := startHarness(t, nil)
	hideGroupcast(t, legacy.n)
	tl.target(legacy, 0x4322)
	tl.run("pair", "--code", manual, "--node-id", "0x4322")
	out = tl.run("group", "create", "258", "--key", "random", "--nodes", "0x4322", "--endpoints", strconv.Itoa(int(legacy.plugEP)))
	if !strings.Contains(out, "(Groups)") || !strings.Contains(out, "group 258 created") {
		t.Fatalf("legacy group create:\n%s", out)
	}
	before = onOff(legacy.plug)
	tl.run("group", "send", "258", "OnOff/Toggle")
	waitFor(t, "the legacy group Toggle", func() bool { return onOff(legacy.plug) != before })
}

// hideGroupcast drops Groupcast from the root's Descriptor ServerList. The
// node facade always mounts Groupcast on the root (node/root.go:209-268)
// and node.Config has no option that leaves it out, so the existing seam
// is the exported root Descriptor's server-list provider
// (node.Node.Root().Descriptor, cluster/core Descriptor.SetServerListProvider):
// the node then presents itself as one without Groupcast, which is what
// the tool decides by. The server itself stays mounted.
func hideGroupcast(t *testing.T, n *node.Node) {
	t.Helper()
	v, ok := n.Root().Descriptor.MatterRead(0x0001)
	ids, _ := v.([]uint32)
	if !ok || len(ids) == 0 {
		t.Fatalf("root ServerList %v", v)
	}
	var kept []uint32
	for _, id := range ids {
		if id != clusterGroupcast {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(ids) {
		t.Fatal("the root listed no Groupcast")
	}
	n.Root().Descriptor.SetServerListProvider(func() []uint32 { return kept })
}

// staticOperational resolves every operational address to one socket —
// node/ota_live_test.go's.
type staticOperational struct{ addr *net.UDPAddr }

func (s staticOperational) ResolveOperational(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
	return []*net.UDPAddr{s.addr}, nil
}

// TestReferenceControllerOTA: "ota serve --announce" serves an image from
// the tool's own node to the harness's OTA requestor, as
// TestLiveOTAProvider (node/ota_live_test.go) does device to device; the
// console grants the consent. Not parallel.
func TestReferenceControllerOTA(t *testing.T) {
	h := startHarness(t, func(c *node.Config) {
		c.OTA = &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota"), Harness: true}
	})
	tl := newTool(t, h)
	started := selfHooks.started
	selfHooks.started = func(id uint64, a *net.UDPAddr) {
		started(id, a)
		// The requestor finds the provider through its operational
		// resolver; the loopback test has no DNS-SD.
		h.n.Bridge().AttachOperationalResolver(staticOperational{addr: a})
	}
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	tl.run("pair", "--code", manual, "--node-id", "0x4321")

	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0", Payload: bytes.Repeat([]byte{0x5A, 0xA5}, 2500),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.ota")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	baseContext = func() context.Context { return ctx }
	defer func() { baseContext = context.Background }()
	out, err := tl.runIn("y\n", "ota", "serve", "--image", path, "--announce", "0x4321", "--once")
	if err != nil {
		t.Fatalf("ota serve: %v\n%s", err, out)
	}
	for _, want := range []string{"serving version 2 for FFF1:8001", "announced to node 17185", "node 17185: Querying", "node 17185: Downloading", "node 17185: Done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ota serve lacks %q:\n%s", want, out)
		}
	}
	if v, _ := h.n.Root().BasicInformation.MatterRead(0x0009); v != uint32(2) {
		t.Errorf("the harness's SoftwareVersion %v, want 2", v)
	}
}
