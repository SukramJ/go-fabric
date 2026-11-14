// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"runtime"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// quotaNode is a node over st whose image holds at most quota bytes.
func quotaNode(t *testing.T, st Store, quota int) (*Node, *logRecorder) {
	t.Helper()
	logs := &logRecorder{}
	n, err := New(Config{ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}), Logger: slog.New(logs), maxImageBytes: quota})
	if err != nil {
		t.Fatal(err)
	}
	return n, logs
}

// blobList is a list of count octet strings of size bytes each.
func blobList(count, size int) spec.Node {
	list := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	blob := make([]byte, size)
	for range count {
		list.Children = append(list.Children, spec.Node{El: tlv.Element{Type: tlv.TypeOctetStr2, Octets: blob}})
	}
	return list
}

// storedBytes is the bytes of the attribute values st holds for node "n"
// on endpoints at or above from, and the number of such endpoints.
func storedBytes(t *testing.T, st Store, from uint16) (total, endpoints int) {
	t.Helper()
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.Number < from {
			continue
		}
		endpoints++
		for _, c := range e.Clusters {
			for _, v := range c.Values {
				total += len(v)
			}
		}
	}
	return total, endpoints
}

// askedFor is the scope of a read of one concrete cluster path.
func askedFor(ep uint16, cluster uint32) scope {
	return newScope([]im.ConcreteAttributePath{{Endpoint: ep, Cluster: cluster, HasEndpoint: true, HasCluster: true}}, false)
}

// TestImageKeepsWhatTheStructureNamesOrTheReadAskedFor: under a wildcard
// the image keeps the endpoints the root's PartsList names; a path outside
// them is dropped and logged once for the node. A read that names a path
// keeps its data.
func TestImageKeepsWhatTheStructureNamesOrTheReadAskedFor(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _, logs := testNode(t, st)
	n.apply(wildcard, &bridge(1).r, true)

	b := &report{}
	b.attr(1000, 0xFFF1_FC00, 0, uintNode(1), 1) // an endpoint no PartsList names
	b.attr(1001, 0xFFF1_FC00, 0, uintNode(1), 1) // a second one: one log line for the node
	b.attr(2, onoff.ClusterID, 0, boolNode(true), 2)
	n.apply(wildcard, &b.r, true)

	if n.s.endpoints[1000] != nil || n.s.endpoints[1001] != nil {
		t.Error("the image holds an endpoint no PartsList names")
	}
	if v := n.Endpoint(2).clusters[onoff.ClusterID].cache.values[0]; !v.El.Bool {
		t.Error("a named cluster's value was not applied beside the dropped ones")
	}
	if total, eps := storedBytes(t, st, 1000); eps != 0 || total != 0 {
		t.Errorf("the store holds %d endpoints (%d bytes) no PartsList names", eps, total)
	}
	if got := logs.count("matter.controller.client.report_path_dropped"); got != 1 {
		t.Errorf("the dropped paths were logged %d times; want once for the node", got)
	}

	asked := &report{}
	asked.attr(1000, 0xFFF1_FC00, 0, uintNode(7), 1)
	n.apply(askedFor(1000, 0xFFF1_FC00), &asked.r, true)
	if ep := n.s.endpoints[1000]; ep == nil || ep.clusters[0xFFF1_FC00] == nil {
		t.Error("a path the read asked for was not kept")
	}
}

// TestImageKeepsAnAttributeTheReadAskedForOnEveryEndpoint: a read whose
// path names only an attribute asks for it on every endpoint and cluster,
// so the image keeps those values on endpoints no PartsList names yet; an
// other attribute there is not asked for and is dropped.
func TestImageKeepsAnAttributeTheReadAskedForOnEveryEndpoint(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _, logs := testNode(t, st)
	sc := newScope([]im.ConcreteAttributePath{{Attribute: spec.AttrClusterRevision, HasAttribute: true}}, false)
	b := &report{}
	b.attr(0, clBasicInfo, spec.AttrClusterRevision, uintNode(3), 1)
	b.attr(1, onoff.ClusterID, spec.AttrClusterRevision, uintNode(6), 1)
	b.attr(2, onoff.ClusterID, spec.AttrClusterRevision, uintNode(6), 1)
	b.attr(2, onoff.ClusterID, 0, boolNode(true), 1)
	n.apply(sc, &b.r, true)
	for _, ep := range []uint16{1, 2} {
		e := n.s.endpoints[ep]
		if e == nil || e.clusters[onoff.ClusterID] == nil {
			t.Fatalf("endpoint %d's ClusterRevision, which the read asked for, is not in the image", ep)
		}
		if _, ok := e.clusters[onoff.ClusterID].cache.values[spec.AttrClusterRevision]; !ok {
			t.Errorf("endpoint %d's ClusterRevision, which the read asked for, is not in the image", ep)
		}
	}
	if _, ok := n.s.endpoints[2].clusters[onoff.ClusterID].cache.values[0]; ok {
		t.Error("an attribute the read did not ask for, on an endpoint no PartsList names, is in the image")
	}
	if got := logs.count("matter.controller.client.report_path_dropped"); got != 1 {
		t.Errorf("the dropped path was logged %d times; want once", got)
	}
}

