// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest_test

import (
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/controller/ble"
	"thingfabric.dev/go-fabric/controller/ble/bletest"
	"thingfabric.dev/go-fabric/transport/btp"
)

// TestSubscribeBeforeTheHandshakeRequestStartsNothing: a C2 subscribe
// that reaches the peripheral before any handshake request starts no
// session, and the request written after it is not answered either
// (review II, R2-C2-test-2050).
func TestSubscribeBeforeTheHandshakeRequestStartsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		served := 0
		p := &bletest.Peripheral{Address: "ab", Serve: func(*btp.Conn) (func([]byte), func()) {
			served++
			return func([]byte) {}, nil
		}}
		defer p.DropConnections()
		conn, err := bletest.NewCentral(p).Connect(t.Context(), ble.Peripheral{Address: "ab"})
		if err != nil {
			t.Fatal(err)
		}
		c2 := make(chan []byte, 8)
		if err := conn.SubscribeC2(t.Context(), func(b []byte) { c2 <- b }); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		req := btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: btp.SupportedVersions, ATTMTU: btp.MaximumBTPMTU, ClientWindowSize: 4})
		if err := conn.WriteC1(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if len(c2) != 0 || served != 0 {
			t.Fatalf("subscribe before the request: %d indications, Serve called %d times; want none", len(c2), served)
		}
	})
}
