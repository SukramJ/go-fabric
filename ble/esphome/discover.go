// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// ServiceType is the DNS-SD service type of an ESPHome device's native
// API: the firmware's mdns component registers `_esphomelib._tcp` on the
// API port (esphome/components/mdns/mdns_component.cpp:140, :146-150), and
// aioesphomeapi browses `_esphomelib._tcp.local.` (host_resolver.py:29,
// discover.py:92-94).
const ServiceType = "_esphomelib._tcp.local"

// The discovery defaults.
const (
	// DefaultRescan is how often a central with [DiscoverOptions] browses
	// for proxies again.
	DefaultRescan = 5 * time.Minute
	// DefaultBrowseTimeout is how long one browse collects answers.
	DefaultBrowseTimeout = 3 * time.Second
)

// The TXT keys discovery reads (mdns_component.cpp:133-144, :238-242). The
// record does not say whether a device is a Bluetooth proxy: that is the
// feature flags of DeviceInfoResponse / DeviceCapabilitiesResponse, read
// by connecting.
const (
	txtMAC           = "mac"
	txtConfigHash    = "config_hash"
	txtAPIEncryption = "api_encryption"
)

// ErrNoBrowser is DiscoverOptions or Discover without a Browser.
var ErrNoBrowser = errors.New("esphome: discovery needs a DNS-SD browser")

// Browser browses one DNS-SD service type until ctx ends and returns the
// instances resolved by then; *discovery.Names is one
// ([discovery.Names.Browse]).
type Browser interface {
	Browse(ctx context.Context, serviceType string) ([]discovery.Instance, error)
}

var _ Browser = (*discovery.Names)(nil)

// Keys are the API encryption keys of discovered devices, keyed by the
// device's name — its DNS-SD instance name, the ESPHome node name
// ("kitchen-proxy") — or by its MAC address in any common spelling
// ("AA:BB:CC:DD:EE:FF", "aabbccddeeff"), which the TXT record's `mac`
// carries. A name matches before a MAC; both match without regard to
// case. A device without a key is used only when it answers plaintext.
type Keys map[string]string

// ParseKey parses name=key — the reference controller's
// --bluetooth-proxy-key — and checks that key is base64 of 32 bytes.
func ParseKey(spec string) (name, key string, err error) {
	name, key, ok := strings.Cut(spec, "=")
	name, key = strings.TrimSpace(name), strings.TrimSpace(key)
	if !ok || name == "" || key == "" {
		return "", "", fmt.Errorf("esphome: %q is not name=key", spec)
	}
	if _, err := wire.DecodeKey(key); err != nil {
		return "", "", fmt.Errorf("esphome: key of %s: %w", name, err)
	}
	return name, key, nil
}

// lookup is the key of the device named name with the TXT mac.
func (k Keys) lookup(name, mac string) string {
	for n, v := range k {
		if strings.EqualFold(n, name) {
			return v
		}
	}
	if m := normalMAC(mac); m != "" {
		for n, v := range k {
			if normalMAC(n) == m {
				return v
			}
		}
	}
	return ""
}

// normalMAC is s as twelve lower-case hex digits, "" when s is no MAC.
func normalMAC(s string) string {
	s = strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "").Replace(strings.TrimSpace(s)))
	if len(s) != 12 {
		return ""
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return s
}

// DiscoverOptions turn on discovery in [NewCentral].
type DiscoverOptions struct {
	// Browser browses DNS-SD; a client/discovery cache
	// (discovery.Open) is one.
	Browser Browser
	// Keys are the discovered devices' API keys.
	Keys Keys
	// Rescan is how often the central browses again; 0 is DefaultRescan.
	// It browses again on every proxy loss as well.
	Rescan time.Duration
	// Timeout is how long one browse collects answers; 0 is
	// DefaultBrowseTimeout.
	Timeout time.Duration
}

func (d DiscoverOptions) normalized() (DiscoverOptions, error) {
	if d.Browser == nil {
		return d, ErrNoBrowser
	}
	if d.Rescan <= 0 {
		d.Rescan = DefaultRescan
	}
	if d.Timeout <= 0 {
		d.Timeout = DefaultBrowseTimeout
	}
	return d, nil
}

