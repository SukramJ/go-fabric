// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Bounds of the numeric TXT keys. Mirrors matter.js 0ceb202d
// packages/protocol/src/common/Scanner.ts:84-140 (UINT16_MAX, UINT32_MAX,
// the 3 600 000 ms interval maximum).
const (
	maxUint16            = 0xFFFF
	maxUint32            = 0xFFFF_FFFF
	maxSessionIntervalMs = 3_600_000
)

// SupportedTransports is the `T` TXT key decoded: bit 1 TCP client, bit 2
// TCP server, bit 0 reserved. Mirrors matter.js 0ceb202d
// packages/protocol/src/common/SupportedTransportsBitmap.ts:9-23.
type SupportedTransports struct {
	TCPClient bool
	TCPServer bool
}

func decodeSupportedTransports(v uint64) SupportedTransports {
	return SupportedTransports{TCPClient: v&(1<<1) != 0, TCPServer: v&(1<<2) != 0}
}

// TXTData is what a node announces in its TXT record, matter.js
// DiscoveryData (packages/protocol/src/common/Scanner.ts:29-69). A nil
// field is absent: not announced, or announced with a value matter.js
// rejects, so that the protocol default applies. The commissionable `D`
// and `CM` keys are the [Scanner]'s (see [Device]); the same type serves
// operational TXT records ([Service.TXTData]), as matter.js's Peer
// parses them with DiscoveryData too.
type TXTData struct {
	// VP is "<vendor>" or "<vendor>+<product>", each re-formatted from its
	// parsed value.
	VP *string
	// DT is the device type.
	DT *uint32
	// DN is the device name.
	DN *string
	// RI is the rotating device identifier.
	RI *string
	// PH is the pairing hint; 0 is illegal and absent.
	PH *uint32
	// PI is the pairing instruction.
	PI *string
	// SII is the session idle interval.
	SII *time.Duration
	// SAI is the session active interval.
	SAI *time.Duration
	// SAT is the session active threshold.
	SAT *time.Duration
	// T is the supported transports.
	T *SupportedTransports
	// ICD is the ICD long-idle-time operating mode, 0 or 1.
	ICD *uint8
}

// ParseTxtDecimal parses a numeric TXT value: a decimal integer without
// leading zeros (/^(0|[1-9]\d*)$/) no greater than limit; anything else is
// absent. Mirrors matter.js 0ceb202d
// packages/protocol/src/common/Scanner.ts:153-159 (parseTxtDecimal).
func ParseTxtDecimal(value string, limit uint64) (uint64, bool) {
	if value == "" || (value[0] == '0' && len(value) > 1) {
		return 0, false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil || v > limit {
		return 0, false
	}
	return v, true
}

func parseTxtDecimalPtr(p Parameters, key string, limit uint64) (uint64, bool) {
	v, ok := p.Get(key)
	if !ok {
		return 0, false
	}
	return ParseTxtDecimal(v, limit)
}

// ParseTXTData reads the TXT keys matter.js knows: DN, RI and PI
// verbatim; VP split at "+", the vendor ≤ 65535 or VP dropped, the product
// parsed on its own and dropped alone; DT ≤ 2³²−1; PH ≤ 2³²−1 and not 0;
// ICD 0 or 1; T ≤ 65535 as the transports bitmap; SII and SAI ≤ 3 600 000
// ms; SAT 1..65535 ms. A value matter.js rejects is absent. Mirrors
// matter.js 0ceb202d packages/protocol/src/common/Scanner.ts:71-145.
func ParseTXTData(p Parameters) TXTData {
	var dd TXTData
	str := func(key string) *string {
		if v, ok := p.Get(key); ok {
			return &v
		}
		return nil
	}
	dd.DN, dd.RI, dd.PI = str("DN"), str("RI"), str("PI")

	if v, ok := p.Get("VP"); ok {
		parts := strings.Split(v, "+")
		if vendor, ok := ParseTxtDecimal(parts[0], maxUint16); ok {
			vp := strconv.FormatUint(vendor, 10)
			if len(parts) > 1 {
				if product, ok := ParseTxtDecimal(parts[1], maxUint16); ok {
					vp += "+" + strconv.FormatUint(product, 10)
				}
			}
			dd.VP = &vp
		}
	}
	if v, ok := parseTxtDecimalPtr(p, "DT", maxUint32); ok {
		dt := narrow32(v)
		dd.DT = &dt
	}
	if v, ok := parseTxtDecimalPtr(p, "PH", maxUint32); ok && v != 0 {
		ph := narrow32(v)
		dd.PH = &ph
	}
	if v, ok := parseTxtDecimalPtr(p, "ICD", 1); ok {
		icd := narrow8(v)
		dd.ICD = &icd
	}
	if v, ok := parseTxtDecimalPtr(p, "T", maxUint16); ok {
		t := decodeSupportedTransports(v)
		dd.T = &t
	}
	interval := func(key string, limit uint64, zeroAbsent bool) *time.Duration {
		v, ok := parseTxtDecimalPtr(p, key, limit)
		if !ok || (zeroAbsent && v == 0) {
			return nil
		}
		d := time.Duration(narrow32(v)) * time.Millisecond
		return &d
	}
	dd.SII = interval("SII", maxSessionIntervalMs, false)
	dd.SAI = interval("SAI", maxSessionIntervalMs, false)
	dd.SAT = interval("SAT", maxUint16, true)
	return dd
}

// TXTData parses the service's TXT parameters ([ParseTXTData]).
func (s *Service) TXTData() TXTData {
	return ParseTXTData(s.Parameters())
}

// narrow32, narrow16 and narrow8 narrow a value [ParseTxtDecimal] already
// held to the limit; the guard is restated on a constant so a static
// analyser sees the bound too. Saturation is unreachable.
func narrow32(v uint64) uint32 {
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

func narrow16(v uint64) uint16 {
	if v > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(v)
}

func narrow8(v uint64) uint8 {
	if v > math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(v)
}
