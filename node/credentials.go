// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"
	"math"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// NewCommissioningCredentials draws a fresh commissioning secret: a random
// passcode from the valid set (bridge.RandomPasscode, never on the
// trivially-guessable list), a random 12-bit discriminator
// (bridge.RandomDiscriminator), a random salt of [PBKDFSaltMax] bytes
// (bridge.RandomSalt) and [PBKDFIterationsMin] PBKDF iterations — values
// [Config.Validate] accepts. LocationCapability is left at its zero value
// ([RegulatoryIndoor]). A host that has no configured secret uses it for
// [Config].Commissioning, or copies its fields into the
// [WindowCredentials] of one [Node.OpenBasicWindow]; it persists the
// result itself when the node's own window must keep its pairing code
// across restarts.
func NewCommissioningCredentials() (Commissioning, error) {
	passcode, err := bridge.RandomPasscode()
	if err != nil {
		return Commissioning{}, fmt.Errorf("node: commissioning credentials: %w", err)
	}
	discriminator, err := bridge.RandomDiscriminator()
	if err != nil {
		return Commissioning{}, fmt.Errorf("node: commissioning credentials: %w", err)
	}
	salt, err := bridge.RandomSalt()
	if err != nil {
		return Commissioning{}, fmt.Errorf("node: commissioning credentials: %w", err)
	}
	return Commissioning{
		Passcode:        passcode,
		Discriminator:   discriminator,
		PBKDFIterations: PBKDFIterationsMin,
		Salt:            salt,
	}, nil
}

// NewWindowCredentials draws fresh credentials for one
// [Node.OpenBasicWindow] window that stays open for timeout (0 is the
// node's own window default): a random 12-bit discriminator, a random
// passcode that passes [ValidateSetupPIN], a random 32-byte salt and
// [PBKDFIterationsMin] PBKDF iterations — NewCommissioningCredentials()
// .WindowCredentials(timeout). A host that serves "open a window with a
// fresh code" draws them here instead of re-implementing the passcode
// rule; [Node.PairingCodesFor] encodes their codes.
//
// Mirrors matter.js PaseClient.generateRandomPasscode and
// generateRandomDiscriminator (packages/protocol/src/session/pase/
// PaseClient.ts:55-71 at 862ad75f; the rule is PairingCodeSchema.ts:
// 171-178 isValidPasscode) and the random 32-byte salt and 1000
// iterations of CommissioningClient.openEnhancedCommissioningWindow
// (packages/node/src/behavior/system/commissioning/
// CommissioningClient.ts:522-525), which draws the same for a window it opens
// on a peer.
func NewWindowCredentials(timeout time.Duration) (WindowCredentials, error) {
	c, err := NewCommissioningCredentials()
	if err != nil {
		return WindowCredentials{}, err
	}
	return c.WindowCredentials(timeout), nil
}

// WindowCredentials are c's secret as the credentials of one
// [Node.OpenBasicWindow] window that stays open for timeout (0 is the
// node's own window default) — a fresh window secret is
// NewCommissioningCredentials().WindowCredentials(timeout).
func (c Commissioning) WindowCredentials(timeout time.Duration) WindowCredentials {
	// Clamped to uint32's range through int64, so the bound is a valid
	// constant on a 32-bit int too; OpenBasicWindow refuses what lies
	// outside [PBKDFIterationsMin, PBKDFIterationsMax].
	v := int64(c.PBKDFIterations)
	if v < 0 {
		v = 0
	}
	if v > math.MaxUint32 {
		v = math.MaxUint32
	}
	iterations := uint32(v)
	return WindowCredentials{
		Discriminator: c.Discriminator,
		Passcode:      c.Passcode,
		Iterations:    iterations,
		Salt:          append([]byte(nil), c.Salt...),
		Timeout:       timeout,
	}
}

// PairingCodesFor returns the QR code payload and the manual pairing code
// of a window the host opens with its own credentials
// ([Node.OpenBasicWindow]): creds' passcode and discriminator under the
// node's vendor and product, on-network discovery — what [Node.PairingCodes]
// returns for the configured secret.
func (n *Node) PairingCodesFor(creds WindowCredentials) (qr, manual string, err error) {
	return pairingCodes(n.cfg.ProductDescription, creds.Discriminator, creds.Passcode)
}

// pairingCodes encodes one passcode and discriminator as the QR payload
// and the manual code.
func pairingCodes(pd ProductDescription, discriminator uint16, passcode uint32) (qr, manual string, err error) {
	qr, err = setup.QRCode(setup.Payload{
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		Discriminator: discriminator,
		Passcode:      passcode,
		DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		return "", "", fmt.Errorf("node: qr payload: %w", err)
	}
	manual, err = setup.ManualCode(discriminator, passcode)
	if err != nil {
		return "", "", fmt.Errorf("node: manual code: %w", err)
	}
	return qr, manual, nil
}
