// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/tlv"
)

// The node's onboarding code — the QR or manual pairing code printed on
// the device — is stored beside the record and the settings, on endpoint 0
// under a cluster id no cluster has. It is this module's: matter.js keeps
// no pairing code (CommissioningClient.State,
// behavior/system/commissioning/CommissioningClient.ts:959 ff. at db021da0,
// has no such field; the passcode is only handed to the commissioner,
// :290). It is not a field of [Record] because a record is rendered by
// diagnostics, and the code is a secret: with it and physical access to
// the device (a factory reset) the device can be commissioned by anyone.
const (
	onboardingCluster uint32 = 0xFFFF_FFFC

	onbCode     uint32 = 0 // the code, normalised
	onbSource   uint32 = 1 // OnboardingSource
	onbStoredAt uint32 = 2 // milliseconds since the Unix epoch
	onbChecks   uint32 = 3 // OnboardingChecks
)

// commissioningModeEnhanced is the commissionable advertisement's CM of an
// Enhanced Commissioning Window: its passcode and its discriminator are
// the window's, minted by the administrator that opened it, and gone when
// it closes (bridge/commissioning_window.go advertises CM=2 for exactly
// that window).
const commissioningModeEnhanced = 2

var (
	// ErrOnboardingCode is a code [Node.SetOnboardingCode] cannot read:
	// neither a QR code nor a manual pairing code, or one whose checksum
	// or passcode is invalid.
	ErrOnboardingCode = errors.New("client: onboarding code")
	// ErrOnboardingMismatch is a readable code that names another device:
	// its vendor, product or discriminator differs from the node's.
	ErrOnboardingMismatch = errors.New("client: onboarding code names another device")
)

// OnboardingSource tells how a node's onboarding code came to be stored.
type OnboardingSource uint8

const (
	// OnboardingPaired is the code the node was commissioned with: the
	// device accepted its passcode.
	OnboardingPaired OnboardingSource = 1
	// OnboardingAttached is a code added after commissioning
	// ([Node.SetOnboardingCode]). Its passcode is unproven: a commissioned
	// device offers no way to test one.
	OnboardingAttached OnboardingSource = 2
)

// OnboardingChecks are the identity checks an attached code passed. A
// check runs only when the node's side of it is known.
type OnboardingChecks uint8

const (
	// OnboardingCheckedVendorProduct says the code's vendor and product
	// ids equal the node's.
	OnboardingCheckedVendorProduct OnboardingChecks = 1 << iota
	// OnboardingCheckedDiscriminator says the code's discriminator equals
	// the one the node advertised while it was commissionable.
	OnboardingCheckedDiscriminator
)

// OnboardingCode is a node's stored onboarding code.
type OnboardingCode struct {
	// Code is the code as entered, normalised: a QR code's "MT:…" text, or
	// a manual pairing code's 11 or 21 digits. It is a secret.
	Code string
	// Source tells whether the device accepted the code or it was attached
	// afterwards.
	Source OnboardingSource
	// StoredAt is when the code was stored.
	StoredAt time.Time
	// Checks are the identity checks an attached code passed; 0 for a
	// paired one, which needs none, and for an attached one whose node
	// had nothing to compare with.
	Checks OnboardingChecks
}

// String keeps the code out of a formatted value.
func (c OnboardingCode) String() string { return "OnboardingCode(redacted)" }

// LogValue keeps the code out of a log record.
func (c OnboardingCode) LogValue() slog.Value { return slog.StringValue(c.String()) }

// Codes are the two forms to show or print: the QR code's text and the
// manual pairing code. A stored QR code yields both — the manual code is
// the short form, or the 21-digit form with the vendor and product ids
// when the code's commissioning flow is not Standard, as connectedhomeip
// generates it (src/setup_payload/ManualSetupPayloadGenerator.cpp:116 at
// f09331bd). A stored manual code yields itself and no QR code: it carries
// four of the discriminator's twelve bits and neither the commissioning
// flow nor the discovery capabilities, and none of those is made up.
func (c OnboardingCode) Codes() (qr, manual string, err error) {
	if !strings.HasPrefix(c.Code, "MT:") {
		return "", c.Code, nil
	}
	p, err := setup.DecodeQRCode(c.Code)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrOnboardingCode, err)
	}
	if p.CustomFlow != 0 {
		manual, err = setup.ManualCodeLong(p.Discriminator, p.Passcode, p.VendorID, p.ProductID)
	} else {
		manual, err = setup.ManualCode(p.Discriminator, p.Passcode)
	}
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrOnboardingCode, err)
	}
	return c.Code, manual, nil
}

