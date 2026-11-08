// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package brdiscovery_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/thread/brdiscovery"
)

// conn is a DNS-SD socket a test speaks for: it records the queries and
// delivers the responses the test injects.
type conn struct {
	mu   sync.Mutex
	sent [][]byte
	out  chan mdns.QueryResponse
}

func (c *conn) Send(msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, append([]byte(nil), msg...))
	return nil
}

func (c *conn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c *conn) Close() error                         { return nil }

func (c *conn) asked(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.sent {
		var m dns.Msg
		if m.Unpack(b) != nil {
			continue
		}
		for _, q := range m.Question {
			if strings.EqualFold(strings.TrimSuffix(q.Name, "."), name) && q.Qtype == dns.TypePTR {
				return true
			}
		}
	}
	return false
}

// escaped is b in the TXT presentation form miekg/dns packs.
func escaped(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		fmt.Fprintf(&sb, "\\%03d", c)
	}
	return sb.String()
}

// TestRegistryOnTheCache runs the registry on a real DNS-SD cache, in a
// synctest bubble: it asks for both service types once
// (BorderRouterRegistry.ts:128-131 of matter.js at db021da0) and lists a
// border router from its _meshcop answer, binary xa and xp included.
func TestRegistryOnTheCache(t *testing.T) {
	t.Parallel()
	synctest.Test(t, registryOnTheCache)
}

func registryOnTheCache(t *testing.T) {
	c := &conn{out: make(chan mdns.QueryResponse, 8)}
	names := discovery.NewNames(c, nil)
	t.Cleanup(func() { _ = names.Close() })
	r := brdiscovery.New(names, brdiscovery.Config{})
	t.Cleanup(r.Close)
	synctest.Wait()
	if !c.asked("_meshcop._udp.local") || !c.asked("_trel._udp.local") {
		t.Fatal("the registry did not ask for both service types")
	}

	added := make(chan brdiscovery.Event, 4)
	r.OnChange(func(e brdiscovery.Event) { added <- e })
	inst := "Kitchen._meshcop._udp.local."
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.SRV{Hdr: dns.RR_Header{Name: inst, Rrtype: dns.TypeSRV, Class: dns.ClassINET | 0x8000, Ttl: 120}, Target: "kitchen.local.", Port: 49154},
		&dns.TXT{Hdr: dns.RR_Header{Name: inst, Rrtype: dns.TypeTXT, Class: dns.ClassINET | 0x8000, Ttl: 4500}, Txt: []string{
			"xa=" + escaped([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11}),
			"xp=" + escaped([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}),
			"nn=Home", "vn=ACME", "mn=BR-100",
		}},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	c.out <- mdns.QueryResponse{Msg: b}
	synctest.Wait()
	select {
	case e := <-added:
		want := brdiscovery.Entry{ExtAddress: "AABBCCDDEEFF0011", ExtPanID: "1122334455667788", NetworkName: "Home", VendorName: "ACME", ModelName: "BR-100", HostName: "kitchen.local", LastSeen: e.Entry.LastSeen}
		if e.Kind != brdiscovery.EventAdded || e.Entry != want || e.Entry.LastSeen.IsZero() {
			t.Fatalf("event %+v, want added %+v", e, want)
		}
	default:
		t.Fatal("no border router added")
	}
	if got, ok := r.Get("aabbccddeeff0011"); !ok || got.NetworkName != "Home" {
		t.Fatalf("Get %+v (%v)", got, ok)
	}
}
