// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// answerOf is the status the bridge answers a decode error with: the
// error's own when it carries one, FAILURE otherwise
// (bridge.replyDecodeError).
func answerOf(err error) StatusCode {
	if sce, ok := errors.AsType[StatusCodeError](err); ok {
		return sce.MatterStatusCode()
	}
	return StatusFailure
}

// Builders of the nested members the table places a validation error in.
func ctx(n uint8) tlv.Tag { return tlv.ContextTag(n) }

func attrPathList(e *tlv.Encoder, tag tlv.Tag, endpoint uint64) {
	e.StartList(tag)
	e.PutUint(ctx(tagAttrPathEndpoint), endpoint)
	e.PutUint(ctx(tagAttrPathCluster), 6)
	e.PutUint(ctx(tagAttrPathAttribute), 0)
	_ = e.EndContainer()
}

func eventPathList(e *tlv.Encoder, tag tlv.Tag, endpoint uint64) {
	e.StartList(tag)
	e.PutUint(ctx(tagEventPathEndpoint), endpoint)
	e.PutUint(ctx(tagEventPathCluster), 6)
	_ = e.EndContainer()
}

func clusterPathList(e *tlv.Encoder, tag tlv.Tag, endpoint uint64, withEndpoint bool) {
	e.StartList(tag)
	if withEndpoint {
		e.PutUint(ctx(1), endpoint)
	}
	e.PutUint(ctx(2), 6)
	_ = e.EndContainer()
}

// The nested payloads a Read and a Subscribe share: each puts one array
// member under tag with a validation error inside it.
type nestedPayload struct {
	name string
	put  func(e *tlv.Encoder, readTags bool)
	want StatusCode // the answer without a later type error
}

func readSubscribePayloads() []nestedPayload {
	tagOf := func(readTags bool, read, sub uint8) tlv.Tag {
		if readTags {
			return ctx(read)
		}
		return ctx(sub)
	}
	return []nestedPayload{
		{"attribute path endpoint", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqAttributeRequests, tagSubReqAttributeRequests))
			attrPathList(e, tlv.AnonymousTag(), 70000)
			_ = e.EndContainer()
		}, StatusConstraintError},
		{"event path endpoint", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqEventRequests, tagSubReqEventRequests))
			eventPathList(e, tlv.AnonymousTag(), 70000)
			_ = e.EndContainer()
		}, StatusConstraintError},
		{"event filter without EventMin", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqEventFilters, tagSubReqEventFilters))
			e.StartStruct(tlv.AnonymousTag())
			e.PutUint(ctx(0), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}, StatusInvalidAction},
		{"data version filter version", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqDataVersionFilters, tagSubReqDataVersionFilters))
			e.StartStruct(tlv.AnonymousTag())
			clusterPathList(e, ctx(0), 1, true)
			e.PutUint(ctx(1), 1<<33)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}, StatusConstraintError},
		{"data version filter cluster path endpoint", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqDataVersionFilters, tagSubReqDataVersionFilters))
			e.StartStruct(tlv.AnonymousTag())
			clusterPathList(e, ctx(0), 70000, true)
			e.PutUint(ctx(1), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}, StatusConstraintError},
		{"data version filter cluster path without endpoint", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqDataVersionFilters, tagSubReqDataVersionFilters))
			e.StartStruct(tlv.AnonymousTag())
			clusterPathList(e, ctx(0), 0, false)
			e.PutUint(ctx(1), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}, StatusInvalidAction},
		{"data version filter without DataVersion", func(e *tlv.Encoder, r bool) {
			e.StartArray(tagOf(r, tagReadReqDataVersionFilters, tagSubReqDataVersionFilters))
			e.StartStruct(tlv.AnonymousTag())
			clusterPathList(e, ctx(0), 1, true)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}, StatusInvalidAction},
	}
}

// putBoolOrMistyped puts a boolean field, or an unsigned integer in its
// place.
func putBoolOrMistyped(e *tlv.Encoder, tag tlv.Tag, mistyped bool) {
	if mistyped {
		e.PutUint(tag, 1)
		return
	}
	e.PutBool(tag, false)
}

type laterCase struct {
	name   string
	decode func([]byte) error
	build  func(e *tlv.Encoder, mistyped bool)
	want   StatusCode
}

