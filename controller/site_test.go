// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// siteController builds a controller with controller.New on a fabrictest
// site (Site.ControllerConfig) next to one device, paced by the site on
// its fake clock; edit adjusts the configuration first.
func siteController(t *testing.T, topo fabrictest.Topology, edit func(*controller.Config)) (*fabrictest.Site, *fabrictest.Device, *controller.Controller) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(topo)
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	if edit != nil {
		edit(&cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, cfg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = site.Run(ctx, c.Close)
	})
	return site, dev, c
}

// manualCode is the device's manual pairing code.
func manualCode(t *testing.T, dev *fabrictest.Device) string {
	t.Helper()
	code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// sitePair is siteController with the device paired by its manual code.
// The site's device carries the module's test attestation, whose
// Certification Declaration of certification_type 0 (test) the
// commissioner reports as a CertificationTypeTest finding; the pairing
// accepts it (attestation.Accept).
func sitePair(t *testing.T, topo fabrictest.Topology, edit func(*controller.Config)) (*fabrictest.Site, *fabrictest.Device, *controller.Controller, *client.Node) {
	t.Helper()
	site, dev, c := siteController(t, topo, edit)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var n *client.Node
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		n, err = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	return site, dev, c, n
}

// TestControllerOnASite: controller.New on Site.ControllerConfig — the
// site's socket, DNS-SD and clock — pairs the site's device by its manual
// code and reads its NodeLabel.
func TestControllerOnASite(t *testing.T) {
	t.Parallel()
	site, dev, c, n := sitePair(t, fabrictest.Topology{}, nil)
	if len(c.Nodes()) != 1 || !n.IsCommissioned() {
		t.Fatalf("nodes %d, commissioned %v", len(c.Nodes()), n.IsCommissioned())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var rep *client.Report
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
		rep, err = n.Read(ctx, req)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := "device" + strconv.Itoa(dev.Index())
	if v, ok := rep.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok || v.El.String != want {
		t.Fatalf("NodeLabel %+v (%v), want %s", v.El, ok, want)
	}
}

// TestOpenDBAndProductionTrust: OpenDB opens a file New runs on, and the
// production trust holds the embedded snapshot's roots without the test
// PAAs.
func TestOpenDBAndProductionTrust(t *testing.T) {
	t.Parallel()
	if _, err := controller.OpenDB(""); err == nil {
		t.Fatal("OpenDB(\"\") succeeded")
	}
	db, err := controller.OpenDB(t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	trust, err := controller.ProductionTrust()
	if err != nil {
		t.Fatal(err)
	}
	if trust.AcceptsTestCertificates() {
		t.Fatal("the production trust accepts test certificates")
	}
	cfg := config(db)
	cfg.Attestation = trust
	c, err := controller.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
