// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A mandatory IM field that is absent is InvalidAction (review 2026-11
// W2-wire-04): matter.js packages/types/src/tlv/TlvObject.ts:258 throws
// ValidationMandatoryFieldMissingError for a TlvField, chip's parsers
// return CHIP_END_OF_TLV and src/app/ReadHandler.cpp:208-214 answers it
// InvalidAction.

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
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
