// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"bytes"
	"encoding/json"
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
	if c.Code.Reveal() != qr || c.Source != OnboardingAttached || c.Checks != OnboardingCheckedVendor|OnboardingCheckedProduct|OnboardingCheckedDiscriminator {
		t.Fatalf("stored source %d checks %b", c.Source, c.Checks)
	}
	gotQR, gotManual, err := c.Codes()
	if err != nil || gotQR.Reveal() != qr || gotManual.Reveal() != manualOf(t, onbDiscriminator) {
		t.Fatalf("Codes = %q, %q, %v", gotQR, gotManual, err)
	}
	if s := fmt.Sprintf("%v %s %+v", c, c, &c); strings.Contains(s, qr) {
		t.Fatalf("a formatted code shows the secret: %s", s)
	}

	again, _ := onboardingNode(t, st, 1)
	if got, ok := again.OnboardingCode(); !ok || got.Code.Reveal() != qr || got.Source != OnboardingAttached || got.Checks != c.Checks || !got.StoredAt.Equal(c.StoredAt.Truncate(1e6)) {
		t.Fatalf("after a reload: ok=%v checks %b at %v, stored at %v", ok, got.Checks, got.StoredAt, c.StoredAt)
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
	if qr, manual, err := c.Codes(); err != nil || !qr.IsZero() || manual.Reveal() != manualOf(t, onbDiscriminator) {
		t.Fatalf("a manual code's forms: %q, %q, %v", qr, manual, err)
	}

	window, _ := onboardingNode(t, &MemoryStore{}, commissioningModeEnhanced)
	c, err = window.SetOnboardingCode(qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: 0x123, Passcode: onbPasscode}))
	if err != nil || c.Checks != OnboardingCheckedVendor|OnboardingCheckedProduct {
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
	if c, ok := n.OnboardingCode(); !ok || c.Code.Reveal() != code || c.Source != OnboardingPaired || c.Checks != 0 {
		t.Fatalf("paired: ok=%v source %d", ok, c.Source)
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

// TestOnboardingCodeOfANodePairedByAddress: a node paired by address
// advertised nothing — its record's discriminator is the zero of a Device
// without one — so the device's real code is accepted without a
// discriminator check, and the code of its pairing is not kept as proven.
func TestOnboardingCodeOfANodePairedByAddress(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	// The device of controller.DeviceAt: addresses and nothing else.
	d := device("", "[fd00::2]:5540")
	d.D, d.CM = 0, 0
	n.updateRecord(func(r *Record) {
		r.applyDevice(&d, n.clock.Now())
		r.PeerAddress = &handshake.PeerAddress{FabricIndex: 1, NodeID: 7}
	})
	if rec := n.Record(); rec.Discriminator == nil || *rec.Discriminator != 0 || rec.CommissioningMode != 0 {
		t.Fatalf("the premise moved: mode %d", rec.CommissioningMode)
	}
	n.keepPairedCode(manualOf(t, onbDiscriminator), n.Record().CommissioningMode)
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("a code was kept as paired without an advertisement")
	}
	for _, code := range []string{
		qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode}),
		manualOf(t, onbDiscriminator),
	} {
		c, err := n.SetOnboardingCode(code)
		if err != nil || c.Checks != 0 || c.Source != OnboardingAttached {
			t.Fatalf("the device's code: checks %b source %d err %v", c.Checks, c.Source, err)
		}
	}
}

// TestOnboardingCodeClearRemovesAnUnreadableRow: a stored row with a
// readable code and a broken field is no code — and Clear still removes
// the secret from the store.
func TestOnboardingCodeClearRemovesAnUnreadableRow(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	values, err := OnboardingCode{Code: newSecretCode(manualOf(t, onbDiscriminator)), Source: OnboardingPaired}.encode()
	if err != nil {
		t.Fatal(err)
	}
	values[onbSource] = []byte{0xFF}
	if err := st.Save("n", 0, onboardingCluster, values, nil); err != nil {
		t.Fatal(err)
	}
	n, _ := onboardingNode(t, st, 1)
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("a broken row was loaded")
	}
	if err := n.ClearOnboardingCode(); err != nil {
		t.Fatal(err)
	}
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		for _, c := range ep.Clusters {
			if c.ID == onboardingCluster {
				t.Fatal("cleared, and the store still has the row")
			}
		}
	}
}

// TestOnboardingCodeStoredForm: a source the module does not know is not
// loaded; unknown check bits are dropped; %#v shows no code.
func TestOnboardingCodeStoredForm(t *testing.T) {
	t.Parallel()
	code := manualOf(t, onbDiscriminator)
	values, err := OnboardingCode{Code: newSecretCode(code), Source: 7}.encode()
	if err != nil {
		t.Fatal(err)
	}
	if c, err := decodeOnboarding(values); err == nil {
		t.Fatalf("source 7 loaded: %v", c)
	}
	values, err = OnboardingCode{Code: newSecretCode(code), Source: OnboardingAttached, Checks: 0xF0 | OnboardingCheckedDiscriminator}.encode()
	if err != nil {
		t.Fatal(err)
	}
	c, err := decodeOnboarding(values)
	if err != nil || c.Checks != OnboardingCheckedDiscriminator {
		t.Fatalf("checks %b, err %v", c.Checks, err)
	}
	if s := fmt.Sprintf("%#v %#v", *c, c); strings.Contains(s, code) {
		t.Fatalf("%%#v shows the secret: %s", s)
	}
}