// onboardingIdentity is what a code says about its device.
type onboardingIdentity struct {
	// vendor and product are 0 when the code does not carry them.
	vendor, product uint16
	// discriminator is the 12-bit one of a QR code, or the 4-bit short
	// one of a manual code (short set).
	discriminator uint16
	short         bool
}

// parseOnboarding validates code with the decoders [Controller.Pair]
// uses and returns its normalised text and its identity.
func parseOnboarding(code string) (string, onboardingIdentity, error) {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "MT:") {
		p, err := setup.DecodeQRCode(code)
		if err != nil {
			return "", onboardingIdentity{}, fmt.Errorf("%w: %w", ErrOnboardingCode, err)
		}
		return code, onboardingIdentity{vendor: p.VendorID, product: p.ProductID, discriminator: p.Discriminator}, nil
	}
	m, err := setup.DecodeManualCode(code)
	if err != nil {
		return "", onboardingIdentity{}, fmt.Errorf("%w: %w", ErrOnboardingCode, err)
	}
	var digits strings.Builder
	for i := range len(code) {
		if code[i] >= '0' && code[i] <= '9' {
			digits.WriteByte(code[i])
		}
	}
	id := onboardingIdentity{discriminator: uint16(m.ShortDiscriminator), short: true}
	if m.HasVendorProduct {
		id.vendor, id.product = m.VendorID, m.ProductID
	}
	return digits.String(), id, nil
}

// OnboardingCode is the node's stored onboarding code; ok is false when it
// has none.
func (n *Node) OnboardingCode() (code OnboardingCode, ok bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.onboarding == nil {
		return OnboardingCode{}, false
	}
	return *n.onboarding, true
}

// SetOnboardingCode attaches the code printed on the device to a
// commissioned node — typed, scanned or read from a photograph by the
// host — and replaces a stored one. The code is validated as a pairing
// code ([ErrOnboardingCode]) and compared with what the node is known to
// be ([ErrOnboardingMismatch], nothing is stored):
//
//   - the vendor and product ids of a QR code or a 21-digit manual code
//     with the node's Basic Information, or the ids it advertised;
//   - the discriminator with the one the node advertised while it was
//     commissionable — a manual code's four bits with that one's upper
//     four. This check is skipped for a node commissioned through an
//     Enhanced Commissioning Window, whose advertised discriminator was
//     the window's and not the device's.
//
// A side the node does not know is skipped; the result's Checks name the
// checks that ran. No check proves the passcode: a commissioned device
// offers no way to test one, so the code stays [OnboardingAttached].
// A node that is not commissioned is [ErrNotCommissioned].
func (n *Node) SetOnboardingCode(code string) (OnboardingCode, error) {
	norm, id, err := parseOnboarding(code)
	if err != nil {
		return OnboardingCode{}, err
	}
	if !n.IsCommissioned() {
		return OnboardingCode{}, ErrNotCommissioned
	}
	checks, err := n.checkOnboarding(id)
	if err != nil {
		return OnboardingCode{}, err
	}
	c := OnboardingCode{Code: norm, Source: OnboardingAttached, StoredAt: n.clock.Now(), Checks: checks}
	if err := n.storeOnboarding(c); err != nil {
		return OnboardingCode{}, err
	}
	return c, nil
}

// ClearOnboardingCode removes the node's stored onboarding code.
func (n *Node) ClearOnboardingCode() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.erased || n.onboarding == nil {
		return nil
	}
	if err := n.store.EraseCluster(n.cfg.ID, 0, onboardingCluster); err != nil {
		return err
	}
	n.onboarding = nil
	return nil
}

// checkOnboarding compares a code's identity with the node's.
func (n *Node) checkOnboarding(id onboardingIdentity) (OnboardingChecks, error) {
	rec := n.Record()
	var checks OnboardingChecks
	vendor, product := n.identityIDs(rec)
	if id.vendor != 0 && vendor != nil {
		if id.vendor != *vendor {
			return 0, fmt.Errorf("%w: its vendor id is 0x%04X, the node's is 0x%04X", ErrOnboardingMismatch, id.vendor, *vendor)
		}
		if id.product != 0 && product != nil && id.product != *product {
			return 0, fmt.Errorf("%w: its product id is 0x%04X, the node's is 0x%04X", ErrOnboardingMismatch, id.product, *product)
		}
		checks |= OnboardingCheckedVendorProduct
	}
	if rec.Discriminator != nil && rec.CommissioningMode != commissioningModeEnhanced {
		want := *rec.Discriminator
		if id.short {
			want >>= 8
		}
		if id.discriminator != want {
			return 0, fmt.Errorf("%w: its discriminator is %d, the node advertised %d", ErrOnboardingMismatch, id.discriminator, want)
		}
		checks |= OnboardingCheckedDiscriminator
	}
	return checks, nil
}

