// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// ControllerConfig is a controller.Config wired to the site, for a
// program's own controller.New to run on it: a host index with its UDP
// socket on the site's network (Listener), the DNS-SD browser on the
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
// ServerNode in the same environment (mock-site.ts:112 at 862ad75f).
func (s *Site) ControllerConfig(opts ControllerOptions) controller.Config {
	s.tb.Helper()
	index := s.index()
	g := s.newGroup("controller " + strconv.Itoa(index))
	l, err := s.listen(index, g)
	if err != nil {
		s.tb.Fatalf("fabrictest: controller %d: listen: %v", index, err)
	}
	db, err := controller.OpenDB(filepath.Join(s.dir, "controller"+strconv.Itoa(index)+".db"))
	if err != nil {
		_ = l.Close()
		s.tb.Fatalf("fabrictest: controller %d: %v", index, err)
	}
	s.onClose(func() { _ = db.Close() })
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
	return controller.Config{
		DB: db, Attestation: trust, VendorID: opts.VendorID, Label: label, Logger: log,
		Clock: s.hostClock(g), Listener: l, Networks: opts.Networks, OwnNetworkProfile: opts.OwnNetworkProfile,
		Names: func(log *slog.Logger) (*discovery.Names, error) {
			return discovery.NewNames(newQueryConn(s), log), nil
		},
	}
}