// TestImageQuotaRefusesAReportBeyondIt: a report that would raise the
// image above the node's configured quota is refused whole and the image
// stays as it was; reports within it are kept.
func TestImageQuotaRefusesAReportBeyondIt(t *testing.T) {
	t.Parallel()
	const quota = 64 << 10
	st := &MemoryStore{}
	n, logs := quotaNode(t, st, quota)
	n.apply(wildcard, &bridge(1).r, true)

	refused := 0
	for i := range 40 {
		ep := uint16(1000 + i) //nolint:gosec // test endpoint numbers
		b := &report{}
		b.attr(ep, 0xFFF1_FC00, 0, blobList(4, 1000), 1)
		n.apply(askedFor(ep, 0xFFF1_FC00), &b.r, true)
		if n.s.endpoints[ep] == nil {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("no report was refused at the quota")
	}
	if got := n.s.imageBytes(); got > quota {
		t.Errorf("the image holds %d bytes; want at most the configured %d", got, quota)
	}
	if total, _ := storedBytes(t, st, 0); total > quota {
		t.Errorf("the store holds %d bytes of the node's image; want at most the configured %d", total, quota)
	}
	if got := logs.count("matter.controller.client.image_quota_reached"); got != 1 {
		t.Errorf("the refusals were logged %d times at warn; want once for the node", got)
	}
}

// TestImageBoundedAcrossReports: forty reports of 1 MiB each, every one
// inside the per-session report bound, leave nothing of the endpoints no
// PartsList names under a wildcard; asked for one by one, they leave at
// most the default quota.
func TestImageBoundedAcrossReports(t *testing.T) {
	t.Parallel()
	for _, wild := range []bool{true, false} {
		st := &MemoryStore{}
		n, _, _ := testNode(t, st)
		n.apply(wildcard, &bridge(1).r, true)
		for i := range 40 {
			ep := uint16(1000 + i) //nolint:gosec // test endpoint numbers
			b := &report{}
			b.attr(ep, 0xFFF1_FC00, 0, blobList(1024, 1000), 1)
			sc := askedFor(ep, 0xFFF1_FC00)
			if wild {
				sc = wildcard
			}
			n.apply(sc, &b.r, true)
		}
		total, eps := storedBytes(t, st, 1000)
		switch {
		case wild && eps != 0:
			t.Errorf("under a wildcard the store holds %d endpoints (%d bytes) no PartsList names", eps, total)
		case !wild && total > DefaultMaxImageBytesPerNode:
			t.Errorf("asked-for reports left %d bytes in %d endpoints; want at most DefaultMaxImageBytesPerNode %d", total, eps, DefaultMaxImageBytesPerNode)
		}
	}
}

// TestStoredImageBeyondQuotaIsCutBackAtLoad: an image stored above the
// quota loads; what the structure does not name is dropped from the image
// and the store, what it names stays.
func TestStoredImageBeyondQuotaIsCutBackAtLoad(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _ := quotaNode(t, st, 0)
	n.apply(wildcard, &bridge(1).r, true)
	for i := range 4 {
		ep := uint16(1000 + i) //nolint:gosec // test endpoint numbers
		b := &report{}
		b.attr(ep, 0xFFF1_FC00, 0, blobList(16, 1000), 1)
		n.apply(askedFor(ep, 0xFFF1_FC00), &b.r, true)
	}
	if _, eps := storedBytes(t, st, 1000); eps != 4 {
		t.Fatalf("the store holds %d asked-for endpoints before the reload; want 4", eps)
	}

	reloaded, logs := quotaNode(t, st, 32<<10)
	if reloaded.s.endpoints[1000] != nil {
		t.Error("an endpoint the structure does not name survived the cut at load")
	}
	if reloaded.Endpoint(2) == nil || reloaded.Endpoint(2).clusters[onoff.ClusterID] == nil {
		t.Error("the cut at load dropped what the structure names")
	}
	if total, eps := storedBytes(t, st, 1000); eps != 0 {
		t.Errorf("the store still holds %d endpoints (%d bytes) the structure does not name", eps, total)
	}
	if !logs.has("matter.controller.client.image_cut_at_load") {
		t.Error("the cut at load was not logged")
	}
}

// count is how often msg was logged.
func (h *logRecorder) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// TestStoredImageIsChargedAsTheImage: an image is charged the same in the
// store as in memory, so the cut at load and the refusal of a report judge
// one measure.
func TestStoredImageIsChargedAsTheImage(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _ := quotaNode(t, st, 0)
	n.apply(wildcard, &bridge(3).r, true)
	ApplyAsked(n, 1000, 0xFFF1_FC00, blobList(16, 100))
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	if image, stored := n.s.imageBytes(), storedImageBytes(eps); image != stored {
		t.Errorf("the image is charged %d bytes in memory and %d in the store", image, stored)
	}
}

// imageHeapFactor is the heap the image may hold per byte it is charged,
// the factor DefaultMaxImageBytesPerNode's charges state.
const imageHeapFactor = 2

// rowStore counts the rows a store would hold and keeps no value, so the
// heap a test measures is the image's.
type rowStore struct {
	MemoryStore
	valueRows int
	clusters  map[[2]uint32]bool
}

func (r *rowStore) Save(_ string, ep uint16, cluster uint32, values map[uint32][]byte, _ *uint32) error {
	for _, v := range values {
		if v != nil {
			r.valueRows++
		}
	}
	r.clusters[[2]uint32{uint32(ep), cluster}] = true
	return nil
}

// heapInUse is the live heap after a collection.
func heapInUse() int64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc) //nolint:gosec // a heap size fits int64
}

