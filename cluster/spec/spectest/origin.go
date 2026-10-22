// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// OriginCase is one row of a server's origin audit ([CheckOrigins]): Act
// changes the server it is handed — a host's Set, a command, a
// controller's write — and every change it reports carries Want; Want
// OriginUnknown states that Act changes nothing (a refused write, a
// no-op). Attributes are the attributes the reported changes must name;
// a case whose Want is [contract.OriginControllerWrite] names the
// attribute it writes — the written attribute reaches the listeners from
// the write, as matter.js emits its `$Changed` on the write's commit
// (Datasource.ts:1473-1476) — and [Write] builds such a case.
type OriginCase struct {
	Name       string
	Act        func(tb testing.TB, srv contract.ClusterServer)
	Want       contract.Origin
	Attributes []uint32
}

// Write is the controller-write row of an origin audit: Act writes value
// to attrID as the dispatcher does (the context carries
// [contract.OriginControllerWrite]) and fails tb when the write is
// refused; the listeners must see attrID from the write. value is in the
// form the dispatcher hands MatterWrite (a decoded TLV value).
func Write(attrID uint32, value any) OriginCase {
	return OriginCase{
		Name: "controller write", Want: contract.OriginControllerWrite, Attributes: []uint32{attrID},
		Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			ctx := contract.WithOrigin(context.Background(), contract.OriginControllerWrite)
			if err := srv.MatterWrite(ctx, attrID, value); err != nil {
				tb.Fatalf("write of 0x%04X = %v: %v", attrID, value, err)
			}
		},
	}
}

// CheckOrigins is the origin audit of a self-held server: for each case
// it runs Act on the server srv returns (a fresh one per case) and fails
// tb unless the server reported at least one change, every change
// carries the case's origin and the changes name each of the case's
// Attributes — or, for OriginUnknown, reported none. A controller-write
// case that names no attribute fails: the audit asserts what the host's
// listener sees, not only that the port was reached. srv must be a
// [contract.AttributeChangeNotifier]; the audit listens where the host's
// OnChanged does (OnAttributeChange).
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
		if c.Want == contract.OriginControllerWrite && len(c.Attributes) == 0 {
			tb.Errorf("%s: a controller-write case names the attribute it writes (OriginCase.Attributes)", c.Name)
		}
		for _, attr := range c.Attributes {
			if !slices.ContainsFunc(got, func(ch contract.Change) bool { return slices.Contains(ch.Attributes, attr) }) {
				tb.Errorf("%s: no change names attribute 0x%04X; changes %+v", c.Name, attr, got)
			}
		}
	}
}