func laterCases() []laterCase {
	readReq := func(b []byte) error { _, err := UnmarshalReadRequestTLV(tlv.NewDecoder(b)); return err }
	subReq := func(b []byte) error { _, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder(b)); return err }
	writeReq := func(b []byte) error { _, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(b), nil); return err }
	invokeReq := func(b []byte) error { _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil); return err }
	timedReq := func(b []byte) error { _, err := UnmarshalTimedRequestTLV(tlv.NewDecoder(b)); return err }
	statusResp := func(b []byte) error { _, err := UnmarshalStatusResponseTLV(tlv.NewDecoder(b)); return err }

	var cases []laterCase
	for _, p := range readSubscribePayloads() {
		cases = append(cases, laterCase{"ReadRequest: " + p.name, readReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			p.put(e, true)
			putBoolOrMistyped(e, ctx(tagReadReqFabricFiltered), m)
			_ = e.EndContainer()
		}, p.want}, laterCase{"SubscribeRequest: " + p.name, subReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(ctx(tagSubReqKeepSubscriptions), false)
			e.PutUint(ctx(tagSubReqMinIntervalFloor), 1)
			e.PutUint(ctx(tagSubReqMaxIntervalCeiling), 10)
			p.put(e, false)
			putBoolOrMistyped(e, ctx(tagSubReqFabricFiltered), m)
			_ = e.EndContainer()
		}, p.want})
	}
	cases = append(cases,
		laterCase{"ReadRequest: event path endpoint, IsUrgent in the same path", readReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.StartArray(ctx(tagReadReqEventRequests))
			e.StartList(tlv.AnonymousTag())
			e.PutUint(ctx(tagEventPathEndpoint), 70000)
			putBoolOrMistyped(e, ctx(tagEventPathIsUrgent), m)
			_ = e.EndContainer()
			_ = e.EndContainer()
			e.PutBool(ctx(tagReadReqFabricFiltered), false)
			_ = e.EndContainer()
		}, StatusConstraintError},
		laterCase{"SubscribeRequest: MinIntervalFloor", subReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(ctx(tagSubReqKeepSubscriptions), false)
			e.PutUint(ctx(tagSubReqMinIntervalFloor), 70000)
			e.PutUint(ctx(tagSubReqMaxIntervalCeiling), 10)
			putBoolOrMistyped(e, ctx(tagSubReqFabricFiltered), m)
			_ = e.EndContainer()
		}, StatusConstraintError},
		laterCase{"SubscribeRequest: MaxIntervalCeiling", subReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(ctx(tagSubReqKeepSubscriptions), false)
			e.PutUint(ctx(tagSubReqMinIntervalFloor), 1)
			e.PutUint(ctx(tagSubReqMaxIntervalCeiling), 70000)
			putBoolOrMistyped(e, ctx(tagSubReqFabricFiltered), m)
			_ = e.EndContainer()
		}, StatusConstraintError},
	)
	writeWith := func(data func(e *tlv.Encoder)) func(e *tlv.Encoder, m bool) {
		return func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(ctx(tagWriteReqTimedRequest), false)
			e.StartArray(ctx(tagWriteReqWriteRequests))
			e.StartStruct(tlv.AnonymousTag())
			data(e)
			_ = e.EndContainer()
			_ = e.EndContainer()
			putBoolOrMistyped(e, ctx(tagWriteReqMoreChunked), m)
			_ = e.EndContainer()
		}
	}
	invokeWith := func(command, delay func(e *tlv.Encoder)) func(e *tlv.Encoder, m bool) {
		return func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutBool(ctx(tagInvokeReqSuppressResponse), false)
			e.StartArray(ctx(tagInvokeReqInvokeRequests))
			e.StartStruct(tlv.AnonymousTag())
			command(e)
			_ = e.EndContainer()
			_ = e.EndContainer()
			if delay != nil {
				delay(e)
			}
			putBoolOrMistyped(e, ctx(tagInvokeReqTimedRequest), m)
			_ = e.EndContainer()
		}
	}
	commandPath := func(e *tlv.Encoder, endpoint uint64) {
		e.StartList(ctx(tagCmdDataPath))
		e.PutUint(ctx(tagCmdPathEndpoint), endpoint)
		e.PutUint(ctx(tagCmdPathCluster), 6)
		e.PutUint(ctx(tagCmdPathCommand), 1)
		_ = e.EndContainer()
	}
	cases = append(cases,
		laterCase{"WriteRequest: DataVersion", writeReq, writeWith(func(e *tlv.Encoder) {
			e.PutUint(ctx(tagAttributeDataDataVersion), 1<<33)
			attrPathList(e, ctx(tagAttributeDataPath), 1)
			e.PutBool(ctx(tagAttributeDataValue), true)
		}), StatusConstraintError},
		laterCase{"WriteRequest: attribute path endpoint", writeReq, writeWith(func(e *tlv.Encoder) {
			attrPathList(e, ctx(tagAttributeDataPath), 70000)
			e.PutBool(ctx(tagAttributeDataValue), true)
		}), StatusConstraintError},
		laterCase{"WriteRequest: AttributeDataIB without Data", writeReq, writeWith(func(e *tlv.Encoder) {
			attrPathList(e, ctx(tagAttributeDataPath), 1)
		}), StatusInvalidAction},
		laterCase{"InvokeRequest: command path endpoint", invokeReq, invokeWith(func(e *tlv.Encoder) {
			commandPath(e, 70000)
		}, nil), StatusConstraintError},
		laterCase{"InvokeRequest: CommandRef", invokeReq, invokeWith(func(e *tlv.Encoder) {
			commandPath(e, 1)
			e.PutUint(ctx(tagCmdDataRef), 70000)
		}, nil), StatusConstraintError},
		laterCase{"InvokeRequest: CommandDataIB without CommandPath", invokeReq, invokeWith(func(e *tlv.Encoder) {
			e.PutUint(ctx(tagCmdDataRef), 1)
		}, nil), StatusInvalidAction},
		laterCase{"InvokeRequest: DelayReportData", invokeReq, invokeWith(func(e *tlv.Encoder) {
			commandPath(e, 1)
		}, func(e *tlv.Encoder) {
			e.StartStruct(ctx(tagInvokeReqDelayReportData))
			e.PutUint(ctx(0), 70000)
			e.PutUint(ctx(1), 1)
			_ = e.EndContainer()
		}), StatusConstraintError},
		laterCase{"TimedRequest: Timeout", timedReq, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutUint(ctx(tagTimedReqTimeout), 70000)
			if m {
				e.PutBool(tlv.AnonymousTag(), true) // a member without a context tag
			}
			_ = e.EndContainer()
		}, StatusConstraintError},
		laterCase{"StatusResponse: Status", statusResp, func(e *tlv.Encoder, m bool) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutUint(ctx(tagStatusResponseStatus), 300)
			if m {
				e.PutBool(tlv.AnonymousTag(), true)
			}
			_ = e.EndContainer()
		}, StatusConstraintError},
	)
	return cases
}

