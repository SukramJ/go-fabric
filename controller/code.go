// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// ErrPairingCode is a pairing code [ParsePairingCode] refuses; [Controller.Pair]
// wraps it the same way. The wrapped error names the rule the code breaks
// in matter.js's words (PairingCodeSchema.ts).
var ErrPairingCode = errors.New("controller: pairing code")

// PairingCode is a decoded onboarding payload: a QR code ("MT:…") or a
// manual pairing code (11 or 21 digits) — matter.js's QrCodeData and
// ManualPairingData (packages/types/src/schema/PairingCodeSchema.ts:106-116,
// :409-421 at 862ad75f).
type PairingCode struct {
	// Manual is true for a manual pairing code, false for a QR code.
	Manual bool
	// Version is the QR payload's format version (0 in Matter 1.x); 0 for
	// a manual code.
	Version uint8
	// VendorID and ProductID are the code's vendor and product; 0 is
	// unspecified, as an 11-digit manual code always is.
	VendorID, ProductID uint16
	// Flow is the QR payload's commissioning flow: 0 Standard, 1 User
	// Action Required, 2 Custom; 0 for a manual code, which carries none.
	Flow uint8
	// Capabilities is the QR payload's discovery capabilities bitmap: bit
	// 0 SoftAP, bit 1 BLE, bit 2 on the IP network; 0 for a manual code.
	Capabilities uint8
	// Discriminator is the 12-bit long discriminator of a QR code, or the
	// 4-bit short discriminator of a manual code.
	Discriminator uint16
	// Passcode is the 27-bit setup passcode.
	Passcode uint32
	// HasExtra reports a QR payload with optional TLV data after its
	// header (a serial number, PBKDF parameters, vendor elements).
	HasExtra bool
}

// ParsePairingCode decodes a QR code ("MT:…", one payload) or a manual
// pairing code (11 or 21 digits; other characters are ignored), with
// matter.js's validation: QrPairingCodeCodec.decode and
// ManualPairingCodeCodec.decode (PairingCodeSchema.ts:330-345, :461-493 at
// 862ad75f), each validating the passcode and the vendor and product
// identity. A code it refuses is [ErrPairingCode].
func ParsePairingCode(code string) (PairingCode, error) {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "MT:") {
		p, err := setup.DecodeQRCode(code)
		if err != nil {
			return PairingCode{}, fmt.Errorf("%w: %w", ErrPairingCode, err)
		}
		return PairingCode{
			Version: p.Version, VendorID: p.VendorID, ProductID: p.ProductID, Flow: p.CustomFlow,
			Capabilities: uint8(p.DiscoveryCaps), Discriminator: p.Discriminator, Passcode: p.Passcode, HasExtra: p.Extra != nil,
		}, nil
	}
	m, err := setup.DecodeManualCode(code)
	if err != nil {
		return PairingCode{}, fmt.Errorf("%w: %w", ErrPairingCode, err)
	}
	return PairingCode{
		Manual: true, VendorID: m.VendorID, ProductID: m.ProductID,
		Discriminator: uint16(m.ShortDiscriminator), Passcode: m.Passcode,
	}, nil
}

// Identifier is the DNS-SD (or BLE) selector of the device the code names:
// its long discriminator for a QR code, its short one for a manual code —
// the identifier matter.js's commissionNode discovers by
// (CommissioningController.ts:462-500 at 862ad75f).
func (p PairingCode) Identifier() discovery.Identifier {
	if p.Manual {
		return discovery.ByShortDiscriminator(uint8(p.Discriminator)) //nolint:gosec // a manual code's 4-bit discriminator
	}
	return discovery.ByLongDiscriminator(p.Discriminator)
}

// DefaultDiscoveryTimeout bounds [Controller.Discover] when its timeout is
// 0 — matter.js discoverCommissionableDevices' Minutes(15)
// (CommissioningController.ts:906-910 at 862ad75f).
const DefaultDiscoveryTimeout = 15 * time.Minute

// Discover searches DNS-SD for the commissionable devices id selects
// until timeout passes on the controller's clock (0 is
// [DefaultDiscoveryTimeout]) or ctx ends, and returns every device found,
// each once, in the order they answered — matter.js
// CommissioningController.discoverCommissionableDevices
// (CommissioningController.ts:906-919, runDiscoverCommissionableDevices
// :84-116 at 862ad75f; a ContinuousDiscovery, Peers.discover,
// packages/node/src/node/client/Peers.ts:204-206). The end of the
// search's time or of ctx is no error; a closed controller is.
func (c *Controller) Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]client.Device, error) {
	if c.names == nil {
		return nil, fmt.Errorf("%w: the controller is closed", ErrConfiguration)
	}
	if timeout <= 0 {
		timeout = DefaultDiscoveryTimeout
	}
	sc := discovery.NewScanner(c.names)
	defer sc.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	t := c.clock.AfterFunc(timeout, cancel)
	defer t.Stop()
	found, err := sc.FindCommissionable(ctx, id, nil)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return found, err
	}
	return found, nil
}
