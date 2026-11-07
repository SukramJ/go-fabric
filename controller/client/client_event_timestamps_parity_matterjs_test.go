// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

// "delivers the timestamps the report decoder resolved", the case matter.js
// #4832 added to packages/node/test/node/ClientEventNotificationTest.ts at
// db021da0: three InitialPress events of one ReportData message — a
// system timestamp and two system deltas — reach the node's event
// delivery with the delta as the wire value and the absolute time the
// report decoder resolved (InputChunk.ts:360-405). matter.js observes
// ChangeNotificationService's EventOccurrence; the Go delivery is
// Hooks.Event with the report's interaction.EventData.

import (
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// systemDeltaReport encodes a ReportData of InitialPress events whose
// timestamps are given as context tag (4 system, 6 delta system) and
// value, as the EventDataIB carries them (Matter Core §10.6.9); the
// encoder of package im writes the epoch timestamp only.
func systemDeltaReport(t *testing.T, subscription uint32, stamps [][2]uint64) []byte {
	t.Helper()
	anon := tlv.AnonymousTag()
	return encode(t, func(e *tlv.Encoder) {
		e.StartStruct(anon)
		e.PutUint(tlv.ContextTag(0), uint64(subscription))
		e.StartArray(tlv.ContextTag(2))
		for i, s := range stamps {
			e.StartStruct(anon)
			e.StartStruct(tlv.ContextTag(1))
			e.StartList(tlv.ContextTag(0))
			e.PutUint(tlv.ContextTag(1), 1)
			e.PutUint(tlv.ContextTag(2), uint64(switchcluster.ClusterID))
			e.PutUint(tlv.ContextTag(3), uint64(switchcluster.EventInitialPress))
			_ = e.EndContainer()
			e.PutUint(tlv.ContextTag(1), uint64(i)+1)
			e.PutUint(tlv.ContextTag(2), uint64(im.EventPriorityInfo))
			e.PutUint(tlv.ContextTag(uint8(s[0])), s[1]) //nolint:gosec // 4 or 6
			e.StartStruct(tlv.ContextTag(7))
			e.PutUint(tlv.ContextTag(0), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		e.PutBool(tlv.ContextTag(4), true)
		e.PutUint(tlv.ContextTag(0xFF), uint64(im.MatterInteractionModelRevision))
		_ = e.EndContainer()
	})
}

func TestClientEventNotificationResolvedTimestampsParity(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sr := newScriptedRegistry(t, seedCommissioned(3), nil)
		n := sr.reg.Get("peer1")
		sr.subscribe(t, n, sr.imageWithOpCreds("ours"), 33)
		sr.send(t, message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: uint16(0x7000 + sr.counter), ProtocolID: im.InteractionModelProtocolID},
			systemDeltaReport(t, 33, [][2]uint64{{4, 1000}, {6, 4}, {6, 1}}))
		waitUntil(t, "the three events", func() bool {
			sr.hooks.mu.Lock()
			defer sr.hooks.mu.Unlock()
			return len(sr.hooks.data) == 3
		})
		client.WaitRegistry(sr.reg)
		sr.hooks.mu.Lock()
		defer sr.hooks.mu.Unlock()
		want := []struct {
			delta    bool
			wire     uint64
			resolved uint64
		}{{false, 1000, 1000}, {true, 4, 1004}, {true, 1, 1005}}
		for i, w := range want {
			e := sr.hooks.data[i]
			if (e.DeltaSystemTimestamp != nil) != w.delta || e.Timestamp != w.wire || e.ResolvedTimestamp == nil || *e.ResolvedTimestamp != w.resolved {
				t.Fatalf("event %d: %+v (resolved %v), want wire %d resolved %d", i+1, e, e.ResolvedTimestamp, w.wire, w.resolved)
			}
		}
	})
}
