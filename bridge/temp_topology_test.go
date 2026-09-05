// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// A populated topology built straight from [endpoint.Spec] values, with no
// host device model behind it. Tests that need many reportable attribute
// paths — report chunking above all — care only that the assembly yields
// enough sensor endpoints, not what a host projected them from.

import (
	"context"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// fakeTempSource is a temperature measurement source with a
// manually-fireable change callback, so a test can drive the production
// notifier wiring without a device behind it.
type fakeTempSource struct {
	mu    sync.Mutex
	cbs   []func()
	value float64
}

var (
	_ contract.MeasurementSource      = (*fakeTempSource)(nil)
	_ contract.FloatMeasurementSource = (*fakeTempSource)(nil)
	_ contract.ChangeNotifier         = (*fakeTempSource)(nil)
)

func (*fakeTempSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementTemperature
}

func (s *fakeTempSource) MatterFloatValue() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, true
}

func (s *fakeTempSource) OnMatterValueChanged(cb func()) func() {
	if cb == nil {
		return func() {}
	}
	s.mu.Lock()
	s.cbs = append(s.cbs, cb)
	idx := len(s.cbs) - 1
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		if idx < len(s.cbs) {
			s.cbs[idx] = nil
		}
		s.mu.Unlock()
	}
}

// manyTempSensorsSnapshotter assembles n temperature-sensor endpoints on one
// central. Thirty is chosen so the reportable-path count clears 100 — the
// threshold above which one ReportData cannot fit a single datagram, which
// is what makes the chunking path observable.
func manyTempSensorsSnapshotter(n int) (Snapshotter, []*fakeTempSource) {
	sources := make([]*fakeTempSource, 0, n)
	specs := make([]endpoint.Spec, 0, n)
	for i := range n {
		src := &fakeTempSource{value: float64(20 + i)}
		sources = append(sources, src)
		specs = append(specs, endpoint.Spec{
			StableKey:      endpoint.StringKey(fmt.Sprintf("ccu1|MANYTMP|%d|measurement|ACTUAL_TEMPERATURE", i+1)),
			DeviceAddress:  "MANYTMP",
			DeviceType:     contract.MeasurementClassDeviceType(contract.MeasurementTemperature),
			FriendlyName:   fmt.Sprintf("Many-Temp %d", i+1),
			ChannelAddress: fmt.Sprintf("MANYTMP:%d", i+1),
			Measurement:    src,
		})
	}
	asm, err := endpoint.New(NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	return func(ctx context.Context) (*endpoint.Topology, error) {
		if err != nil {
			return nil, err
		}
		return asm.Assemble(ctx, []endpoint.Snapshot{{
			Scope:         "ccu1",
			Endpoints:     specs,
			ModelComplete: true,
		}})
	}, sources
}

// encodeScenarioWriteRequest builds the TLV body of an IM WriteRequest
// setting one boolean attribute. Written out by hand rather than through a
// helper so the tests that use it pin the exact wire shape a commissioner
// sends, field tag by field tag.
func encodeScenarioWriteRequest(path im.ConcreteAttributePath, value bool) ([]byte, error) {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))     // WriteRequests
	enc.StartStruct(tlv.AnonymousTag())   // AttributeDataIB
	enc.PutUint(tlv.ContextTag(0), 0)     // DataVersion (optional, 0 = no constraint)
	path.MarshalTLV(enc, tlv.ContextTag(1))
	enc.PutBool(tlv.ContextTag(2), value)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), uint64(im.MatterInteractionModelRevision))
	_ = enc.EndContainer()
	return enc.Bytes()
}

// manyTempSensorsSnapshotterForTest is the arity the chunking test wants:
// the snapshotter alone, with the sources discarded because that test drives
// the report path directly rather than through a value change.
func manyTempSensorsSnapshotterForTest() Snapshotter {
	s, _ := manyTempSensorsSnapshotter(30)
	return s
}
