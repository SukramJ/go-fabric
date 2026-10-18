// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// A port of matter.js packages/protocol/test/common/DiscoveryDataTest.ts at
// 862ad75f: the TXT keys DiscoveryData decodes and the values it drops.

import (
	"maps"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// dd is a decoded TXTData as the matter.js object literal: absent keys
// missing, intervals in milliseconds.
type dd map[string]any

func ddOf(d TXTData) dd {
	out := dd{}
	if d.VP != nil {
		out["VP"] = *d.VP
	}
	if d.DT != nil {
		out["DT"] = uint64(*d.DT)
	}
	if d.DN != nil {
		out["DN"] = *d.DN
	}
	if d.RI != nil {
		out["RI"] = *d.RI
	}
	if d.PH != nil {
		out["PH"] = uint64(*d.PH)
	}
	if d.PI != nil {
		out["PI"] = *d.PI
	}
	for k, v := range map[string]*time.Duration{"SII": d.SII, "SAI": d.SAI, "SAT": d.SAT} {
		if v != nil {
			out[k] = v.Milliseconds()
		}
	}
	if d.T != nil {
		out["T"] = *d.T
	}
	if d.ICD != nil {
		out["ICD"] = uint64(*d.ICD)
	}
	return out
}

func ddExpect(t *testing.T, entries map[string]string, want dd) {
	t.Helper()
	p := make(Parameters, len(entries))
	for k, v := range entries {
		p[k] = []byte(v)
	}
	if got := ddOf(ParseTXTData(p)); !maps.Equal(got, want) {
		t.Errorf("decode(%q) = %v, want %v", entries, got, want)
	}
}

// TestDiscoveryDataCaseInsensitiveKeys covers DiscoveryDataTest.ts:15-27.
// matter.js builds a DnssdParameters, which folds keys; the Go port checks
// both a hand-built Parameters and one a discovered Name builds, where the
// first occurrence of a key wins regardless of its case.
func TestDiscoveryDataCaseInsensitiveKeys(t *testing.T) {
	t.Parallel()
	t.Run("decodes the keys of DNS-SD parameters regardless of their case", func(t *testing.T) {
		t.Parallel()
		want := dd{"SII": int64(5000), "SAT": int64(4000), "DN": "Kitchen"}
		ddExpect(t, map[string]string{"sii": "5000", "Sat": "4000", "dn": "Kitchen"}, want)

		n, _, _ := testNames(t)
		deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, false, "sii=5000", "Sat=4000", "dn=Kitchen", "SII=1")})
		if got := ddOf(ParseTXTData(n.Get(instQ).Parameters())); !maps.Equal(got, want) {
			t.Errorf("from a discovered name: %v, want %v", got, want)
		}
	})
}

// TestDiscoveryDataSessionIntervals covers DiscoveryDataTest.ts:29-70.
func TestDiscoveryDataSessionIntervals(t *testing.T) {
	t.Parallel()
	t.Run("decodes valid values", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "5000", "SAI": "300", "SAT": "4000"},
			dd{"SII": int64(5000), "SAI": int64(300), "SAT": int64(4000)})
	})
	t.Run("accepts SII/SAI at the maximum of 3600000 ms and zero", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "3600000", "SAI": "0"}, dd{"SII": int64(3600000), "SAI": int64(0)})
	})
	t.Run("accepts SAT at the maximum of 65535 ms", func(t *testing.T) {
		ddExpect(t, map[string]string{"SAT": "65535"}, dd{"SAT": int64(65535)})
	})
	t.Run("drops SII/SAI above 3600000 ms", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "3600001", "SAI": "4000000"}, dd{})
	})
	t.Run("drops SAT above 65535 ms and SAT of zero", func(t *testing.T) {
		ddExpect(t, map[string]string{"SAT": "65536"}, dd{})
		ddExpect(t, map[string]string{"SAT": "0"}, dd{})
	})
	t.Run("drops empty values", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "", "SAI": "", "SAT": ""}, dd{})
	})
	t.Run("drops non-numeric and non-integer values", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "abc", "SAI": "12.5", "SAT": "-5"}, dd{})
		ddExpect(t, map[string]string{"SII": "1e3", "SAI": " 5", "SAT": "0x10"}, dd{})
	})
	t.Run("drops values with leading zeros", func(t *testing.T) {
		ddExpect(t, map[string]string{"SII": "0500", "SAI": "007", "SAT": "065535"}, dd{})
	})
}

// TestDiscoveryDataIdentity covers DiscoveryDataTest.ts:72-110.
func TestDiscoveryDataIdentity(t *testing.T) {
	t.Parallel()
	t.Run("decodes valid values", func(t *testing.T) {
		ddExpect(t, map[string]string{"VP": "65521+32768", "DT": "4293984257", "PH": "33"},
			dd{"VP": "65521+32768", "DT": uint64(0xfff10001), "PH": uint64(33)})
		ddExpect(t, map[string]string{"VP": "65521"}, dd{"VP": "65521"})
	})
	t.Run("accepts values at their maximum", func(t *testing.T) {
		ddExpect(t, map[string]string{"VP": "65535+65535", "DT": "4294967295", "PH": "4294967295"},
			dd{"VP": "65535+65535", "DT": uint64(4294967295), "PH": uint64(4294967295)})
	})
	t.Run("keeps the VP vendor when only the product is invalid", func(t *testing.T) {
		for _, vp := range []string{"1+65536", "1+", "1+02"} {
			ddExpect(t, map[string]string{"VP": vp}, dd{"VP": "1"})
		}
	})
	t.Run("drops VP with an invalid vendor", func(t *testing.T) {
		for _, vp := range []string{"65536+1", "+1", "065521+1", " 1", "abc"} {
			ddExpect(t, map[string]string{"VP": vp}, dd{})
		}
	})
	t.Run("drops DT and PH outside 32 bits, non-integers, leading zeros and PH of zero", func(t *testing.T) {
		ddExpect(t, map[string]string{"DT": "4294967296", "PH": "4294967296"}, dd{})
		ddExpect(t, map[string]string{"DT": "-1", "PH": "1.5"}, dd{})
		ddExpect(t, map[string]string{"DT": "0256", "PH": "033"}, dd{})
		ddExpect(t, map[string]string{"PH": "0"}, dd{})
	})
}

// TestDiscoveryDataTransports covers DiscoveryDataTest.ts:112-127.
func TestDiscoveryDataTransports(t *testing.T) {
	t.Parallel()
	t.Run("decodes valid values", func(t *testing.T) {
		ddExpect(t, map[string]string{"T": "4", "ICD": "1"},
			dd{"T": SupportedTransports{TCPClient: false, TCPServer: true}, "ICD": uint64(1)})
		ddExpect(t, map[string]string{"T": "0", "ICD": "0"},
			dd{"T": SupportedTransports{}, "ICD": uint64(0)})
	})
	t.Run("drops signed, hexadecimal, non-integer and leading-zero values", func(t *testing.T) {
		for _, v := range []string{"-1", "+4", "0x4", "4.0", "1e1", " 4", "04", ""} {
			ddExpect(t, map[string]string{"T": v, "ICD": v}, dd{})
		}
	})
	t.Run("drops T outside 16 bits and ICD other than 0 or 1", func(t *testing.T) {
		ddExpect(t, map[string]string{"T": "65536", "ICD": "2"}, dd{})
	})
}