// candidate is one discovered device worth connecting to.
type candidate struct {
	key   string
	name  string
	hash  string
	addrs []string
	psk   string
}

// candidates browses for timeout and sorts the instances into devices to
// connect to and devices skipped for a reason the TXT record already
// gives: a device that requires encryption without a key in keys.
func candidates(ctx context.Context, b Browser, keys Keys, timeout time.Duration, clk clock.Clock, log *slog.Logger) ([]candidate, error) {
	bctx, cancel := clock.WithTimeout(ctx, clk, timeout)
	instances, err := b.Browse(bctx, ServiceType)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("esphome: browse %s: %w", ServiceType, err)
	}
	out := make([]candidate, 0, len(instances))
	for _, in := range instances {
		mac, _ := in.Parameters.Get(txtMAC)
		hash, _ := in.Parameters.Get(txtConfigHash)
		c := candidate{key: strings.ToLower(in.Name), name: in.Name, hash: hash, psk: keys.lookup(in.Name, mac)}
		for _, a := range in.Addresses {
			c.addrs = append(c.addrs, a.String())
		}
		if _, encrypted := in.Parameters.Get(txtAPIEncryption); encrypted && c.psk == "" {
			skip(log, c, ErrRequiresEncryption)
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// skip logs a discovered device that is not used.
func skip(log *slog.Logger, c candidate, reason error) {
	log.Info("ble.esphome.proxy_skipped", slog.String("proxy", c.name), slog.Any("addresses", c.addrs), slog.String("reason", reason.Error()))
}

// Discover browses DNS-SD for timeout for ESPHome devices
// ([ServiceType]), connects to each once and returns the Bluetooth proxies
// a central can commission through: those with ACTIVE_CONNECTIONS and
// REMOTE_CACHING. A device's key comes from keys (by name or by the TXT
// mac); a device without one is used only when it answers plaintext.
// Every device left out is logged on slog.Default as
// ble.esphome.proxy_skipped with the reason. NewCentral with
// Options.Discover runs the same discovery and keeps it current.
func Discover(ctx context.Context, browser Browser, keys Keys, timeout time.Duration) ([]Proxy, error) {
	if browser == nil {
		return nil, ErrNoBrowser
	}
	if timeout <= 0 {
		timeout = DefaultBrowseTimeout
	}
	var o Options
	o.Logger = slog.Default()
	o.normalize()
	cs, err := candidates(ctx, browser, keys, timeout, o.Clock, o.Logger)
	if err != nil {
		return nil, err
	}
	found := make([]*Proxy, len(cs))
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Go(func() {
			var psk []byte
			if c.psk != "" {
				k, err := wire.DecodeKey(c.psk)
				if err != nil {
					skip(o.Logger, c, err)
					return
				}
				psk = k
			}
			dctx, cancel := clock.WithTimeout(ctx, o.Clock, o.ConnectTimeout)
			defer cancel()
			a, addr, err := dialFirst(dctx, c.addrs, psk, &o)
			if err != nil {
				skip(o.Logger, c, err)
				return
			}
			a.close(context.Canceled)
			found[i] = &Proxy{Addr: addr, Key: c.psk, Name: c.name}
		})
	}
	wg.Wait()
	out := make([]Proxy, 0, len(found))
	for _, p := range found {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, ctx.Err()
}

// triggerRescan asks the discovery loop to browse again; a no-op without
// discovery.
func (c *Central) triggerRescan() {
	if c.rescans == nil {
		return
	}
	select {
	case c.rescans <- struct{}{}:
	default:
	}
}

// discoverLoop browses again every Rescan and on every proxy loss.
func (c *Central) discoverLoop() {
	defer c.wg.Done()
	tk := c.clk.NewTicker(c.disc.Rescan)
	defer tk.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-tk.C():
		case <-c.rescans:
		}
		c.rescan(c.ctx)
	}
}

