// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	padef "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// pushWire is req as the server receives it from the wire: encoded, then
// decoded by the definition's request decoder, the path every controller's
// request takes before the server runs. A decode refusal is returned as
// the error, as the interaction layer answers it.
func pushWire(t *testing.T, p *camera.PushTransport, cmd uint32, req spec.Encodable) (any, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	req.EncodeTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	fields, ok, err := spec.DecodeRequest(padef.ClusterID, cmd, dec, open)
	if !ok {
		t.Fatalf("command 0x%02X has no request decoder", cmd)
	}
	if err != nil {
		return nil, err
	}
	return pushInvoke(p, cmd, fields)
}

// TestPushTransportCertificationSteps holds the PushAvStreamTransport
// answers four certification steps expect, connectedhomeip's (owner
// decision 69): the fields they exercise are the server's to judge, not
// the model constraint's (script/clustergen/relaxed.go).
func TestPushTransportCertificationSteps(t *testing.T) {
	empty := func(o *padef.TransportOptionsStruct) {
		v, a := []padef.VideoStreamStruct{}, []padef.AudioStreamStruct{}
		o.VideoStreams, o.AudioStreams = &v, &a
	}

	// TC-PAVST-2.3 step 25 (TC_PAVST_2_3.py:435-443): a motion trigger
	// with MotionSensitivity 11 on a camera with PERZONESENS is
	// INVALID_COMMAND — a MotionSensitivity is not expected there
	// (PushAVStreamTransportLogic.cpp:571-577 at 6170af84), whatever its
	// value; the model's "1 to 10" answered CONSTRAINT_ERROR first.
	t.Run("TC-PAVST-2.3 step 25", func(t *testing.T) {
		p, _ := newPush(t)
		o := motionOptions()
		v := spec.ValueOf[uint8](11)
		o.TriggerOptions.MotionSensitivity = &v
		_, err := pushWire(t, p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		wantCode(t, "AllocatePushTransport with MotionSensitivity 11", statusOf(err), im.StatusInvalidCommand)
	})
	// Without PERZONESENS the value is judged by the server against 1 to
	// 10, CONSTRAINT_ERROR as the model would (:556-569).
	t.Run("MotionSensitivity 11 without PERZONESENS", func(t *testing.T) {
		p, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Features = 0 })
		o := motionOptions()
		zonesNoSens(&o)
		v := spec.ValueOf[uint8](11)
		o.TriggerOptions.MotionSensitivity = &v
		_, err := pushWire(t, p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		wantCode(t, "AllocatePushTransport with MotionSensitivity 11", statusOf(err), im.StatusConstraintError)
	})

	// TC-PAVST-2.4 step 5 (TC_PAVST_2_4.py:198-212) and TC-PAVST-2.6
	// step 8 (TC_PAVST_2_6.py:283-299): ModifyPushTransport with the
	// deprecated stream ids and empty VideoStreams and AudioStreams lists
	// succeeds — an empty list is no stream field
	// (PushAVStreamTransportLogic.cpp:672-678); the model's "1 to 16"
	// answered CONSTRAINT_ERROR first.
	t.Run("TC-PAVST-2.4 step 5, TC-PAVST-2.6 step 8", func(t *testing.T) {
		p, _ := newPush(t)
		conn := allocatePush(t, p, motionOptions())
		o := motionOptions()
		empty(&o)
		_, err := pushWire(t, p, padef.CmdModifyPushTransport, padef.ModifyPushTransportRequest{ConnectionId: conn.ConnectionId, TransportOptions: o})
		wantCode(t, "ModifyPushTransport with empty stream lists", statusOf(err), im.StatusSuccess)
	})

	// TC-PAVST-2.11 step 21a (TC_PAVST_2_11.py:462-490): VideoStreamID
	// beside a VideoStreams list naming the same stream, AudioStreams
	// empty, is INVALID_COMMAND — old and new stream fields together
	// (PushAVStreamTransportLogic.cpp:681-682); the model's "1 to 16" on
	// the empty AudioStreams answered CONSTRAINT_ERROR first.
	t.Run("TC-PAVST-2.11 step 21a", func(t *testing.T) {
		p, _ := newPush(t)
		o := motionOptions()
		o.AudioStreamId = nil
		v, a := []padef.VideoStreamStruct{{VideoStreamId: 1, VideoStreamName: "video"}}, []padef.AudioStreamStruct{}
		o.VideoStreams, o.AudioStreams = &v, &a
		_, err := pushWire(t, p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		wantCode(t, "AllocatePushTransport with old and new stream fields", statusOf(err), im.StatusInvalidCommand)
	})

	// More than 16 streams stays a CONSTRAINT_ERROR, now the server's
	// (pushtransport.go's maxPushStreams).
	t.Run("17 video streams", func(t *testing.T) {
		p, _ := newPush(t)
		o := motionOptions()
		o.VideoStreamId, o.AudioStreamId = nil, nil
		v := make([]padef.VideoStreamStruct, 17)
		for i := range v {
			v[i] = padef.VideoStreamStruct{VideoStreamId: uint16(i + 1), VideoStreamName: string(rune('a' + i))}
		}
		o.VideoStreams = &v
		_, err := pushWire(t, p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		wantCode(t, "AllocatePushTransport with 17 video streams", statusOf(err), im.StatusConstraintError)
	})
}
