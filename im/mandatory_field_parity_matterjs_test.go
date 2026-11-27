// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A mandatory IM field that is absent is InvalidAction (review 2026-11
// W2-wire-04): matter.js packages/types/src/tlv/TlvObject.ts:261 throws
// ValidationMandatoryFieldMissingError for a TlvField, chip's parsers
// return CHIP_END_OF_TLV and src/app/ReadHandler.cpp:208-214 answers it
// InvalidAction.

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

func TestMandatoryFieldParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		decode func() error
	}{
		{"ReadRequest without IsFabricFiltered (TlvReadRequest.ts:30)", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder([]byte{0x15, 0x36, 0x00, 0x17, 0x18, 0x18, 0x18}))
			return err
		}},
		{"SubscribeRequest without intervals (TlvSubscribeRequest.ts:24-27)", func() error {
			_, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder([]byte{0x15, 0x28, 0x00, 0x28, 0x07, 0x18}))
			return err
		}},
		{"TimedRequest without Timeout (TlvTimedRequest.ts:15)", func() error {
			_, err := UnmarshalTimedRequestTLV(tlv.NewDecoder([]byte{0x15, 0x18}))
			return err
		}},
		{"EventFilterIB without EventMin (TlvEventFilter.ts:17)", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder([]byte{0x15, 0x36, 0x02, 0x15, 0x18, 0x18, 0x28, 0x03, 0x18}))
			return err
		}},
		{"DataVersionFilterIB without Path (TlvDataVersionFilter.ts:16)", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder([]byte{0x15, 0x28, 0x03, 0x36, 0x04, 0x15, 0x24, 0x01, 0x05, 0x18, 0x18, 0x18}))
			return err
		}},
		{"DataVersionFilterIB ClusterPathIB without cluster (TlvClusterPath.ts:17)", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder([]byte{0x15, 0x28, 0x03, 0x36, 0x04, 0x15, 0x37, 0x00, 0x24, 0x01, 0x01, 0x18, 0x24, 0x01, 0x05, 0x18, 0x18, 0x18}))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.decode()
			var sce StatusCodeError
			if !errors.Is(err, ErrMissingField) || !errors.As(err, &sce) || sce.MatterStatusCode() != StatusInvalidAction {
				t.Fatalf("decode error = %v, want ErrMissingField (InvalidAction)", err)
			}
		})
	}
}

// TestMandatoryWriteInvokeFieldParity: the mandatory fields of the Write
// and Invoke request messages and of their AttributeDataIB and
// CommandDataIB — TlvWriteRequest.ts:32 writeRequests and
// TlvInvokeRequest.ts:33 invokeRequests at db91886c;
// TlvAttributeData.ts:18 path, :19 data and TlvCommandData.ts:17
// commandPath at db021da0. Each is a TlvField, so a message without it
// fails validation with ValidationMandatoryFieldMissingError
// (packages/types/src/tlv/TlvObject.ts:255-262 at db021da0), answered
// InvalidAction. SuppressResponse and TimedRequest of both requests are
// TlvOptionalField (TlvWriteRequest.ts:22, :29, TlvInvokeRequest.ts:23,
// :30 at db91886c) and may be left out: the request decodes with the flag
// false (the controls).
func TestMandatoryWriteInvokeFieldParity(t *testing.T) {
	t.Parallel()
	write := func(b []byte) func() error {
		return func() error { _, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(b), nil); return err }
	}
	invoke := func(b []byte) func() error {
		return func() error { _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil); return err }
	}
	// 0x15 struct, 0x18 end, 0x28/0x29 tt = false/true, 0x36 tt = array,
	// 0x37 tt = list, 0x24 tt v = uint8, 0x25 tt vvvv = uint16.
	missing := []struct {
		name   string
		decode func() error
	}{
		{"WriteRequest without WriteRequests", write([]byte{0x15, 0x28, 0x01, 0x18})},
		{"AttributeDataIB without Path and Data", write([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x15, 0x18, 0x18, 0x18})},
		{"AttributeDataIB without Data", write([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x15, 0x37, 0x01, 0x24, 0x02, 0x01, 0x24, 0x03, 0x06, 0x25, 0x04, 0x01, 0x40, 0x18, 0x18, 0x18, 0x18})},
		{"AttributeDataIB without Path", write([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x15, 0x24, 0x02, 0x07, 0x18, 0x18, 0x18})},
		{"InvokeRequest without any field", invoke([]byte{0x15, 0x18})},
		{"InvokeRequest without InvokeRequests", invoke([]byte{0x15, 0x28, 0x00, 0x28, 0x01, 0x18})},
		{"CommandDataIB without CommandPath", invoke([]byte{0x15, 0x28, 0x00, 0x28, 0x01, 0x36, 0x02, 0x15, 0x18, 0x18, 0x18})},
	}
	for _, tc := range missing {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.decode()
			var sce StatusCodeError
			if !errors.Is(err, ErrMissingField) || !errors.As(err, &sce) || sce.MatterStatusCode() != StatusInvalidAction {
				t.Fatalf("decode error = %v, want ErrMissingField (InvalidAction)", err)
			}
		})
	}
	// Controls: the smallest complete messages decode.
	for name, decode := range map[string]func() error{
		"WriteRequest without the optional SuppressResponse": write([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x18, 0x18}),
		"WriteRequest with one complete AttributeDataIB":     write([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x15, 0x37, 0x01, 0x24, 0x02, 0x01, 0x24, 0x03, 0x06, 0x25, 0x04, 0x01, 0x40, 0x18, 0x24, 0x02, 0x07, 0x18, 0x18, 0x18}),
		"InvokeRequest with no commands":                     invoke([]byte{0x15, 0x28, 0x00, 0x28, 0x01, 0x36, 0x02, 0x18, 0x18}),
		"WriteRequest without TimedRequest":                  write([]byte{0x15, 0x36, 0x02, 0x18, 0x18}),
		"InvokeRequest without SuppressResponse":             invoke([]byte{0x15, 0x28, 0x01, 0x36, 0x02, 0x18, 0x18}),
		"InvokeRequest without TimedRequest":                 invoke([]byte{0x15, 0x28, 0x00, 0x36, 0x02, 0x18, 0x18}),
	} {
		if err := decode(); err != nil {
			t.Errorf("%s: %v, want nil", name, err)
		}
	}
}
