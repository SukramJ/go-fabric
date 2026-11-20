// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/tlv"
)

// unitTestingPath is the UnitTesting attribute attr on the Aggregator,
// endpoint 1 — matter.js's UNIT_TESTING_ENDPOINT
// (unit-testing-cluster.ts:64 at b25ed960).
func unitTestingPath(attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{
		Endpoint: 1, Cluster: unitTestingClusterID, Attribute: attr,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
}

// startUnitTestingNode starts the fleet's node with --unit-testing's
// server on the Aggregator and returns a CASE session of a commissioner
// that commissioned it.
func startUnitTestingNode(t *testing.T) *bridgetest.Session {
	t.Helper()
	ut, err := newUnitTesting()
	if err != nil {
		t.Fatal(err)
	}
	n := testNodeWith(t, newFleet(), filepath.Join(t.TempDir(), "ut.db"), func(cfg *node.Config) {
		cfg.Network.PreferIPv4 = true
		cfg.AggregatorServers = append(cfg.AggregatorServers, contract.ClusterServer(ut))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("node.Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), bridgetest.Options{
		Passcode: 20202021, Discriminator: 3840, FabricID: 0xFAB, NodeID: 0xD0D, Seed: 1,
	})
	s, err := c.Commission(ctx)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	return s
}

// readBack reads one UnitTesting attribute and returns its value.
func readBack(t *testing.T, s *bridgetest.Session, attr uint32) bridgetest.TLV {
	t.Helper()
	r, err := s.Read(context.Background(), unitTestingPath(attr))
	if err != nil {
		t.Fatalf("read 0x%02X: %v", attr, err)
	}
	v, ok := r.Value(1, unitTestingClusterID, attr)
	if !ok {
		st, _ := r.Status(1, unitTestingClusterID, attr)
		t.Fatalf("read 0x%02X: no value (status %v)", attr, st)
	}
	return v
}

// write writes one UnitTesting attribute and fails unless the device
// answers SUCCESS.
func write(t *testing.T, s *bridgetest.Session, path im.ConcreteAttributePath, value func(*tlv.Encoder, tlv.Tag)) {
	t.Helper()
	st, err := s.Write(context.Background(), path, value)
	if err != nil {
		t.Fatalf("write 0x%02X: %v", path.Attribute, err)
	}
	if st != im.StatusSuccess {
		t.Fatalf("write 0x%02X: status %v, want SUCCESS", path.Attribute, st)
	}
}

// TestUnitTestingServesWhatTheGoTHWrites settles, over the wire against
// the daemon, what the data-type steps of TC-IDM-3.1 (7, 8, 9, 13) and
// TC-IDM-4.1 (7) need of --unit-testing's cluster, a definition stated by
// hand through cluster/spec: the initial state of matter.js's
// UnitTestingServer, a written float, octet string and struct read back as
// written — the struct through the definition's own Decode, as no
// generated decoder knows the cluster — and the long octet-string list
// replaced whole.
//
// Mirrors matter.js support/chip-testing/src/cert/unit-testing-cluster.ts:91-106
// and test/cert/TC-IDM-3.1.test.ts:80-85 (the written values) at b25ed960.
func TestUnitTestingServesWhatTheGoTHWrites(t *testing.T) {
	t.Parallel()
	s := startUnitTestingNode(t)

	if v := readBack(t, s, unitTestingAttrListLongOctetString); len(v.Children) != 4 || !bytes.Equal(v.Children[0].Octets, unitTestingLongOctetStringEntry) {
		t.Errorf("initial listLongOctetString: %d entries, want 4 of the 512-byte entry", len(v.Children))
	}

	write(t, s, unitTestingPath(unitTestingAttrFloatSingle), func(e *tlv.Encoder, tag tlv.Tag) { e.PutFloat32(tag, 1.5) })
	if v := readBack(t, s, unitTestingAttrFloatSingle); v.Type != tlv.TypeFloat4 || v.Float != 1.5 {
		t.Errorf("floatSingle read back %v (type 0x%02X), want single 1.5", v.Float, v.Type)
	}

	octets := []byte{0x01, 0x02, 0xa5, 0x5a, 0xff}
	write(t, s, unitTestingPath(unitTestingAttrOctetString), func(e *tlv.Encoder, tag tlv.Tag) { e.PutOctets(tag, octets) })
	if v := readBack(t, s, unitTestingAttrOctetString); !bytes.Equal(v.Octets, octets) {
		t.Errorf("octetString read back %x, want %x", v.Octets, octets)
	}

	write(t, s, unitTestingPath(unitTestingAttrStructAttr), func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutUint(tlv.ContextTag(0), 7)
		e.PutBool(tlv.ContextTag(1), true)
		e.PutUint(tlv.ContextTag(2), 2)
		e.PutOctets(tlv.ContextTag(3), []byte{0xde, 0xad})
		e.PutUTF8(tlv.ContextTag(4), "")
		e.PutUint(tlv.ContextTag(5), 5)
		e.PutFloat32(tlv.ContextTag(6), 1.5)
		e.PutFloat64(tlv.ContextTag(7), -0.25)
		_ = e.EndContainer()
	})
	v := readBack(t, s, unitTestingAttrStructAttr)
	field := func(tag uint8) bridgetest.TLV {
		f, ok := v.Child(tag)
		if !ok {
			t.Errorf("structAttr read back without field %d", tag)
		}
		return f
	}
	if a, b, c, d, e, f, g, h := field(0), field(1), field(2), field(3), field(4), field(5), field(6), field(7); a.Uint != 7 || !b.Bool || c.Uint != 2 ||
		!bytes.Equal(d.Octets, []byte{0xde, 0xad}) || e.String != "" || f.Uint != 5 || g.Float != 1.5 || h.Float != -0.25 {
		t.Errorf("structAttr read back a=%d b=%v c=%d d=%x e=%q f=%d g=%v h=%v, want 7 true 2 dead \"\" 5 1.5 -0.25",
			a.Uint, b.Bool, c.Uint, d.Octets, e.String, f.Uint, g.Float, h.Float)
	}

	// The list replaced whole with one entry: three, as TC-IDM-3.1 step 13
	// writes them (:85), fit no message and go as a chunked write
	// (TestUnitTestingListAppend).
	write(t, s, unitTestingPath(unitTestingAttrListLongOctetString), func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartArray(tag)
		e.PutOctets(tlv.AnonymousTag(), unitTestingLongOctetStringEntry)
		_ = e.EndContainer()
	})
	if v := readBack(t, s, unitTestingAttrListLongOctetString); len(v.Children) != 1 {
		t.Errorf("listLongOctetString after the write: %d entries, want 1", len(v.Children))
	}
}

