// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// nilListingSource lists a nil entry beside a real server — a host
// defect the dispatcher must not turn into a process stop.
type nilListingSource struct{ srv contract.ClusterServer }

func (s nilListingSource) MatterDeviceType() uint16 { return 0x010A }
func (s nilListingSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv, nil}
}

// TestNilClusterServerIsDroppedEverywhere (R2-P6-err-0403) holds the
// class: a nil entry in a source's MatterClusterServers or in a published
// set reaches no dispatcher entry point. Every exported entry point of
// [TopologyDispatcher] that walks an endpoint's servers is called once
// with a path the nil entry would be asked for; none may panic, and the
// drop is logged once per endpoint, not once per dispatch.
func TestNilClusterServerIsDroppedEverywhere(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := &listServer{list: []uint16{1}}
	ep := &Endpoint{ID: 3, Source: nilListingSource{srv: srv}}
	topo := makeTopology(ep)
	topo.Endpoints[0].PublishClusterServers([]contract.ClusterServer{nil})
	d := NewTopologyDispatcher(topo)
	ctx := context.Background()
	other := uint32(0xFFF1FC99)
	cmd := func(e uint16) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: e, Cluster: other, HasEndpoint: true, HasCluster: true, HasCommand: true}
	}
	attr := func(e uint16) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: e, Cluster: other, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	allow := func(uint16, uint32, uint32) im.StatusCode { return im.StatusSuccess }
	allowCmd := func(uint16, uint32, uint32) im.StatusCode { return im.StatusSuccess }
	for _, e := range []uint16{0, 3} {
		calls := map[string]func(){
			"Read(wildcard cluster)": func() { d.Read(ctx, im.ConcreteAttributePath{Endpoint: e, HasEndpoint: true}) },
			"Read(absent cluster)":   func() { d.Read(ctx, attr(e)) },
			"Read(wildcard endpoint)": func() {
				d.Read(ctx, im.ConcreteAttributePath{Cluster: other, HasCluster: true})
			},
			"ReadAtEndpoint":      func() { d.ReadAtEndpoint(ctx, im.ConcreteAttributePath{}, e) },
			"AttributePathStatus": func() { d.AttributePathStatus(ctx, attr(e)) },
			"ExpansionEndpoints":  func() { d.ExpansionEndpoints(im.ConcreteAttributePath{Cluster: other, HasCluster: true}) },
			"Write":               func() { d.Write(ctx, attr(e), im.AttributeValue{Value: uint8(1)}) },
			"WriteAuthorized":     func() { d.WriteAuthorized(ctx, attr(e), im.AttributeValue{Value: uint8(1)}, allow) },
			"MinReadPrivilege":    func() { d.MinReadPrivilege(e, other, 0) },
			"MinWritePrivilege":   func() { d.MinWritePrivilege(e, other, 0) },
			"MinInvokePrivilege":  func() { d.MinInvokePrivilege(e, other, 0) },
			"IsTimedCommand":      func() { d.IsTimedCommand(e, other, 0) },
			"CurrentDataVersion":  func() { d.CurrentDataVersion(ctx, e, other) },
			"Invoke":              func() { d.Invoke(ctx, cmd(e), nil) },
			"InvokeAuthorized":    func() { d.InvokeAuthorized(ctx, cmd(e), nil, allowCmd) },
			"CommandTargets": func() {
				d.CommandTargets(im.ConcreteCommandPath{Cluster: other, HasCluster: true, HasCommand: true}, allowCmd)
			},
			"CommandPathStatus": func() { d.CommandPathStatus(cmd(e)) },
			"EventPathStatus": func() {
				d.EventPathStatus(im.ConcreteEventPath{Endpoint: e, Cluster: other, HasEndpoint: true, HasCluster: true})
			},
			"BumpDataVersionOf":    func() { topo.Endpoints[len(topo.Endpoints)-1].BumpDataVersionOf(other) },
			"ClusterServers twice": func() { ClusterServers(ep); ClusterServers(topo.Endpoints[0]) },
		}
		for name, call := range calls {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("endpoint %d, %s: panicked on a nil server: %v", e, name, r)
					}
				}()
				call()
			}()
		}
	}
	for _, srvs := range [][]contract.ClusterServer{ClusterServers(ep), ClusterServers(topo.Endpoints[0])} {
		for i, s := range srvs {
			if s == nil {
				t.Errorf("ClusterServers handed out a nil entry at %d", i)
			}
		}
	}
	logs := buf.String()
	for _, id := range []string{"endpoint=3", "endpoint=0"} {
		if n := strings.Count(logs, id); n != 1 {
			t.Errorf("nil server on %s logged %d times, want once per endpoint:\n%s", id, n, logs)
		}
	}
}
