// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"log/slog"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
)

// TestServedClusterRevisionAndFeatureMap reads ClusterRevision (0xFFFD)
// and FeatureMap (0xFFFC) off constructed servers and holds them to
// parity/schema.json, matter.js at the pin (W6-test-10): every server a
// node mounts on its root, and every server the device kit builds for a
// generated device type with its defaults, answers the snapshot's revision
// and a FeatureMap whose every bit is a feature the snapshot defines for
// the cluster. It replaces the tests that compared a revision constant
// with the snapshot it was generated from: a server that states its
// revision by hand (OperationalCredentials, AccessControl,
// GroupKeyManagement) and one that takes it from its generated definition
// are read the same way, as a controller reads them.
func TestServedClusterRevisionAndFeatureMap(t *testing.T) {
	t.Parallel()
	type served struct {
		where string
		srv   contract.ClusterServer
	}
	var all []served
	d := quietSite(t).AddDevice(fabrictest.Topology{})
	for _, srv := range d.Node().Root().Servers() {
		all = append(all, served{"root", srv})
	}
	built, refused := 0, 0
	for _, ty := range device.Types() {
		dev, err := device.New(ty, device.Options{StableKey: endpoint.StringKey("rev:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler)})
		if err != nil {
			// A type whose defaults need the host's declarations (a
			// feature selection, a port); the device package's
			// TestEveryTypeConstructsAndConforms lists each.
			refused++
			continue
		}
		built++
		for _, srv := range dev.Servers() {
			all = append(all, served{ty.Name, srv})
		}
	}
	if built < 60 {
		t.Fatalf("only %d of %d device types build with their defaults", built, built+refused)
	}
	clusters := map[uint32]bool{}
	for _, s := range all {
		id := s.srv.MatterClusterID()
		clusters[id] = true
		js := paritytest.ClusterSnapshot(t, id)
		rev, ok := readUint(s.srv, cluster.AttrGlobalClusterRevision)
		if !ok || rev != uint64(js.Revision) {
			t.Errorf("%s: %s (0x%04X) answers ClusterRevision %v (%v), matter.js %d", s.where, js.Name, id, rev, ok, js.Revision)
		}
		fm, ok := readUint(s.srv, cluster.AttrGlobalFeatureMap)
		if !ok {
			t.Errorf("%s: %s (0x%04X) answers no FeatureMap", s.where, js.Name, id)
			continue
		}
		var defined uint64
		for _, f := range js.Features {
			defined |= 1 << f.Bit
		}
		if extra := fm &^ defined; extra != 0 {
			t.Errorf("%s: %s (0x%04X) FeatureMap 0x%X sets bits 0x%X matter.js does not define", s.where, js.Name, id, fm, extra)
		}
	}
	t.Logf("%d servers of %d clusters read (root, %d device types; %d need declarations)", len(all), len(clusters), built, refused)
}

// readUint reads a global attribute as an unsigned value, whatever unsigned
// type the server answers it in.
func readUint(srv contract.ClusterServer, attr uint32) (uint64, bool) {
	v, ok := srv.MatterRead(attr)
	if !ok {
		return 0, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		return rv.Uint(), true
	default:
		return 0, false
	}
}
