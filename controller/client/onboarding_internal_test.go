// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/secure/setup"
)

const (
	onbVendor, onbProduct = 0xFFF1, 0x8000
	onbDiscriminator      = 0xF00
	onbPasscode           = 20202021
)

// onboardingNode is a commissioned node over st that advertised the
// identity above in commissioning mode mode; its log goes to the buffer,
// attributes included.
func onboardingNode(t *testing.T, st Store, mode uint8) (*Node, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	n, err := New(Config{
		ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}),
		Logger: slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	n.updateRecord(func(r *Record) {
		v, p, d := uint16(onbVendor), uint16(onbProduct), uint16(onbDiscriminator)
		r.PeerAddress = &handshake.PeerAddress{FabricIndex: 1, NodeID: 7}
		r.VendorID, r.ProductID, r.Discriminator, r.CommissioningMode = &v, &p, &d, mode
	})
	return n, &logs
}

func qrOf(t *testing.T, p setup.Payload) string {
	t.Helper()
	p.DiscoveryCaps = setup.DiscoveryOnIP
	s, err := setup.QRCode(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func manualOf(t *testing.T, discriminator uint16) string {
	t.Helper()
	s, err := setup.ManualCode(discriminator, onbPasscode)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestOnboardingCodeAttach: a QR code that names the node is stored with
// both checks, yields both printable forms, survives a reload and goes
// with the node's erase; the code appears in no log line.
func TestOnboardingCodeAttach(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, logs := onboardingNode(t, st, 1)
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("a new node has a code")
	}
	qr := qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode})
	c, err := n.SetOnboardingCode("  " + qr + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != qr || c.Source != OnboardingAttached || c.Checks != OnboardingCheckedVendorProduct|OnboardingCheckedDiscriminator {
		t.Fatalf("stored %q source %d checks %b", c.Code, c.Source, c.Checks)
	}
	gotQR, gotManual, err := c.Codes()
	if err != nil || gotQR != qr || gotManual != manualOf(t, onbDiscriminator) {
		t.Fatalf("Codes = %q, %q, %v", gotQR, gotManual, err)
	}
	if s := fmt.Sprintf("%v %s %+v", c, c, &c); strings.Contains(s, qr) {
		t.Fatalf("a formatted code shows the secret: %s", s)
	}

	again, _ := onboardingNode(t, st, 1)
	if got, ok := again.OnboardingCode(); !ok || got.Code != qr || got.Source != OnboardingAttached || got.Checks != c.Checks || !got.StoredAt.Equal(c.StoredAt.Truncate(1e6)) {
		t.Fatalf("after a reload: %q ok=%v checks %b at %v, stored at %v", got.Code, ok, got.Checks, got.StoredAt, c.StoredAt)
	}
	if again.Endpoint(0) != nil && again.Endpoint(0).Cluster(onboardingCluster) != nil {
		t.Fatal("the stored code became a cluster of the image")
	}

	if err := again.ClearOnboardingCode(); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.OnboardingCode(); ok {
		t.Fatal("cleared, and still there")
	}
	if third, _ := onboardingNode(t, st, 1); func() bool { _, ok := third.OnboardingCode(); return ok }() {
		t.Fatal("cleared, and back after a reload")
	}
	if strings.Contains(logs.String(), qr) || strings.Contains(logs.String(), strconv.Itoa(onbPasscode)) {
		t.Fatalf("the log holds the code:\n%s", logs.String())
	}
}

// TestOnboardingCodeErasedWithTheNode: Node.Erase leaves no code in the
// store.
func TestOnboardingCodeErasedWithTheNode(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _ := onboardingNode(t, st, 1)
	if _, err := n.SetOnboardingCode(manualOf(t, onbDiscriminator)); err != nil {
		t.Fatal(err)
	}
	if err := n.Erase(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("erased, and the node still has its code")
	}
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		for _, c := range ep.Clusters {
			if c.ID == onboardingCluster {
				t.Fatal("erased, and the store still has the code")
			}
		}
	}
}