// TestOnboardingCodeLongManualForm: a QR code with a non-standard
// commissioning flow yields the 21-digit manual code.
func TestOnboardingCodeLongManualForm(t *testing.T) {
	t.Parallel()
	qr := qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode, CustomFlow: 1})
	want, err := setup.ManualCodeLong(onbDiscriminator, onbPasscode, onbVendor, onbProduct)
	if err != nil {
		t.Fatal(err)
	}
	if _, manual, err := (OnboardingCode{Code: newSecretCode(qr)}).Codes(); err != nil || manual.Reveal() != want {
		t.Fatalf("manual %q, want %q, err %v", manual, want, err)
	}
}

// TestOnboardingCodeAttachingThePairedCode: attaching the code the node
// was paired with leaves it proven; another form of it is attached.
func TestOnboardingCodeAttachingThePairedCode(t *testing.T) {
	t.Parallel()
	n, _ := onboardingNode(t, &MemoryStore{}, 1)
	code := manualOf(t, onbDiscriminator)
	n.keepPairedCode(code, 1)
	if c, err := n.SetOnboardingCode(code); err != nil || c.Source != OnboardingPaired {
		t.Fatalf("the same code: source %d, err %v", c.Source, err)
	}
	qr := qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode})
	if c, err := n.SetOnboardingCode(qr); err != nil || c.Source != OnboardingAttached {
		t.Fatalf("its QR form: source %d, err %v", c.Source, err)
	}
}

// TestSecretCodeIsRedactedEverywhere: the code's text reaches no rendering
// it was not revealed to — every fmt verb, encoding/json, both slog
// handlers — as itself, as an [OnboardingCode], and as a value, pointer or
// unexported field of a host's own struct. Reveal is the one way to it.
func TestSecretCodeIsRedactedEverywhere(t *testing.T) {
	t.Parallel()
	type host struct {
		Name   string
		Value  OnboardingCode
		Ptr    *OnboardingCode
		Secret SecretCode
		List   []SecretCode
		Map    map[string]SecretCode
		hidden SecretCode
		inner  OnboardingCode
	}
	for _, text := range []string{
		qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct, Discriminator: onbDiscriminator, Passcode: onbPasscode}),
		manualOf(t, onbDiscriminator),
	} {
		secret := newSecretCode(text)
		if secret.Reveal() != text || secret.IsZero() {
			t.Fatal("Reveal does not return the text")
		}
		code := OnboardingCode{Code: secret, Source: OnboardingPaired}
		h := host{Name: "h", Value: code, Ptr: &code, Secret: secret, List: []SecretCode{secret}, Map: map[string]SecretCode{"k": secret}, hidden: secret, inner: code}
		values := []any{secret, &secret, code, &code, h, &h, []any{secret, code}, map[string]any{"k": h}}
		// The forms the text can take under a verb: itself, quoted, hex of
		// either case.
		leaks := []string{text, fmt.Sprintf("%x", text), fmt.Sprintf("%X", text)}
		check := func(what, out string) {
			t.Helper()
			for _, leak := range leaks {
				if strings.Contains(out, leak) {
					t.Fatalf("%s shows the secret: %s", what, out)
				}
			}
		}
		for _, v := range values {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%T", "%p", "%c", "%U", "%o", "%b", "%e", "%t"} {
				check(fmt.Sprintf("%s of %T", verb, v), fmt.Sprintf(verb, v))
			}
			check(fmt.Sprintf("Sprint of %T", v), fmt.Sprint(v))
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("json of %T: %v", v, err)
			}
			check(fmt.Sprintf("json of %T", v), string(b))
			for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
				"slog text": func(w *bytes.Buffer) slog.Handler { return slog.NewTextHandler(w, nil) },
				"slog json": func(w *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(w, nil) },
			} {
				var buf bytes.Buffer
				log := slog.New(handler(&buf))
				log.Info("m", slog.Any("v", v), slog.Group("g", slog.Any("v", v)))
				log.With(slog.Any("w", v)).Info("m", "k", v)
				check(fmt.Sprintf("%s of %T", name, v), buf.String())
			}
		}
		if b, err := json.Marshal(secret); err != nil || string(b) != `"redacted"` {
			t.Fatalf("json of a code: %s, %v", b, err)
		}
	}
	var zero SecretCode
	if zero.Reveal() != "" || !zero.IsZero() || zero.String() != "" {
		t.Fatal("the zero value is not no code")
	}
}

// TestOnboardingChecksNameWhatRan: a node whose product id is unknown
// passes the vendor check alone, and the result does not claim the
// product's. (A code always names both or neither: setup's decoders refuse
// a vendor id without a product id.)
func TestOnboardingChecksNameWhatRan(t *testing.T) {
	t.Parallel()
	n, _ := onboardingNode(t, &MemoryStore{}, commissioningModeEnhanced)
	n.updateRecord(func(r *Record) { r.ProductID = nil })
	if vendor, product := n.identityIDs(n.Record()); vendor == nil || product != nil {
		t.Fatalf("the premise moved: vendor %v product %v", vendor, product)
	}
	c, err := n.SetOnboardingCode(qrOf(t, setup.Payload{VendorID: onbVendor, ProductID: onbProduct + 1, Discriminator: onbDiscriminator, Passcode: onbPasscode}))
	if err != nil || c.Checks != OnboardingCheckedVendor {
		t.Fatalf("a node without a product id: checks %b, err %v", c.Checks, err)
	}
}
