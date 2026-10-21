// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// OriginCase is one row of a server's origin audit ([CheckOrigins]): Act
// changes the server it is handed — a host's Set, a command, a
// controller's write — and every change it reports carries Want; Want
// OriginUnknown states that Act changes nothing (a refused write, a
// no-op).
type OriginCase struct {
	Name string
	Act  func(tb testing.TB, srv contract.ClusterServer)
	Want contract.Origin
}

// CheckOrigins is the origin audit of a self-held server: for each case
// it runs Act on the server srv returns (a fresh one per case) and fails
// tb unless the server reported at least one change and every change
// carries the case's origin — or, for OriginUnknown, reported none. srv
// must be a [contract.AttributeChangeNotifier].
func CheckOrigins(tb testing.TB, srv func(tb testing.TB) contract.ClusterServer, cases ...OriginCase) {
	tb.Helper()
	for _, c := range cases {
		s := srv(tb)
		n, ok := s.(contract.AttributeChangeNotifier)
		if !ok {
			tb.Fatalf("%T reports no attribute change", s)
		}
		var (
			mu   sync.Mutex
			seen []contract.Change
		)
		unsubscribe := n.OnAttributeChange(func(ch contract.Change) {
			mu.Lock()
			seen = append(seen, ch)
			mu.Unlock()
		})
		c.Act(tb, s)
		unsubscribe()
		mu.Lock()
		got := seen
		mu.Unlock()
		switch {
		case c.Want == contract.OriginUnknown && len(got) > 0:
			tb.Errorf("%s: changes %+v, want none", c.Name, got)
		case c.Want != contract.OriginUnknown && len(got) == 0:
			tb.Errorf("%s: no change reported, want one from %v", c.Name, c.Want)
		}
		for _, ch := range got {
			if c.Want != contract.OriginUnknown && ch.Origin != c.Want {
				tb.Errorf("%s: change %v from %v, want %v", c.Name, ch.Attributes, ch.Origin, c.Want)
			}
		}
	}
}