// TestUnitTestingServesEveryAttributeFromTheStart: a wildcard read of the
// cluster on endpoint 1 returns all four attributes before any write, each
// at matter.js's initial value (unit-testing-cluster.ts:91-106 at
// b25ed960), and a subscription to floatSingle is established — chip's
// TC-IDM-10.1 reads every attribute AttributeList names, and TC-IDM-4.1
// step 7 subscribes to floatSingle (PR #562's chiptool.yml: both absent,
// the subscribe refused InvalidAction).
func TestUnitTestingServesEveryAttributeFromTheStart(t *testing.T) {
	t.Parallel()
	s := startUnitTestingNode(t)
	ctx := context.Background()
	wild := im.ConcreteAttributePath{Endpoint: 1, Cluster: unitTestingClusterID, HasEndpoint: true, HasCluster: true}
	r, err := s.Read(ctx, wild)
	if err != nil {
		t.Fatal(err)
	}
	for _, attr := range []uint32{unitTestingAttrFloatSingle, unitTestingAttrOctetString, unitTestingAttrStructAttr, unitTestingAttrListLongOctetString} {
		if _, ok := r.Value(1, unitTestingClusterID, attr); !ok {
			t.Errorf("the wildcard read of 1/UnitTesting returns no attribute 0x%02X", attr)
		}
	}
	if v, ok := r.Value(1, unitTestingClusterID, unitTestingAttrFloatSingle); ok && (v.Type != tlv.TypeFloat4 || v.Float != 0) {
		t.Errorf("floatSingle starts at %v (type 0x%02X), want single 0", v.Float, v.Type)
	}
	if v, ok := r.Value(1, unitTestingClusterID, unitTestingAttrOctetString); ok && (v.Type < tlv.TypeOctetStr1 || v.Type > tlv.TypeOctetStr8 || len(v.Octets) != 0) {
		t.Errorf("octetString starts at %x (type 0x%02X), want the empty octet string", v.Octets, v.Type)
	}
	sub, err := s.Subscribe(ctx, 0, 30, unitTestingPath(unitTestingAttrFloatSingle))
	if err != nil {
		t.Fatalf("subscribe to floatSingle: %v", err)
	}
	if _, ok := sub.Priming.Value(1, unitTestingClusterID, unitTestingAttrFloatSingle); !ok {
		t.Error("the priming report holds no floatSingle")
	}
}

