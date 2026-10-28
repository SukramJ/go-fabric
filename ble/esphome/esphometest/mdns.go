// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphometest

import (
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// serviceType is the API's DNS-SD service type
// (esphome/components/mdns/mdns_component.cpp:140, :146-150).
const serviceType = "_esphomelib._tcp.local."

// Network is an in-memory DNS-SD network on which fake proxies advertise
// their API as an ESPHome device does: an `_esphomelib._tcp` instance named
// after the device on its API port, the host `<name>.local` at 127.0.0.1,
// and the TXT keys of the firmware's mdns component — version, mac,
// config_hash, platform, board, network, and api_encryption with a key or
// api_encryption_supported without (mdns_component.cpp:133-242).
// [Network.Conn] is an mdns.QueryConn on it: a client/discovery cache
// built on one (discovery.NewNames) browses the advertised proxies.
type Network struct {
	mu    sync.Mutex
	ads   []*advert
	conns []*queryConn
}

// advert is one advertised proxy.
type advert struct {
	proxy *Proxy
	name  string
	txt   []string
}

// NewNetwork returns an empty network.
func NewNetwork() *Network { return &Network{} }

// Advertise advertises p under its Options.Name; extra TXT entries
// ("config_hash=…") come before, and so win over, the defaults. A proxy
// advertised again replaces its earlier entry.
func (n *Network) Advertise(p *Proxy, extra ...string) {
	txt := append(slices.Clone(extra),
		"version=2026.10.0",
		"mac="+strings.ToLower(strings.ReplaceAll(p.opts.MAC, ":", "")),
		"config_hash=0a1b2c3d",
		"platform=ESP32",
		"board=esp32dev",
		"network=wifi",
	)
	if p.psk != nil {
		txt = append(txt, "api_encryption=Noise_NNpsk0_25519_ChaChaPoly_SHA256")
	} else {
		txt = append(txt, "api_encryption_supported=Noise_NNpsk0_25519_ChaChaPoly_SHA256")
	}
	n.mu.Lock()
	n.ads = slices.DeleteFunc(n.ads, func(a *advert) bool { return a.proxy == p })
	n.ads = append(n.ads, &advert{proxy: p, name: p.opts.Name, txt: txt})
	n.mu.Unlock()
}

// Withdraw stops advertising p and multicasts its goodbye, as a device
// that leaves the network gracefully does (RFC 6762 §10.1).
func (n *Network) Withdraw(p *Proxy) {
	n.mu.Lock()
	var gone []*advert
	n.ads = slices.DeleteFunc(n.ads, func(a *advert) bool {
		if a.proxy == p {
			gone = append(gone, a)
			return true
		}
		return false
	})
	conns := slices.Clone(n.conns)
	n.mu.Unlock()
	for _, a := range gone {
		m := new(dns.Msg)
		m.Response, m.Authoritative = true, true
		m.Answer = []dns.RR{a.ptr(0)}
		for _, c := range conns {
			c.deliver(m)
		}
	}
}

// Conn returns a new querier on the network.
func (n *Network) Conn() mdns.QueryConn {
	c := &queryConn{n: n, out: make(chan mdns.QueryResponse, 256)}
	n.mu.Lock()
	n.conns = append(n.conns, c)
	n.mu.Unlock()
	return c
}

func (a *advert) instance() string { return a.name + "." + serviceType }
func (a *advert) host() string     { return a.name + ".local." }

func hdr(name string, t uint16, ttl uint32, flush bool) dns.RR_Header {
	class := uint16(dns.ClassINET)
	if flush {
		class |= 0x8000
	}
	return dns.RR_Header{Name: name, Rrtype: t, Class: class, Ttl: ttl}
}

func (a *advert) ptr(ttl uint32) dns.RR {
	return &dns.PTR{Hdr: hdr(serviceType, dns.TypePTR, ttl, false), Ptr: a.instance()}
}

func (a *advert) srv() dns.RR {
	return &dns.SRV{Hdr: hdr(a.instance(), dns.TypeSRV, 120, true), Target: a.host(), Port: a.proxy.Port()}
}

func (a *advert) txtRR() dns.RR {
	return &dns.TXT{Hdr: hdr(a.instance(), dns.TypeTXT, 4500, true), Txt: a.txt}
}

func (a *advert) addr() dns.RR {
	return &dns.A{Hdr: hdr(a.host(), dns.TypeA, 120, true), A: net.IPv4(127, 0, 0, 1).To4()}
}

// answer is the response to one question, nil when the network has none.
func (n *Network) answer(q dns.Question) *dns.Msg {
	n.mu.Lock()
	ads := slices.Clone(n.ads)
	n.mu.Unlock()
	m := new(dns.Msg)
	m.Response, m.Authoritative = true, true
	name := strings.ToLower(q.Name)
	for _, a := range ads {
		switch {
		case name == serviceType && (q.Qtype == dns.TypePTR || q.Qtype == dns.TypeANY):
			m.Answer = append(m.Answer, a.ptr(4500))
			m.Extra = append(m.Extra, a.srv(), a.txtRR(), a.addr())
		case name == strings.ToLower(a.instance()):
			if q.Qtype == dns.TypeSRV || q.Qtype == dns.TypeANY {
				m.Answer = append(m.Answer, a.srv())
				m.Extra = append(m.Extra, a.addr())
			}
			if q.Qtype == dns.TypeTXT || q.Qtype == dns.TypeANY {
				m.Answer = append(m.Answer, a.txtRR())
			}
		case name == strings.ToLower(a.host()) && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeANY):
			m.Answer = append(m.Answer, a.addr())
		default:
		}
	}
	if len(m.Answer) == 0 {
		return nil
	}
	return m
}

// queryConn is one querier on a Network: every query it sends is answered
// on its own Responses.
type queryConn struct {
	n   *Network
	mu  sync.Mutex
	out chan mdns.QueryResponse
	off bool
}

func (c *queryConn) Send(msg []byte) error {
	var q dns.Msg
	if err := q.Unpack(msg); err != nil {
		return err
	}
	for _, question := range q.Question {
		if m := c.n.answer(question); m != nil {
			c.deliver(m)
		}
	}
	return nil
}

func (c *queryConn) deliver(m *dns.Msg) {
	b, err := m.Pack()
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.off {
		return
	}
	select {
	case c.out <- mdns.QueryResponse{Msg: b}:
	default:
	}
}

func (c *queryConn) Responses() <-chan mdns.QueryResponse { return c.out }

func (c *queryConn) Close() error {
	c.mu.Lock()
	c.off = true
	c.mu.Unlock()
	c.n.mu.Lock()
	c.n.conns = slices.DeleteFunc(c.n.conns, func(x *queryConn) bool { return x == c })
	c.n.mu.Unlock()
	return nil
}
