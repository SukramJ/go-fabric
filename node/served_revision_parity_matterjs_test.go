// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
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
//
// The same walk holds each server's attribute surface together (review II,
// W3-clucore-13): every attribute it lists — MatterAttributes, or
// MatterReportable where the dispatcher falls back to it — answers a read
// the way the dispatcher reads it (endpoint/dispatcher.go readOne:
// MatterReadFiltered first, then MatterRead), and every reportable
// attribute is a listed one. A listed attribute without an answer reads
// UNSUPPORTED_ATTRIBUTE while AttributeList names it; a reportable one
// outside the list is a report for an attribute AttributeList does not
// name (W3-clucore-11 was one). The listed attributes that answer no read
// at this revision are pinned in unansweredWithDefaults, exactly.
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
	unanswered := map[string]bool{}
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
		reportable := s.srv.MatterReportable()
		listed := reportable
		if lister, ok := s.srv.(contract.ClusterAttributeLister); ok {
			listed = lister.MatterAttributes()
			for _, attr := range reportable {
				if !slices.Contains(listed, attr) {
					t.Errorf("%s: %s (0x%04X) reports attribute 0x%04X, which MatterAttributes does not list", s.where, js.Name, id, attr)
				}
			}
		}
		for _, attr := range listed {
			// The dispatcher answers the globals a server leaves out
			// (synthesizeGlobalRead).
			if attr >= cluster.AttrGlobalGeneratedCommandList {
				continue
			}
			if !answersRead(s.srv, attr) {
				unanswered[fmt.Sprintf("%s %s 0x%04X:0x%04X", s.where, js.Name, id, attr)] = true
			}
		}
	}
	for key := range unanswered {
		if !unansweredWithDefaults[key] {
			t.Errorf("%s: the server lists the attribute and answers no read of it", key)
		}
	}
	for key := range unansweredWithDefaults {
		if !unanswered[key] {
			t.Errorf("%s is pinned in unansweredWithDefaults and is answered now: remove the entry", key)
		}
	}
	t.Logf("%d servers of %d clusters read (root, %d device types; %d need declarations)", len(all), len(clusters), built, refused)
}

// unansweredWithDefaults are the listed attributes that answer no read at
// this revision, as "<where> <cluster> <cluster id>:<attribute id>" —
// measured, not judged: each is a generated server (*spec.Server) the
// device kit builds with its defaults, serving an attribute that has no
// model default the generator renders and no value from the host
// (cluster/spec/server.go MatterRead, "An attribute neither answers is
// absent"). Whether device.New is to refuse such a type, as it refuses the
// ones in the device package's refusedDefaults, or the server is to hold a
// value, is open (review II, the finding W3-clucore-13's guard produced).
// The set is exact in both directions: a new entry fails, and so does one
// that is answered again.
var unansweredWithDefaults = map[string]bool{
	"AirPurifier FanControl 0x0202:0x0001":                                    true,
	"AirQualitySensor AirQuality 0x005B:0x0000":                               true,
	"BasicVideoPlayer MediaPlayback 0x0506:0x0000":                            true,
	"Camera CameraAvStreamManagement 0x0551:0x0002":                           true,
	"Camera CameraAvStreamManagement 0x0551:0x0004":                           true,
	"Camera CameraAvStreamManagement 0x0551:0x0007":                           true,
	"Camera CameraAvStreamManagement 0x0551:0x0018":                           true,
	"CastingVideoPlayer MediaPlayback 0x0506:0x0000":                          true,
	"Chime Chime 0x0556:0x0000":                                               true,
	"ContentApp ApplicationBasic 0x050D:0x0002":                               true,
	"ContentApp ApplicationBasic 0x050D:0x0004":                               true,
	"ContentApp ApplicationBasic 0x050D:0x0005":                               true,
	"ContentApp ApplicationBasic 0x050D:0x0006":                               true,
	"Dishwasher OperationalState 0x0060:0x0004":                               true,
	"Dishwasher OperationalState 0x0060:0x0005":                               true,
	"EnergyEvse EnergyEvseMode 0x009D:0x0000":                                 true,
	"ExtractorHood FanControl 0x0202:0x0001":                                  true,
	"Fan FanControl 0x0202:0x0001":                                            true,
	"Intercom CameraAvStreamManagement 0x0551:0x0007":                         true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0000":             true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0003":             true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0004":             true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0005":             true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0007":             true,
	"JointFabricAdministrator JointFabricDatastore 0x0752:0x0008":             true,
	"LaundryDryer OperationalState 0x0060:0x0004":                             true,
	"LaundryDryer OperationalState 0x0060:0x0005":                             true,
	"LaundryWasher OperationalState 0x0060:0x0004":                            true,
	"LaundryWasher OperationalState 0x0060:0x0005":                            true,
	"ModeSelect ModeSelect 0x0050:0x0000":                                     true,
	"NetworkInfrastructureManager ThreadBorderRouterManagement 0x0452:0x0000": true,
	"NetworkInfrastructureManager ThreadBorderRouterManagement 0x0452:0x0001": true,
	"RoboticVacuumCleaner RvcOperationalState 0x0061:0x0004":                  true,
	"RoboticVacuumCleaner RvcOperationalState 0x0061:0x0005":                  true,
	"RoboticVacuumCleaner RvcRunMode 0x0054:0x0000":                           true,
	"SoilSensor SoilMeasurement 0x0430:0x0000":                                true,
	"ThreadBorderRouter ThreadBorderRouterManagement 0x0452:0x0000":           true,
	"ThreadBorderRouter ThreadBorderRouterManagement 0x0452:0x0001":           true,
	"WaterHeater WaterHeaterManagement 0x0094:0x0005":                         true,
	"WaterHeater WaterHeaterMode 0x009E:0x0000":                               true,
}

// answersRead reports whether srv answers a read of attr on the path the
// dispatcher takes (endpoint/dispatcher.go readOne): the fabric-aware read
// first when the server has one, then MatterRead.
func answersRead(srv contract.ClusterServer, attr uint32) bool {
	if fsr, ok := srv.(contract.FabricScopedReader); ok {
		if _, ok := fsr.MatterReadFiltered(context.Background(), attr); ok {
			return true
		}
	}
	_, ok := srv.MatterRead(attr)
	return ok
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
