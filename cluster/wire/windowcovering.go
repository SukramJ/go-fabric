// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"

	"github.com/SukramJ/go-fabric/tlv"
)

// WindowCoveringClusterID is the Matter cluster identifier for WindowCovering.
//
// Deprecated: use ClusterID of cluster/spec/windowcovering, the
// generated id: one source of ids (review II, R2-P6-par-1262,
// decision 140). Removable in the next minor release.
const WindowCoveringClusterID uint32 = 0x0102

// WindowCovering attribute IDs.
const (
	WindowCoveringAttrType                             uint32 = 0x0000
	WindowCoveringAttrCurrentPositionLiftPercentage    uint32 = 0x0008
	WindowCoveringAttrOperationalStatus                uint32 = 0x000A
	WindowCoveringAttrTargetPositionLiftPercent100ths  uint32 = 0x000B
	WindowCoveringAttrCurrentPositionLiftPercent100ths uint32 = 0x000E
	WindowCoveringAttrEndProductType                   uint32 = 0x000D
	WindowCoveringAttrConfigStatus                     uint32 = 0x0007
	WindowCoveringAttrMode                             uint32 = 0x0017
	WindowCoveringAttrSafetyStatus                     uint32 = 0x001A
)

// WindowCovering command IDs per Matter §5.3.7.
const (
	WindowCoveringCmdUpOrOpen    uint32 = 0x00
	WindowCoveringCmdDownOrClose uint32 = 0x01
	WindowCoveringCmdStopMotion  uint32 = 0x02
	// WindowCoveringCmdGoToLiftValue names a command id.
	//
	// Deprecated: the command is not in the Matter model this module serves
	// (parity/schema.json has no command 0x04 on WindowCovering); nothing
	// outside this package uses it (review II, R2-P6-api-1261, decision 140).
	// Removable in the next minor release.
	WindowCoveringCmdGoToLiftValue      uint32 = 0x04
	WindowCoveringCmdGoToLiftPercentage uint32 = 0x05
	// WindowCoveringCmdGoToTiltValue names a command id.
	//
	// Deprecated: the command is not in the Matter model this module serves
	// (parity/schema.json has no command 0x07 on WindowCovering); nothing
	// outside this package uses it (review II, R2-P6-api-1261, decision 140).
	// Removable in the next minor release.
	WindowCoveringCmdGoToTiltValue      uint32 = 0x07
	WindowCoveringCmdGoToTiltPercentage uint32 = 0x08
)

// ErrWindowCoveringMalformed is returned for malformed WindowCovering
// command payloads.
//
// Deprecated: GoToLiftValue and GoToTiltValue (0x04, 0x07) are not in the
// Matter model this module serves (parity/schema.json carries
// WindowCovering commands 0, 1, 2, 5 and 8 only); nothing outside this
// package uses it (review II, R2-P6-api-1261, decision 140). Removable in
// the next minor release.
var ErrWindowCoveringMalformed = errors.New("wire: WindowCovering command malformed")

// GoToLiftValueRequest mirrors Matter §5.3.7.4.
//
// Deprecated: GoToLiftValue and GoToTiltValue (0x04, 0x07) are not in the
// Matter model this module serves (parity/schema.json carries
// WindowCovering commands 0, 1, 2, 5 and 8 only); nothing outside this
// package uses it (review II, R2-P6-api-1261, decision 140). Removable in
// the next minor release.
type GoToLiftValueRequest struct {
	LiftValue uint16 // raw position
}

// GoToTiltValueRequest mirrors Matter §5.3.7.4.
//
// Deprecated: GoToLiftValue and GoToTiltValue (0x04, 0x07) are not in the
// Matter model this module serves (parity/schema.json carries
// WindowCovering commands 0, 1, 2, 5 and 8 only); nothing outside this
// package uses it (review II, R2-P6-api-1261, decision 140). Removable in
// the next minor release.
type GoToTiltValueRequest struct {
	TiltValue uint16
}

// DecodeGoToLiftValue parses GoToLiftValue payloads.
//
// Deprecated: GoToLiftValue and GoToTiltValue (0x04, 0x07) are not in the
// Matter model this module serves (parity/schema.json carries
// WindowCovering commands 0, 1, 2, 5 and 8 only); nothing outside this
// package uses it (review II, R2-P6-api-1261, decision 140). Removable in
// the next minor release.
func DecodeGoToLiftValue(payload []byte) (GoToLiftValueRequest, error) {
	var req GoToLiftValueRequest
	if err := walkContext(payload, ErrWindowCoveringMalformed, []uint32{0}, func(tag uint32, el tlv.Element) (err error) {
		if tag == 0 {
			req.LiftValue, err = fieldUint16(el, ErrWindowCoveringMalformed)
		}
		return err
	}); err != nil {
		return req, err
	}
	return req, nil
}

// DecodeGoToTiltValue parses GoToTiltValue payloads.
//
// Deprecated: GoToLiftValue and GoToTiltValue (0x04, 0x07) are not in the
// Matter model this module serves (parity/schema.json carries
// WindowCovering commands 0, 1, 2, 5 and 8 only); nothing outside this
// package uses it (review II, R2-P6-api-1261, decision 140). Removable in
// the next minor release.
func DecodeGoToTiltValue(payload []byte) (GoToTiltValueRequest, error) {
	var req GoToTiltValueRequest
	if err := walkContext(payload, ErrWindowCoveringMalformed, []uint32{0}, func(tag uint32, el tlv.Element) (err error) {
		if tag == 0 {
			req.TiltValue, err = fieldUint16(el, ErrWindowCoveringMalformed)
		}
		return err
	}); err != nil {
		return req, err
	}
	return req, nil
}
