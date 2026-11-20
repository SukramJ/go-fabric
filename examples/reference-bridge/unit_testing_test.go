// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
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

// TestUnitTestingListAppend is the shape of a chunked write of
// listLongOctetString, the evidence TC-IDM-3.1 step 13 asks for: a REPLACE
// with an empty list, then each entry appended (ListIndex null) — the
// chunks client/interaction's chunkWrite sends for a list too large for one
// message. The daemon answers the ADD CONSTRAINT_ERROR: bridge's
// listItemValue decodes an appended scalar entry as []any, and the
// dispatcher's appendListValue refuses an item whose slice type differs
// from the stored list's ([][]byte). The gap is the module's, not this
// cluster's (P2b-F report, finding); the test is skipped until it closes.
func TestUnitTestingListAppend(t *testing.T) {
	t.Parallel()
	t.Skip("module gap: a list-append of a scalar entry to a [][]byte list answers CONSTRAINT_ERROR (bridge listItemValue, endpoint appendListValue); P2b-F report")
	s := startUnitTestingNode(t)
	listPath := unitTestingPath(unitTestingAttrListLongOctetString)
	write(t, s, listPath, func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartArray(tag)
		_ = e.EndContainer()
	})
	appendPath := listPath
	appendPath.ListAppend = true
	for range 3 {
		write(t, s, appendPath, func(e *tlv.Encoder, tag tlv.Tag) { e.PutOctets(tag, unitTestingLongOctetStringEntry) })
	}
	if v := readBack(t, s, unitTestingAttrListLongOctetString); len(v.Children) != 3 {
		t.Errorf("listLongOctetString after the chunked write: %d entries, want 3", len(v.Children))
	}
}
