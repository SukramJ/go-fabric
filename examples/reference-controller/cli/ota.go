// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// statusNames names the OTA update states (mattercore.OTAUpdateStatus).
var statusNames = map[mattercore.OTAUpdateStatus]string{
	mattercore.OTAUpdateUnknown: "Unknown", mattercore.OTAUpdateWaitForConsent: "WaitForConsent",
	mattercore.OTAUpdateQuerying: "Querying", mattercore.OTAUpdateDownloading: "Downloading",
	mattercore.OTAUpdateWaitForApply: "WaitForApply", mattercore.OTAUpdateApplying: "Applying",
	mattercore.OTAUpdateDone: "Done", mattercore.OTAUpdateCancelled: "Cancelled",
}

func statusName(st mattercore.OTAUpdateStatus) string {
	if n, ok := statusNames[st]; ok {
		return n
	}
	return strconv.Itoa(int(st))
}

// cmdOTA dispatches "ota serve".
func cmdOTA(e *env, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: %s", commands["ota"].usage)
	}
	e.ownNode = true
	return cmdOTAServe(e, args[1:])
}

// cmdOTAServe serves one image through the controller's OTA push
// (controller.Config.OTA, controller/softwareupdate): the image goes into
// the update manager's store, the console grants the consent (or --yes),
// and with --announce the manager pushes it to that node — it announces
// the provider and follows the update to its end; without --announce the
// image is offered to every paired node of its vendor and product that
// asks. It runs until Ctrl-C, or with --once until one update is done.
func cmdOTAServe(e *env, args []string) error {
	var imagePath, announce string
	var version, vendor, product uint
	var once, yes bool
	pos, err := parseArgs(e, "ota serve", args, func(fs *flag.FlagSet) {
		fs.StringVar(&imagePath, "image", "", "the OTA image file (Matter OTA format)")
		fs.UintVar(&version, "version", 0, "the software version offered (the image header's; another one is refused)")
		fs.UintVar(&vendor, "vendor", 0, "the vendor id it is offered to (the image header's; another one is refused)")
		fs.UintVar(&product, "product", 0, "the product id it is offered to (the image header's; another one is refused)")
		fs.StringVar(&announce, "announce", "", "push the image to this node")
		fs.BoolVar(&once, "once", false, "exit after the first update is done")
		fs.BoolVar(&yes, "yes", false, "grant every consent without asking")
	})
	if err != nil {
		return err
	}
	if len(pos) != 0 || imagePath == "" {
		return fmt.Errorf("usage: %s", commands["ota"].usage)
	}
	image, err := os.ReadFile(imagePath) //nolint:gosec // G304: the operator names the image file
	if err != nil {
		return err
	}
	h, _, _, err := otaimage.ReadHeader(bytes.NewReader(image))
	if err != nil {
		return fmt.Errorf("%s: %w", imagePath, err)
	}
	// The update manager offers an image by its header (matter.js's OTA
	// store keys it so, DclOtaUpdateService.ts:534-536 at 862ad75f).
	if (version != 0 && version != uint(h.SoftwareVersion)) || (vendor != 0 && vendor != uint(h.VendorID)) || (product != 0 && product != uint(h.ProductID)) {
		return fmt.Errorf("--version, --vendor and --product must match the image header (%d, %04X, %04X)", h.SoftwareVersion, h.VendorID, h.ProductID)
	}
	versionString := h.SoftwareVersionString
	if versionString == "" {
		versionString = strconv.FormatUint(uint64(h.SoftwareVersion), 10)
	}
	return withStack(e, func(s *stack) error {
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		self, err := s.ensureSelf(ctx)
		if err != nil {
			return err
		}
		mgr := s.ctl.SoftwareUpdates()
		if _, err := s.images.Store(ctx, image, softwareupdate.ModeLocal, nil); err != nil {
			return err
		}
		done := make(chan struct{}, 1)
		s.statusMu.Lock()
		s.onStatus = func(p softwareupdate.Peer, st softwareupdate.Status, v uint32) {
			e.printf("node %d: %s (version %d)\n", p.NodeID, statusName(st), v)
			if st == mattercore.OTAUpdateDone {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
		s.statusMu.Unlock()
		provEP, _ := self.n.OTAProviderEndpoint()
		e.printf("serving version %d for %04X:%04X from node %d endpoint %d\n", h.SoftwareVersion, h.VendorID, h.ProductID, self.nodeID, provEP)
		target := softwareupdate.Target{VendorID: h.VendorID, ProductID: h.ProductID, TargetSoftwareVersion: h.SoftwareVersion}
		if announce != "" {
			if err := pushTo(ctx, e, s, mgr, announce, target, versionString, yes); err != nil {
				return err
			}
		} else if err := offerAll(ctx, e, s, mgr, target, versionString, yes); err != nil {
			return err
		}
		defer func() {
			for _, f := range s.restore {
				f()
			}
		}()
		for {
			select {
			case <-e.ctx.Done():
				return nil
			case <-done:
				if once {
					return nil
				}
			}
		}
	})
}

// pushTo starts the node, waits for its subscription — the manager updates
// a node on current data only (OtaAnnouncements.ts:221-253 at 862ad75f) —
// asks the console unless yes, and hands the consent to the manager, which
// announces the provider (SoftwareUpdateManager.addUpdateConsent, :1153-1196).
func pushTo(ctx context.Context, e *env, s *stack, mgr *softwareupdate.Manager, arg string, t softwareupdate.Target, versionString string, yes bool) error {
	n, id, err := s.node(arg)
	if err != nil {
		return err
	}
	// The tool pairs its nodes without a default subscription
	// (pair.go); the update needs one for the time it runs.
	if !n.AutoSubscribe() {
		if err := n.SetAutoSubscribe(ctx, true); err != nil {
			return err
		}
		s.restore = append(s.restore, func() { _ = n.SetAutoSubscribe(context.Background(), false) }) //nolint:contextcheck // runs at the end, after ctx
	}
	if err := n.Start(ctx); err != nil {
		return err
	}
	if err := waitUntil(ctx, n.SubscriptionActive); err != nil {
		return fmt.Errorf("node %d: no subscription: %w", id, err)
	}
	p, _ := n.PeerAddress()
	if !yes && !confirm(e, fmt.Sprintf("update node %d to version %d (%s)?", id, t.TargetSoftwareVersion, versionString)) {
		return errors.New("declined")
	}
	queued, err := mgr.AddUpdateConsent(ctx, p, t)
	if err != nil {
		return err
	}
	if !queued {
		return fmt.Errorf("node %d is not applicable for an OTA update (no OTA requestor, or another vendor or product)", id)
	}
	e.printf("announced to node %d\n", id)
	return nil
}

// offerAll grants the consent for every paired node of the image's vendor
// and product, after one question unless yes: a node that queries on its
// own then gets the image (SoftwareUpdateManager.updateExistsFor,
// :385-468, a consent held for the version).
func offerAll(ctx context.Context, e *env, s *stack, mgr *softwareupdate.Manager, t softwareupdate.Target, versionString string, yes bool) error {
	nodes := s.reg.Commissioned()
	if len(nodes) == 0 {
		return nil
	}
	if !yes && !confirm(e, fmt.Sprintf("offer version %d (%s) to the paired nodes of %04X:%04X?", t.TargetSoftwareVersion, versionString, t.VendorID, t.ProductID)) {
		return errors.New("declined")
	}
	for _, n := range nodes {
		if p, ok := n.PeerAddress(); ok {
			if _, err := mgr.AddUpdateConsent(ctx, p, t); err != nil && !errors.Is(err, softwareupdate.ErrMismatch) {
				return err
			}
		}
	}
	return nil
}

// waitUntil polls cond until it holds or ctx ends.
func waitUntil(ctx context.Context, cond func() bool) error {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for !cond() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}