// rescan browses once and brings the discovered set up to date: a new
// proxy is connected and joins, a known one follows its address, one no
// longer advertised and idle leaves. A proxy given to NewCentral stays,
// and a device advertised at its address is that proxy, not a second one.
func (c *Central) rescan(ctx context.Context) {
	cs, err := candidates(ctx, c.disc.Browser, c.disc.Keys, c.disc.Timeout, c.clk, c.log)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Warn("ble.esphome.discover_failed", slog.Any("err", err))
		}
		return
	}
	advertised := map[string]bool{}
	var fresh []candidate
	for _, cd := range cs {
		advertised[cd.key] = true
		if p := c.discoveredProxy(cd.key); p != nil {
			if p.setAddrs(cd.addrs) {
				c.log.Info("ble.esphome.proxy_moved", slog.String("proxy", p.name), slog.Any("addresses", cd.addrs))
			}
			continue
		}
		if c.staticAt(cd.addrs) {
			continue
		}
		c.setMu.Lock()
		hash, refused := c.skipped[cd.key]
		c.setMu.Unlock()
		if refused && hash == cd.hash {
			continue
		}
		fresh = append(fresh, cd)
	}
	c.join(ctx, fresh)
	c.leave(advertised)
}

// discoveredProxy is the discovered proxy of an instance name.
func (c *Central) discoveredProxy(key string) *proxy {
	for _, p := range c.proxyList() {
		if !p.static && p.key == key {
			return p
		}
	}
	return nil
}

// staticAt reports whether a given proxy is dialled at one of addrs.
func (c *Central) staticAt(addrs []string) bool {
	for _, p := range c.proxyList() {
		if p.static && p.at(addrs) {
			return true
		}
	}
	return false
}

// join connects to the new candidates at once and adds those that are
// usable proxies.
func (c *Central) join(ctx context.Context, cs []candidate) {
	var wg sync.WaitGroup
	for _, cd := range cs {
		wg.Go(func() {
			p, err := newProxy(c, Proxy{Addr: cd.addrs[0], Key: cd.psk, Name: cd.name}, cd.addrs[1:]...)
			if err != nil {
				c.refuse(cd, err)
				return
			}
			p.key = cd.key
			dctx, cancel := clock.WithTimeout(ctx, c.clk, c.opts.ConnectTimeout)
			a, err := p.dial(dctx)
			cancel()
			if err != nil {
				p.stop()
				if permanent(err) {
					c.refuse(cd, err)
				} else if ctx.Err() == nil {
					skip(c.log, cd, err)
				}
				return
			}
			c.setMu.Lock()
			if ctx.Err() != nil || c.ctx.Err() != nil {
				c.setMu.Unlock()
				a.close(context.Canceled)
				p.stop()
				return
			}
			c.proxies = append(c.proxies, p)
			delete(c.skipped, cd.key)
			c.setMu.Unlock()
			c.log.Info("ble.esphome.proxy_discovered", slog.String("proxy", p.name), slog.Any("addresses", cd.addrs), slog.Bool("encrypted", p.psk != nil))
			c.start(p, a)
		})
	}
	wg.Wait()
}

// refuse remembers a device refused for good at its configuration.
func (c *Central) refuse(cd candidate, err error) {
	c.setMu.Lock()
	c.skipped[cd.key] = cd.hash
	c.setMu.Unlock()
	skip(c.log, cd, err)
}

// leave drops the discovered proxies no longer advertised that carry no
// connection; one still connected stays until a browse misses it while
// it is down.
func (c *Central) leave(advertised map[string]bool) {
	c.setMu.Lock()
	var gone []*proxy
	keep := c.proxies[:0:0]
	for _, p := range c.proxies {
		if !p.static && !advertised[p.key] && p.idle() {
			gone = append(gone, p)
			continue
		}
		keep = append(keep, p)
	}
	c.proxies = keep
	c.setMu.Unlock()
	for _, p := range gone {
		p.stop()
		c.forgetProxy(p)
		c.log.Info("ble.esphome.proxy_removed", slog.String("proxy", p.name))
	}
}
