// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// namingServer is a manufacturer cluster whose write answers the change
// list it is given (contract.ChangeNamingWriter).
type namingServer struct {
	gateServer
	names []uint32
	err   error
}

func (s *namingServer) MatterWriteCommit(_ context.Context, a uint32, v any) ([]uint32, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.writes++
	s.vals[a] = v
	return s.names, nil
}

// TestWriteResultCarriesTheCommit: TopologyDispatcher gives every
// successful write its verdict in the result (im.WriteResult.Commit): the
// attributes a ChangeNamingWriter names, sorted and each once, an empty
// list for one that names none, and none for a refused write. The cluster's
// DataVersion advances once for a commit that names an attribute and not
// for an empty one — matter.js advances a datasource's version once per
// commit, only when a field was noted
// (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
// db021da0) (review II, R2-P5-write-01, -02).
func TestWriteResultCarriesTheCommit(t *testing.T) {
	t.Parallel()
	srv := &namingServer{gateServer: gateServer{id: 0xFFF1FC02, attrs: []uint32{0x0000, 0x0001, 0x0002}, vals: map[uint32]any{0x0000: uint16(0), 0x0001: uint16(0), 0x0002: uint16(0)}}}
	d := gateDispatcher(t, store.PrivilegeAdminister, srv)
	path := concreteAttrPath(2, srv.id, 0x0000)
	steps := []struct {
		name     string
		names    []uint32
		err      error
		want     []uint32
		advanced bool
	}{
		{"a sibling and the written one, unsorted and twice", []uint32{0x0002, 0x0000, 0x0002}, nil, []uint32{0x0000, 0x0002}, true},
		{"the sibling alone", []uint32{0x0001}, nil, []uint32{0x0001}, true},
		{"nothing", nil, nil, []uint32{}, false},
		{"refused", nil, errors.New("no"), nil, false},
	}
	for _, s := range steps {
		srv.names, srv.err = s.names, s.err
		before, _ := d.CurrentDataVersion(context.Background(), 2, srv.id)
		out := im.HandleWrite(caseCtx(false), d, im.WriteRequest{Writes: []im.AttributeWrite{writeOf(path, uint16(1))}})
		if len(out.Results) != 1 || len(out.Response.Responses) != 1 {
			t.Fatalf("%s: %+v", s.name, out)
		}
		r := out.Results[0]
		switch {
		case s.err != nil:
			if r.Status == im.StatusSuccess || r.Commit != nil {
				t.Errorf("%s: %+v, want a refusal without a Commit", s.name, r)
			}
		case r.Commit == nil:
			t.Errorf("%s: no Commit", s.name)
		case !slices.Equal(r.Commit.Changed, s.want) || r.Commit.Changed == nil:
			t.Errorf("%s: Commit names %v, want %v", s.name, r.Commit.Changed, s.want)
		}
		after, _ := d.CurrentDataVersion(context.Background(), 2, srv.id)
		if (after != before) != s.advanced {
			t.Errorf("%s: DataVersion %d → %d, want advanced %v", s.name, before, after, s.advanced)
		}
	}
}

// TestAttributePathStatusAnswersAsReadDoes: the status
// TopologyDispatcher.AttributePathStatus gives a concrete path is the one
// its Read answers the path with — decided from the topology and the
// server's attribute set where those decide it, and by the read otherwise
// (review II, R2-P5-write-06).
func TestAttributePathStatusAnswersAsReadDoes(t *testing.T) {
	t.Parallel()
	srv := &gateServer{id: 0xFFF1FC04, attrs: []uint32{0x0000}, vals: map[uint32]any{0x0000: uint16(1), 0x0005: uint16(5)}}
	d := gateDispatcher(t, store.PrivilegeAdminister, srv)
	for _, c := range []struct {
		name string
		path im.ConcreteAttributePath
		want im.StatusCode
	}{
		{"an endpoint the topology lacks", concreteAttrPath(9, srv.id, 0x0000), im.StatusUnsupportedEndpoint},
		{"a cluster the endpoint lacks", concreteAttrPath(2, 0xFFF1FC05, 0x0000), im.StatusUnsupportedCluster},
		{"a listed attribute", concreteAttrPath(2, srv.id, 0x0000), im.StatusSuccess},
		{"AttributeList", concreteAttrPath(2, srv.id, 0xFFFB), im.StatusSuccess},
		{"an attribute neither listed nor read", concreteAttrPath(2, srv.id, 0x0001), im.StatusUnsupportedAttribute},
		{"an attribute the server reads without listing it", concreteAttrPath(2, srv.id, 0x0005), im.StatusSuccess},
	} {
		got := d.AttributePathStatus(caseCtx(false), c.path)
		read := d.Read(caseCtx(false), c.path)
		if got != c.want || len(read) != 1 || read[0].Status != got {
			t.Errorf("%s: status %v, Read %+v; want %v from both", c.name, got, read, c.want)
		}
	}
}

// TestTwoAttributeDataOfOneClusterAdvanceTwice (review II, Fable read
// 5b-write, F1's granularity): one WriteRequest whose two attribute data
// change two attributes of one cluster advances its DataVersion twice —
// one commit per attribute data, each advancing once for what it changed
// (matter.js AttributeWriteResponse.ts:433-435; Datasource.ts:1456-1457
// at db021da0).
func TestTwoAttributeDataOfOneClusterAdvanceTwice(t *testing.T) {
	t.Parallel()
	light, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
	if err != nil {
		t.Fatal(err)
	}
	d := gateDispatcher(t, store.PrivilegeAdminister, light)
	before, _ := d.CurrentDataVersion(context.Background(), 2, onoff.ClusterID)
	out := im.HandleWrite(caseCtx(false), d, im.WriteRequest{Writes: []im.AttributeWrite{
		writeOf(concreteAttrPath(2, onoff.ClusterID, onoff.AttrOnTime), uint16(7)),
		writeOf(concreteAttrPath(2, onoff.ClusterID, onoff.AttrOffWaitTime), uint16(9)),
	}})
	if len(out.Results) != 2 || out.Results[0].Status != im.StatusSuccess || out.Results[1].Status != im.StatusSuccess {
		t.Fatalf("results %+v", out.Results)
	}
	after, _ := d.CurrentDataVersion(context.Background(), 2, onoff.ClusterID)
	if after-before != 2 {
		t.Fatalf("DataVersion %d → %d, want two advances for two changing attribute data", before, after)
	}
}
