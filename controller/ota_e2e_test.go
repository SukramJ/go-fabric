// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// otaRig is a site with a controller whose OTA push runs on its own node
// and a device with the module's OTA requestor (harness mode: it applies
// in place and reports the new version, node/ota.go) — the setting of
// matter.js's OtaTest (packages/node/test/behaviors/ota/OtaTest.ts at
// 862ad75f: a controller with the provider, a device with the requestor).
type otaRig struct {
	site   *fabrictest.Site
	dev    *fabrictest.Device
	c      *controller.Controller
	n      *client.Node
	images *softwareupdate.Images

	mu       sync.Mutex
	statuses []mattercore.OTAUpdateStatus
	done     []softwareupdate.Peer
	failed   []softwareupdate.Peer
	applying []softwareupdate.Peer
	avail    []softwareupdate.SoftwareUpdateInfo
}

func newOTARig(t *testing.T, edit func(*controller.OTAConfig)) *otaRig {
	t.Helper()
	return newOTARigWith(t, edit, &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota"), Harness: true})
}

// newOTARigWith is newOTARig with the device's OTA requestor configured
// by devOTA; nil runs the device without one.
func newOTARigWith(t *testing.T, edit func(*controller.OTAConfig), devOTA *node.OtaConfig) *otaRig {
	t.Helper()
	r := &otaRig{site: fabrictest.NewSite(t, fabrictest.Options{}), images: softwareupdate.NewImages(softwareupdate.ImagesConfig{})}
	r.dev = r.site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) { cfg.OTA = devOTA }})
	ota := &controller.OTAConfig{
		Images: r.images, AllowTestImages: true,
		OnStatus: func(_ softwareupdate.Peer, s softwareupdate.Status, _ uint32) {
			r.mu.Lock()
			r.statuses = append(r.statuses, s)
			r.mu.Unlock()
		},
		OnUpdateDone:     func(p softwareupdate.Peer) { r.mu.Lock(); r.done = append(r.done, p); r.mu.Unlock() },
		OnUpdateFailed:   func(p softwareupdate.Peer) { r.mu.Lock(); r.failed = append(r.failed, p); r.mu.Unlock() },
		OnUpdateApplying: func(p softwareupdate.Peer) { r.mu.Lock(); r.applying = append(r.applying, p); r.mu.Unlock() },
		OnUpdateAvailable: func(_ softwareupdate.Peer, i softwareupdate.SoftwareUpdateInfo) {
			r.mu.Lock()
			r.avail = append(r.avail, i)
			r.mu.Unlock()
		},
	}
	if edit != nil {
		edit(ota)
	}
	cfg := r.site.ControllerConfig(fabrictest.ControllerOptions{OTA: ota})
	r.run(t, func(ctx context.Context) (err error) {
		r.c, err = controller.New(ctx, cfg)
		return err
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = r.site.Run(ctx, r.c.Close)
	})
	r.run(t, func(ctx context.Context) (err error) {
		r.n, err = r.c.Pair(ctx, manualCode(t, r.dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	r.eventually(t, "the default subscription", r.n.SubscriptionActive)
	return r
}

func (r *otaRig) run(t *testing.T, fn func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := r.site.Run(ctx, fn); err != nil {
		t.Fatal(err)
	}
}

func (r *otaRig) eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := r.site.Eventually(ctx, what, cond); err != nil {
		t.Fatal(err)
	}
}

// isolate drops every datagram to and from the device while on: an
// announcement the manager sends then waits out its retransmissions, and
// the queued entry stays queued.
func (r *otaRig) isolate(on bool) {
	if !on {
		r.site.Net().Filter(nil)
		return
	}
	dev := r.dev.Addr().String()
	r.site.Net().Filter(func(d fabrictest.Datagram) bool { return d.From.String() != dev && d.To.String() != dev })
}

func (r *otaRig) peer(t *testing.T) softwareupdate.Peer {
	t.Helper()
	p, ok := r.n.PeerAddress()
	if !ok {
		t.Fatal("not commissioned")
	}
	return p
}

// storeImage stores an image of the device's next version (addTestOtaImage,
// ota-utils.ts:183-206).
func (r *otaRig) storeImage(t *testing.T, version uint32) {
	t.Helper()
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: version, SoftwareVersionString: "v2.0.0",
		Payload: bytes.Repeat([]byte{0x5A, 0xA5}, 25*1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.images.Store(context.Background(), image, softwareupdate.ModeTest, nil); err != nil {
		t.Fatal(err)
	}
}

func (r *otaRig) snapshot() (statuses []mattercore.OTAUpdateStatus, done, failed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.statuses), len(r.done), len(r.failed)
}

// TestOTAPushFromTheController: the controller's own node serves an image
// the controller pushes to a device — consent, announcement, QueryImage,
// a BDX download, the apply request and the applied notification — and the
// device runs the new version (OtaTest.ts:91 "Successfully processes a
// software update"). It is the end-to-end proof of brief CM §1.4.
func TestOTAPushFromTheController(t *testing.T) {
	t.Parallel()
	r := newOTARig(t, nil)
	r.storeImage(t, 2)
	mgr := r.c.SoftwareUpdates()
	if mgr == nil {
		t.Fatal("no update manager")
	}
	var queued bool
	r.run(t, func(ctx context.Context) (err error) {
		queued, err = mgr.AddUpdateConsent(ctx, r.peer(t), softwareupdate.Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
		return err
	})
	if !queued {
		t.Fatal("the update was not queued")
	}
	r.eventually(t, "the update done", func() bool { _, done, _ := r.snapshot(); return done > 0 })
	statuses, _, failed := r.snapshot()
	if failed != 0 {
		t.Fatalf("an update failed: %v", statuses)
	}
	want := []mattercore.OTAUpdateStatus{mattercore.OTAUpdateQuerying, mattercore.OTAUpdateDownloading, mattercore.OTAUpdateWaitForApply, mattercore.OTAUpdateApplying, mattercore.OTAUpdateDone}
	for _, s := range want {
		if !slices.Contains(statuses, s) {
			t.Errorf("statuses %v lack %v", statuses, s)
		}
	}
	if v, _ := r.dev.Node().Root().BasicInformation.MatterRead(0x0009); v != uint32(2) {
		t.Errorf("device SoftwareVersion %v, want 2", v)
	}
	if q := mgr.QueuedUpdates(); len(q) != 0 {
		t.Errorf("queue %v after the update", q)
	}
}