// identityIDs are the node's vendor and product ids: its Basic
// Information's when the image holds them, else the advertised ones of
// the record; nil when neither is known.
func (n *Node) identityIDs(rec Record) (vendor, product *uint16) {
	vendor, product = rec.VendorID, rec.ProductID
	root := n.Endpoint(0)
	if root == nil {
		return vendor, product
	}
	bi := root.Cluster(basicinformation.ClusterID)
	if v, ok := bi.Value(basicinformation.AttrVendorId); ok && isUint(v) {
		id := uint16(v.El.Uint) //nolint:gosec // a vendor-id attribute
		vendor = &id
	}
	if v, ok := bi.Value(basicinformation.AttrProductId); ok && isUint(v) {
		id := uint16(v.El.Uint) //nolint:gosec // a uint16 attribute
		product = &id
	}
	return vendor, product
}

// keepPairedCode stores the code a commissioning succeeded with. mode is
// the commissioning mode the device advertised: the code of an Enhanced
// Commissioning Window is the window's and useless once it closed, so it
// is not kept. A failure is logged — without the code — and does not fail
// the commissioning.
func (n *Node) keepPairedCode(code string, mode uint8) {
	if mode == commissioningModeEnhanced {
		n.log.Info("matter.controller.client.onboarding_code_not_kept", slog.String("node", n.ID()),
			slog.String("reason", "the code of an enhanced commissioning window ends with the window"))
		return
	}
	norm, _, err := parseOnboarding(code)
	if err == nil {
		err = n.storeOnboarding(OnboardingCode{Code: norm, Source: OnboardingPaired, StoredAt: n.clock.Now()})
	}
	if err != nil {
		n.log.Warn("matter.controller.client.store_write_failed", slog.String("what", "onboarding code"), slog.Any("err", err))
	}
}

// storeOnboarding persists c and makes it the node's code.
func (n *Node) storeOnboarding(c OnboardingCode) error {
	values, err := c.encode()
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.erased {
		return ErrClosed
	}
	if err := n.store.Save(n.cfg.ID, 0, onboardingCluster, values, nil); err != nil {
		return err
	}
	n.onboarding = &c
	return nil
}

// encode is the code's stored form, one TLV element per field.
func (c OnboardingCode) encode() (map[uint32][]byte, error) {
	anon := tlv.AnonymousTag()
	out := make(map[uint32][]byte, 4)
	for id, f := range map[uint32]func(e *tlv.Encoder){
		onbCode:     func(e *tlv.Encoder) { e.PutUTF8(anon, c.Code) },
		onbSource:   func(e *tlv.Encoder) { e.PutUint(anon, uint64(c.Source)) },
		onbStoredAt: func(e *tlv.Encoder) { e.PutUint(anon, uint64(max(c.StoredAt.UnixMilli(), 0))) }, //nolint:gosec // clamped
		onbChecks:   func(e *tlv.Encoder) { e.PutUint(anon, uint64(c.Checks)) },
	} {
		e := tlv.NewEncoder()
		f(e)
		b, err := e.Bytes()
		if err != nil {
			return nil, fmt.Errorf("onboarding field %d: %w", id, err)
		}
		out[id] = b
	}
	return out, nil
}

// decodeOnboarding reads a stored code; nil when the store holds none.
func decodeOnboarding(values map[uint32][]byte) (*OnboardingCode, error) {
	var c OnboardingCode
	for id, b := range values {
		if b == nil {
			continue
		}
		v, err := decodeValue(b)
		if err != nil {
			return nil, fmt.Errorf("onboarding field %d: %w", id, err)
		}
		switch id {
		case onbCode:
			c.Code, err = spec.DecodeString(v)
		case onbSource:
			var s uint8
			s, err = spec.DecodeUint[uint8](8)(v)
			c.Source = OnboardingSource(s)
		case onbStoredAt:
			var ms uint64
			ms, err = spec.DecodeUint[uint64](64)(v)
			c.StoredAt = time.UnixMilli(int64(ms)) //nolint:gosec // written by encode from a time
		case onbChecks:
			var k uint8
			k, err = spec.DecodeUint[uint8](8)(v)
			c.Checks = OnboardingChecks(k)
		}
		if err != nil {
			return nil, fmt.Errorf("onboarding field %d: %w", id, err)
		}
	}
	if c.Code == "" {
		return nil, nil //nolint:nilnil // no stored code is not an error
	}
	return &c, nil
}
