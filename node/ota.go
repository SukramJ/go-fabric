// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// otaApplier is the default [mattercore.OtaApplier], the reference daemon's
// (examples/reference-bridge/ota.go:36-214).
//
// A product applies an image by installing it and restarting into it; the
// requestor then reports the update at the next start (matter.js
// OtaSoftwareUpdateRequestorServer.ts #handlePreviousUpdateOnStart). This
// example cannot flash itself, so it validates the image, keeps it under
// dir/applied/ for an operator, and keeps running the old version — which
// the requestor reports as a failed update after the next restart, as it
// must.
//
// In harness mode it instead reports the image's version as running at
// once, without a restart, and runs the requestor's start-up path in place
// (Restarted): VersionApplied, Applying → Idle, NotifyUpdateApplied. That
// is what the CHIP SU cases read afterwards (TC_SU_2_7 step 7 at the
// harness pin reads SoftwareVersion and the VersionApplied event). The
// matter.js cert requestor does the same through a real restart only when
// started with --rebootAfterApply (support/chip-testing/src/
// OtaRequestorTestInstance.ts:43-48, 151-164); without it, it leaves
// SoftwareVersion alone.
type otaApplier struct {
	vendorID, productID uint16
	basicInfo           *mattercore.BasicInformation
	dir                 string
	harness             bool
	requestor           func() *mattercore.OTASoftwareUpdateRequestor
	logger              *slog.Logger
	clock               clock.Clock
}

var _ mattercore.OtaApplier = (*otaApplier)(nil)

// RequestUserConsent implements [mattercore.OtaApplier]. The node has no
// user to ask; it declares CanConsent only in harness mode, where it
// consents as the matter.js cert requestor does
// (OtaRequestorTestInstance.ts:166-168).
func (a *otaApplier) RequestUserConsent(context.Context, uint32, string) (bool, error) {
	return a.harness, nil
}

// ApplyUpdate implements [mattercore.OtaApplier]: the image's payload digest
// and total size hold, its version is the one the provider offered, it
// carries a payload, and its vendor and product are this node's (a zero
// field applies to every vendor or product, OtaImageHeader.ts:26-41) —
// then it is moved out of the download location, as an applied or
// abandoned file must be (OtaSoftwareUpdateRequestorServer.ts:153-159).
func (a *otaApplier) ApplyUpdate(ctx context.Context, newSoftwareVersion uint32, file string) error {
	header, err := a.verify(newSoftwareVersion, file)
	if err != nil {
		_ = os.Remove(file)
		return err
	}
	appliedDir := filepath.Join(a.dir, "applied")
	if err := os.MkdirAll(appliedDir, 0o750); err != nil {
		return err
	}
	kept := filepath.Join(appliedDir, fmt.Sprintf("ota-%04x-%04x-v%d.ota", a.vendorID, a.productID, newSoftwareVersion))
	if err := os.Rename(file, kept); err != nil {
		_ = os.Remove(file)
		return err
	}
	if !a.harness {
		a.logger.Info("ota.image_stored",
			slog.Uint64("version", uint64(newSoftwareVersion)), slog.String("path", kept),
			slog.String("hint", "install it and restart; this example does not flash itself"))
		return nil
	}
	// A device that flashes its image stays in Applying until it reboots;
	// applied in place, the requestor would leave Applying a millisecond
	// after entering it, inside one subscription tick, and a subscriber
	// would only ever see the Idle that follows. TC-SU-2.2 step 5 waits
	// for Applying on its UpdateState subscription (TC_SU_2_2.py:1232-1262
	// at the harness pin) and timed out on exactly that (run 38154468426).
	if err := sleepCtx(ctx, a.clock, otaHarnessApplyHold); err != nil {
		return err
	}
	if err := a.basicInfo.SetSoftwareVersion(newSoftwareVersion, header.SoftwareVersionString); err != nil {
		return err
	}
	a.logger.Info("ota.applied_in_place", slog.Uint64("version", uint64(newSoftwareVersion)))
	// The requestor returns to Idle once this returns; the start-up path
	// runs after that, as a restart would.
	go a.restartedWhenIdle(context.WithoutCancel(ctx))
	return nil
}