// TestOnboardingCodeRefused: an unreadable code, a code of another device
// and a node that is not commissioned store nothing.
func TestOnboardingCodeRefused(t *testing.T) {
	t.Parallel()
	base := setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode}
	otherVendor, otherProduct, otherDisc := base, base, base
	otherVendor.VendorID++
	otherProduct.ProductID++
	otherDisc.Discriminator = 0x123
	long, err := setup.ManualCodeLong(onbDiscriminator, onbPasscode, onbVendor+1, onbProduct)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		code string
		want error
	}{
		"not a code":             {"hello", ErrOnboardingCode},
		"bad check digit":        {manualOf(t, onbDiscriminator)[:10] + "0", ErrOnboardingCode},
		"qr, other vendor":       {qrOf(t, otherVendor), ErrOnboardingMismatch},
		"qr, other product":      {qrOf(t, otherProduct), ErrOnboardingMismatch},
		"qr, other disc.":        {qrOf(t, otherDisc), ErrOnboardingMismatch},
		"manual, other disc.":    {manualOf(t, 0x100), ErrOnboardingMismatch},
		"21 digit, other vendor": {long, ErrOnboardingMismatch},
	} {
		n, _ := onboardingNode(t, &MemoryStore{}, 1)
		if tc.code == manualOf(t, onbDiscriminator) {
			t.Fatalf("%s: the case is the valid code", name)
		}
		if _, err := n.SetOnboardingCode(tc.code); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if _, ok := n.OnboardingCode(); ok {
			t.Errorf("%s: a refused code was stored", name)
		}
	}
	n, _, _ := testNode(t, &MemoryStore{})
	if _, err := n.SetOnboardingCode(manualOf(t, onbDiscriminator)); !errors.Is(err, ErrNotCommissioned) {
		t.Fatalf("not commissioned: err = %v", err)
	}
}

// TestOnboardingCodeChecksOnlyWhatIsKnown: a manual code is compared by
// its short discriminator only; after an Enhanced Commissioning Window
// the advertised discriminator was the window's and is not compared; a
// node that advertised nothing gets no check.
func TestOnboardingCodeChecksOnlyWhatIsKnown(t *testing.T) {
	t.Parallel()
	n, _ := onboardingNode(t, &MemoryStore{}, 1)
	c, err := n.SetOnboardingCode(manualOf(t, onbDiscriminator))
	if err != nil || c.Checks != OnboardingCheckedDiscriminator {
		t.Fatalf("manual code: checks %b, err %v", c.Checks, err)
	}
	if qr, manual, err := c.Codes(); err != nil || qr != "" || manual != manualOf(t, onbDiscriminator) {
		t.Fatalf("a manual code's forms: %q, %q, %v", qr, manual, err)
	}

	window, _ := onboardingNode(t, &MemoryStore{}, commissioningModeEnhanced)
	c, err = window.SetOnboardingCode(qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: 0x123, Passcode: onbPasscode}))
	if err != nil || c.Checks != OnboardingCheckedVendorProduct {
		t.Fatalf("after an enhanced window: checks %b, err %v", c.Checks, err)
	}

	bare, _ := onboardingNode(t, &MemoryStore{}, 0)
	bare.updateRecord(func(r *Record) { r.VendorID, r.ProductID, r.Discriminator = nil, nil, nil })
	c, err = bare.SetOnboardingCode(manualOf(t, 0x100))
	if err != nil || c.Checks != 0 {
		t.Fatalf("nothing known: checks %b, err %v", c.Checks, err)
	}
}

// TestOnboardingCodeKeptAtPairing: the code of a commissioning is kept as
// paired — but not the code of an Enhanced Commissioning Window.
func TestOnboardingCodeKeptAtPairing(t *testing.T) {
	t.Parallel()
	code := manualOf(t, onbDiscriminator)
	n, logs := onboardingNode(t, &MemoryStore{}, 1)
	n.keepPairedCode(code, 1)
	if c, ok := n.OnboardingCode(); !ok || c.Code != code || c.Source != OnboardingPaired || c.Checks != 0 {
		t.Fatalf("paired: %q ok=%v source %d", c.Code, ok, c.Source)
	}
	window, wlogs := onboardingNode(t, &MemoryStore{}, commissioningModeEnhanced)
	window.keepPairedCode(code, commissioningModeEnhanced)
	if _, ok := window.OnboardingCode(); ok {
		t.Fatal("a window's code was kept")
	}
	if !strings.Contains(wlogs.String(), "onboarding_code_not_kept") {
		t.Fatal("the skipped code is not logged")
	}
	if s := logs.String() + wlogs.String(); strings.Contains(s, code) {
		t.Fatalf("the log holds the code:\n%s", s)
	}
}

// TestOnboardingCodeUnreadableInTheStore: a stored code that cannot be
// decoded is no code, and the node loads.
func TestOnboardingCodeUnreadableInTheStore(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	if err := st.Save("n", 0, onboardingCluster, map[uint32][]byte{onbCode: {0xFF}}, nil); err != nil {
		t.Fatal(err)
	}
	n, logs := onboardingNode(t, st, 1)
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("an unreadable code was loaded")
	}
	if !strings.Contains(logs.String(), "onboarding_code_unreadable") {
		t.Fatal("the unreadable code is not logged")
	}
}
