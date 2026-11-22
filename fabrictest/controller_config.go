// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/transport/udp"
)

// ControllerConfig is a controller.Config wired to the site, for a
// program's own controller.New to run on it: a host index with its UDP
// socket on the site's network (Listener) — on a loopback site the
// address New binds itself (Listen [::1]:0), so that with incoming TCP it
// picks a port free for both, as a program's controller does — the
// DNS-SD browser on the
// site's answers (Names), the site's clock carrying the host's in-flight
// counters (Clock), the site's logger, and a SQLite database of its own in
// the site's directory (DB, closed with the site). opts gives the vendor
// id, the label, the logger and the trust store (attestation.TestStore
// when nil; NoAttestationStore has no meaning here, controller.New needs
// a store); the fabric and node ids are controller.New's to draw.
//
// The caller edits the configuration as its program would, builds the
// controller with controller.New and closes it before the site closes —
// under [Site.Run] with a fake clock, as every call that waits on the
// network. With a fake clock New itself and the pairing run under
// Site.Run too. The site's devices do not resolve such a controller as an
// operational peer (a device's subscription re-establishment and an ICD
// Check-In reach the controllers of [Site.AddController] only).
//
// It is the composition of [Site.AddController] for the role's own
// composition root — matter.js's MockSite builds its controller as a
// ServerNode in the same environment (mock-site.ts:112 at b25ed960).
func (s *Site) ControllerConfig(opts ControllerOptions) controller.Config {
	s.tb.Helper()
	index := s.index()
	g := s.newGroup("controller " + strconv.Itoa(index))
	// On loopback New binds the socket itself: with incoming TCP it binds
	// the UDP and TCP pair and binds again while the port is taken for
	// TCP, which it does only for a port it chose (controller/tcp.go
	// bindTransports). A socket bound here left the TCP twin of its
	// ephemeral port to chance — a TCP socket elsewhere on the machine
	// holding the number failed New with EADDRINUSE.
	var l *udp.Listener
	var addr *net.UDPAddr
	if s.net != nil {
		var err error
		if l, err = s.listen(index, g); err != nil {
			s.tb.Fatalf("fabrictest: controller %d: listen: %v", index, err)
		}
		addr = l.LocalAddr()
	}
	db, err := controller.OpenDB(filepath.Join(s.dir, "controller"+strconv.Itoa(index)+".db"))
	if err != nil {
		if l != nil {
			_ = l.Close()
		}
		s.tb.Fatalf("fabrictest: controller %d: %v", index, err)
	}
	s.onClose(func() { s.logTeardown(fmt.Sprintf("controller %d: close the store", index), db.Close()) })
	log := opts.Logger
	if log == nil {
		log = s.log
	}
	trust := opts.Attestation
	if trust == nil {
		trust = attestation.TestStore()
	}
	label := opts.Label
	if label == "" {
		label = "fabrictest"
	}
	cfg := controller.Config{
		DB: db, Attestation: trust, VendorID: opts.VendorID, Label: label, Logger: log,
		Advertiser: s.controllerAdvertiser(index, addr),
		Clock:      s.hostClock(g), Listener: l, Networks: opts.Networks, OwnNetworkProfile: opts.OwnNetworkProfile,
		TCP: opts.TCP, Sessions: opts.Sessions, TransportPreference: opts.TransportPreference,
		Names: func(log *slog.Logger) (*discovery.Names, error) {
			return discovery.NewNames(newQueryConn(s), log), nil
		},
	}
	if l == nil {
		cfg.Listen = loopbackListen
	}
	if opts.OTA != nil {
		ota := *opts.OTA
		cfg.OTA = &ota
	}
	return cfg
}