// otaHarnessApplyHold is how long the in-place apply of --ota-harness stays
// in Applying: eight ticks of the subscription engine (250 ms,
// im/subscription/manager.go:185), so the state is reported before the
// requestor returns to Idle.
const otaHarnessApplyHold = 2 * time.Second

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, clk clock.Clock, d time.Duration) error {
	t := clock.Or(clk).NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *otaApplier) verify(version uint32, file string) (otaimage.Header, error) {
	f, err := os.Open(filepath.Clean(file)) //nolint:gosec // the requestor names its own download location
	if err != nil {
		return otaimage.Header{}, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return otaimage.Header{}, err
	}
	size := uint64(fi.Size()) //nolint:gosec // a file size is not negative
	h, err := otaimage.ExtractPayload(f, io.Discard, &size)
	if err != nil {
		return otaimage.Header{}, err
	}
	switch {
	case h.SoftwareVersion != version:
		return h, fmt.Errorf("ota: image is version %d but the provider announced %d", h.SoftwareVersion, version)
	case h.PayloadSize == 0:
		return h, errors.New("ota: image carries no payload")
	case h.VendorID != 0 && h.VendorID != a.vendorID:
		return h, fmt.Errorf("ota: image is for vendor 0x%04x, this node is 0x%04x", h.VendorID, a.vendorID)
	case h.ProductID != 0 && h.ProductID != a.productID:
		return h, fmt.Errorf("ota: image is for product 0x%04x, this node is 0x%04x", h.ProductID, a.productID)
	}
	return h, nil
}

// restartedWhenIdle waits for the requestor to leave Applying, then runs
// its start-up path.
func (a *otaApplier) restartedWhenIdle(ctx context.Context) {
	r := a.requestor()
	if r == nil {
		return
	}
	clk := clock.Or(a.clock)
	deadline := clk.Now().Add(10 * time.Second)
	for clk.Now().Before(deadline) {
		if v, _ := r.MatterRead(otadef.AttrUpdateState); v == uint8(otadef.UpdateStateIdle) {
			break
		}
		if !clock.Sleep(clk, 10*time.Millisecond, ctx.Done()) {
			break
		}
	}
	r.Restarted(ctx)
}

// buildOtaRequestor builds the root's OTA Software Update Requestor
// (ota.go:183-216): the configured applier, or the default one.
func buildOtaRequestor(cfg *Config, connector mattercore.OtaProviderConnector, basicInfo *mattercore.BasicInformation) (*mattercore.OTASoftwareUpdateRequestor, error) {
	ota := cfg.OTA
	if err := os.MkdirAll(ota.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("ota download directory: %w", err)
	}
	var requestor *mattercore.OTASoftwareUpdateRequestor
	applier := ota.Applier
	if applier == nil {
		applier = &otaApplier{
			vendorID: cfg.ProductDescription.VendorID, productID: cfg.ProductDescription.ProductID, basicInfo: basicInfo,
			dir: ota.Dir, harness: ota.Harness, logger: cfg.Logger, clock: cfg.Clock,
			requestor: func() *mattercore.OTASoftwareUpdateRequestor { return requestor },
		}
	}
	rcfg := mattercore.OTARequestorConfig{
		NodeInformation: basicInfo.OtaNodeInformation,
		Connector:       connector,
		Applier:         applier,
		Store:           cfg.Store.Credentials,
		DownloadDir:     ota.Dir,
		CanConsent:      ota.Harness,
		Logger:          cfg.Logger,
		TimeSource:      cfg.Clock,
	}
	if ota.Harness {
		// The matter.js cert requestor's wait after an announcement
		// (OtaRequestorTestInstance.ts:65, :327-335), as the daemon's.
		quick := 250 * time.Millisecond
		rcfg.AnnouncedUpdateQueryDelay = &quick
	}
	r, err := mattercore.NewOTASoftwareUpdateRequestor(rcfg)
	if err != nil {
		return nil, err
	}
	requestor = r
	return r, nil
}