// TestImageQuotaBoundsMemoryAndRows: what the quota charges for an image
// bounds the heap the image holds, within imageHeapFactor, and the rows its
// store holds — for values of one byte under fresh attribute ids, under
// fresh cluster ids, on fresh endpoints a read asked for, and for lists of
// one-byte elements. Each mode is offered more than the quota in reports of
// a thousand entries. Not parallel: it measures the heap.
func TestImageQuotaBoundsMemoryAndRows(t *testing.T) { //nolint:paralleltest // measures the heap
	const quota, reports, perReport = 4 << 20, 40, 1000
	for _, mode := range []string{"fresh attributes", "fresh clusters", "fresh endpoints", "lists of one-byte elements"} {
		st := &rowStore{clusters: map[[2]uint32]bool{}}
		n, _ := quotaNode(t, st, quota)
		n.apply(wildcard, &bridge(1).r, true)
		charged0, rows0, clusters0 := n.s.imageBytes(), st.valueRows, len(st.clusters)
		heap0 := heapInUse()
		for r := range reports {
			b := &report{}
			sc := wildcard
			for i := range perReport {
				k := uint32(r*perReport + i) //nolint:gosec // a small test count
				switch mode {
				case "fresh attributes":
					b.attr(0, 0xFFF1_FC00, 0x1000+k, boolNode(true), 1)
				case "fresh clusters":
					b.attr(0, 0x0001_0000+k, 0, boolNode(true), 1)
				case "fresh endpoints":
					ep := uint16(2000 + k) //nolint:gosec // a small test count
					b.attr(ep, 0xFFF1_FC00, 0, boolNode(true), 1)
				case "lists of one-byte elements":
					if i == 0 {
						list := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
						for range perReport {
							list.Children = append(list.Children, boolNode(true))
						}
						b.attr(0, 0xFFF1_FC00, 0x1000+uint32(r), list, 1) //nolint:gosec // a small test count
					}
				}
			}
			if mode == "fresh endpoints" {
				paths := make([]im.ConcreteAttributePath, 0, perReport)
				for _, a := range b.r.Attributes {
					paths = append(paths, im.ConcreteAttributePath{Endpoint: a.Path.Endpoint, HasEndpoint: true})
				}
				sc = newScope(paths, false)
			}
			n.apply(sc, &b.r, true)
		}
		heap := heapInUse() - heap0
		charged := n.s.imageBytes() - charged0
		rows, versionRows := st.valueRows-rows0, len(st.clusters)-clusters0
		t.Logf("%s: charged %d of the quota %d, heap %d bytes, %d value rows, %d version rows", mode, charged, quota, heap, rows, versionRows)
		if charged <= 0 {
			t.Fatalf("%s: nothing was admitted", mode)
		}
		if heap > imageHeapFactor*int64(charged) {
			t.Errorf("%s: the image holds %d heap bytes for %d charged bytes (%.0f per charged byte); want at most %d per charged byte",
				mode, heap, charged, float64(heap)/float64(charged), imageHeapFactor)
		}
		// A value row is charged at least one element and its attribute.
		if maxRows := quota / (imageAttributeBytes + imageElementBytes); rows > maxRows || versionRows > quota/imageClusterBytes {
			t.Errorf("%s: the store holds %d value rows and %d version rows; want at most %d and %d (the quota over a row's charge)",
				mode, rows, versionRows, maxRows, quota/imageClusterBytes)
		}
		runtime.KeepAlive(n)
	}
}