// TestValidationErrorsYieldToALaterTypeError (review II, 5a-2 A1-d): a
// request decoder keeps an out-of-range integer or a missing mandatory
// field for the end of the decode and goes on, so a mistyped member after
// it — at any depth — answers FAILURE; without one the request answers the
// kept error's status. matter.js throws every type error while it decodes
// and validates bounds and mandatory fields afterwards
// (packages/types/src/schema/Schema.ts:18-24 at db021da0). Every request
// decoder loop of this package is reached: the Read, Subscribe, Write,
// Invoke and Timed requests, the StatusResponse, the attribute, event,
// command and cluster paths, the event and data-version filters, the
// AttributeDataIB, the CommandDataIB and DelayReportData.
func TestValidationErrorsYieldToALaterTypeError(t *testing.T) {
	t.Parallel()
	for _, tc := range laterCases() {
		for _, mistyped := range []bool{false, true} {
			e := tlv.NewEncoder()
			tc.build(e, mistyped)
			b, err := e.Bytes()
			if err != nil {
				t.Fatalf("%s: encode: %v", tc.name, err)
			}
			derr := tc.decode(b)
			if derr == nil {
				t.Errorf("%s (mistyped %v): decoded, want an error", tc.name, mistyped)
				continue
			}
			want := tc.want
			if mistyped {
				want = StatusFailure
			}
			if got := answerOf(derr); got != want {
				t.Errorf("%s (mistyped %v): answered %s, want %s (%v)", tc.name, mistyped, got, want, derr)
			}
		}
	}
}