// TestUnitTestingListAppend is TC-IDM-3.1 step 13's write, as the Go-TH
// leg sends it: the module's controller writes listLongOctetString with
// three 512-byte entries, which fit no message, so the client sends it
// chunked — a REPLACE with the empty list, then one ADD (ListIndex null)
// per entry over several WriteRequest messages — and the list reads back
// with three entries; the controller's own Debug line counts the
// messages, the step's evidence (owner decision C-21). Each ADD is decoded
// with the definition's entry type, as matter.js decodes it with the
// list's elementSchema
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:446-460 at
// b25ed960); before, the daemon answered every ADD CONSTRAINT_ERROR.
func TestUnitTestingListAppend(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	logger := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	applier := &otaApplier{vendorID: testVendorID, productID: testProductID, dir: filepath.Join(dir, "ota"), logger: logger}
	actions, err := mattercore.NewActions(mattercore.ActionsConfig{Host: newFleet().actions})
	if err != nil {
		t.Fatal(err)
	}
	ut, err := newUnitTesting()
	if err != nil {
		t.Fatal(err)
	}
	const passcode, discriminator = 20202021, 3840
	cfg := nodeConfig(flags{dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "reference-daemon", salt: "go-fabric-ref-salt", iterations: 1000},
		passcode, discriminator, node.TCPConfig{}, applier, actions, logger)
	cfg.AggregatorServers = append(cfg.AggregatorServers, ut)
	site.AddNode(cfg)

	var logs bytes.Buffer
	var mu sync.Mutex
	ctlLog := slog.New(slog.NewTextHandler(lockedWriter{&mu, &logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ccfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: ctlLog})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, ccfg); return err }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.Background(), c.Close) })
	code, err := setup.ManualCode(discriminator, passcode)
	if err != nil {
		t.Fatal(err)
	}
	p := unitTestingPath(unitTestingAttrListLongOctetString)
	entries := [][]byte{unitTestingLongOctetStringEntry, unitTestingLongOctetStringEntry, unitTestingLongOctetStringEntry}
	value := listOfOctets(entries)
	var st client.WriteStatuses
	var read *client.Report
	if err := site.Run(ctx, func(ctx context.Context) error {
		n, err := c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		if err != nil {
			return err
		}
		mu.Lock()
		logs.Reset()
		mu.Unlock()
		if st, err = n.Write(ctx, []client.WriteRequest{{Path: p, Value: value}}, client.WriteOptions{}); err != nil {
			return err
		}
		read, err = n.Read(ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Attributes: []im.ConcreteAttributePath{p}}, IncludeKnownVersions: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Err(); err != nil {
		t.Fatalf("the chunked write: %v (statuses %+v)", err, st)
	}
	v, ok := read.Value(p.Endpoint, p.Cluster, p.Attribute)
	if !ok || len(v.Children) != len(entries) {
		t.Fatalf("listLongOctetString after the chunked write: %d entries (ok %v), want %d", len(v.Children), ok, len(entries))
	}
	for i, e := range v.Children {
		if !bytes.Equal(e.El.Octets, unitTestingLongOctetStringEntry) {
			t.Errorf("entry %d reads back %d bytes, not the long entry", i, len(e.El.Octets))
		}
	}
	mu.Lock()
	m := regexp.MustCompile(`msg=matter\.controller\.interaction\.write .*?chunks=(\d+)`).FindStringSubmatch(logs.String())
	mu.Unlock()
	if m == nil {
		t.Fatal("the controller logged no matter.controller.interaction.write line for the write")
	}
	if chunks, _ := strconv.Atoi(m[1]); chunks < 2 {
		t.Errorf("the write went as %d WriteRequest message, want at least 2 (chunked)", chunks)
	}
}

// lockedWriter serialises the controller's log writes into a buffer.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

// listOfOctets encodes a list of octet strings.
func listOfOctets(entries [][]byte) spec.Encodable {
	return encodableFunc(func(enc *tlv.Encoder, tag tlv.Tag) { spec.PutList(spec.PutBytes)(enc, tag, entries) })
}

type encodableFunc func(enc *tlv.Encoder, tag tlv.Tag)

func (f encodableFunc) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { f(enc, tag) }
