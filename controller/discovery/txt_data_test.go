// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestService_TXTDataFromTheOperationalRecord(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	const op = "0123456789ABCDEF-0000000000000001._matter._tcp.local"
	deliver(t, n, 0, []dns.RR{txtRR(op, 120, true, "SII=500", "SAI=300", "SAT=4000", "T=4", "ICD=0")})
	s := NewService(n, op)
	defer s.Close()
	dd := s.TXTData()
	if dd.SII == nil || *dd.SII != 500*time.Millisecond || *dd.SAI != 300*time.Millisecond || *dd.SAT != 4*time.Second {
		t.Fatalf("intervals = %v %v %v", dd.SII, dd.SAI, dd.SAT)
	}
	if dd.T == nil || dd.T.TCPClient || !dd.T.TCPServer || dd.ICD == nil || *dd.ICD != 0 {
		t.Fatalf("T/ICD = %+v %v", dd.T, dd.ICD)
	}
	if dd.DN != nil || dd.VP != nil || dd.PH != nil {
		t.Fatal("absent keys must be nil")
	}
}
