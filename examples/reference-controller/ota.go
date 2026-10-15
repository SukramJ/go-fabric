// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
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
	return cmdOTAServe(e, args[1:])
}

// cmdOTAServe serves one image from the own node's OTA provider — the
// provider side of TestLiveOTAProvider (node/ota_live_test.go): the image
// source answers QueryImage for the image's vendor and product below its
// version, the console grants the consent, and --announce sends
// AnnounceOtaProvider to a node. It runs until Ctrl-C, or with --once
// until one update is done.
func cmdOTAServe(e *env, args []string) error {
	var imagePath, announce string
	var version, vendor, product uint
	var once, yes bool
	pos, err := parseArgs(e, "ota serve", args, func(fs *flag.FlagSet) {
		fs.StringVar(&imagePath, "image", "", "the OTA image file (Matter OTA format)")
		fs.UintVar(&version, "version", 0, "the software version offered (default the image header's)")
		fs.UintVar(&vendor, "vendor", 0, "the vendor id it is offered to (default the image header's)")
		fs.UintVar(&product, "product", 0, "the product id it is offered to (default the image header's)")
		fs.StringVar(&announce, "announce", "", "announce the provider to this node")
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
	if version == 0 {
		version = uint(h.SoftwareVersion)
	}
	if vendor == 0 {
		vendor = uint(h.VendorID)
	}
	if product == 0 {
		product = uint(h.ProductID)
	}
	if version > 0xFFFFFFFF || vendor > 0xFFFF || product > 0xFFFF {
		return errors.New("--version, --vendor or --product out of range")
	}
	return withStack(e, func(s *stack) error {
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		self, err := s.ensureSelf(ctx)
		if err != nil {
			return err
		}
		done := make(chan struct{}, 1)
		imgs := self.images
		imgs.mu.Lock()
		imgs.image, imgs.yes = image, yes
		imgs.vendor, imgs.product, imgs.version = uint16(vendor), uint16(product), uint32(version) //nolint:gosec // G115: range checked above
		imgs.versionS = h.SoftwareVersionString
		if imgs.versionS == "" {
			imgs.versionS = strconv.FormatUint(uint64(version), 10)
		}
		imgs.onStatus = func(p mattercore.OTAPeer, st mattercore.OTAUpdateStatus, v uint32) {
			e.printf("node %d: %s (version %d)\n", p.NodeID, statusName(st), v)
			if st == mattercore.OTAUpdateDone {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
		imgs.mu.Unlock()
		provEP, err := self.providerEndpoint()
		if err != nil {
			return err
		}
		e.printf("serving version %d for %04X:%04X from node %d endpoint %d\n", version, vendor, product, self.nodeID, provEP)
		if announce != "" {
			n, id, err := s.node(announce)
			if err != nil {
				return err
			}
			req := otadef.AnnounceOtaProviderRequest{
				ProviderNodeId: self.nodeID, VendorId: fabricVendorID,
				AnnouncementReason: otadef.AnnouncementReasonUpdateAvailable, Endpoint: provEP,
			}
			if _, err := n.Invoke(ctx, rootCommand(otadef.ClusterID, otadef.CmdAnnounceOtaProvider), req, interaction.InvokeOptions{}); err != nil {
				return fmt.Errorf("AnnounceOtaProvider to node %d: %w", id, err)
			}
			e.printf("announced to node %d\n", id)
		}
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

// rootCommand is the root endpoint's command path.
func rootCommand(cluster, command uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: 0, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true}
}
