// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"bytes"
	"strconv"
	"testing"
	"time"
)

// FuzzParseTXTData feeds ParseTXTData the TXT parameters of a foreign
// announcement. The input is split into entries at '\n', and each entry
// into key and value at its first '=', the way Name.parameters splits a
// TXT record. Invariant: it never panics, and every parsed field keeps
// the bound the function documents: SII and SAI at most one hour, SAT
// from 1 to 65535 ms, ICD 0 or 1, PH never 0.
func FuzzParseTXTData(f *testing.F) {
	f.Add([]byte("VP=65521+32769\nDT=257\nDN=Kitchen\nSII=500\nSAI=300\nSAT=4000\nT=6\nICD=0\nPH=33\nPI=10"))
	f.Add([]byte("VP=0123\nSII=3600001\nSAT=0\nICD=2\nPH=0\nT=65536"))
	f.Add([]byte("vp=1+\nsii=\n=x\nSAT=65535"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<16 {
			return
		}
		p := make(Parameters)
		for entry := range bytes.SplitSeq(raw, []byte{'\n'}) {
			key, value, _ := bytes.Cut(entry, []byte{'='})
			if len(key) == 0 {
				continue
			}
			if _, dup := p[string(key)]; !dup {
				p[string(key)] = value
			}
		}
		dd := ParseTXTData(p)
		for name, d := range map[string]*time.Duration{"SII": dd.SII, "SAI": dd.SAI} {
			if d != nil && (*d < 0 || *d > time.Hour) {
				t.Fatalf("%s = %v, outside 0..1h", name, *d)
			}
		}
		if dd.SAT != nil && (*dd.SAT < time.Millisecond || *dd.SAT > 65535*time.Millisecond) {
			t.Fatalf("SAT = %v, outside 1..65535 ms", *dd.SAT)
		}
		if dd.ICD != nil && *dd.ICD > 1 {
			t.Fatalf("ICD = %d, want 0 or 1", *dd.ICD)
		}
		if dd.PH != nil && *dd.PH == 0 {
			t.Fatal("PH = 0, which is absent")
		}
	})
}

// FuzzParseTxtDecimal: whatever ParseTxtDecimal accepts is no greater
// than the limit and is the canonical decimal spelling of the value. A
// leading zero, a sign or a space is never accepted.
func FuzzParseTxtDecimal(f *testing.F) {
	f.Add("0", uint64(1))
	f.Add("65535", uint64(maxUint16))
	f.Add("65536", uint64(maxUint16))
	f.Add("007", uint64(10))
	f.Add("-1", uint64(10))
	f.Add("18446744073709551616", uint64(maxUint32))
	f.Fuzz(func(t *testing.T, value string, limit uint64) {
		v, ok := ParseTxtDecimal(value, limit)
		if !ok {
			return
		}
		if v > limit {
			t.Fatalf("ParseTxtDecimal(%q, %d) = %d above the limit", value, limit, v)
		}
		if s := strconv.FormatUint(v, 10); s != value {
			t.Fatalf("ParseTxtDecimal(%q) = %d, which spells %q", value, v, s)
		}
	})
}

// FuzzDecodeSupportedTransports: the T bitmap sets TCPClient from bit 1
// and TCPServer from bit 2 alone. No other bit changes the result.
func FuzzDecodeSupportedTransports(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(6))
	f.Add(uint64(1))
	f.Add(^uint64(0))
	f.Fuzz(func(t *testing.T, v uint64) {
		got := decodeSupportedTransports(v)
		want := SupportedTransports{TCPClient: v&2 != 0, TCPServer: v&4 != 0}
		if got != want {
			t.Fatalf("decodeSupportedTransports(%#x) = %+v, want %+v", v, got, want)
		}
		if decodeSupportedTransports(v&6) != got {
			t.Fatalf("decodeSupportedTransports(%#x) depends on a bit other than 1 and 2", v)
		}
	})
}
