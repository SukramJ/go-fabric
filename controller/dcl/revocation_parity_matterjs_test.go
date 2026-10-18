// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclCertificateServiceRevocationTest.ts at
// 862ad75f: revocation information installed outside the DCL, the offline
// store, and the revocation-set reader.

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// DclCertificateServiceRevocationTest.ts:18-23.
const (
	dclRevPAIAKID       = "63540E47F64B1C38D13884A462D16C195D8FFB3C"
	dclRevPAIName       = "MD0xJTAjBgNVBAMMHE1hdHRlciBEZXYgUEFJIDB4RkZGMSBubyBQSUQxFDASBgorBgEEAYKifAIBDARGRkYx"
	dclRevRevokedSerial = "19367D978EAC533A"
	dclRevOtherSerial   = "0AB042494323FE54"
	dclRevOtherName     = "MDAxGDAWBgNVBAMMD01hdHRlciBUZXN0IFBBQTEUMBIGCisGAQQBgqJ8AgEMBEZGRjE="
)

// dclRevNameAsDerHex is nameAsDerHex (:27-29).
func dclRevNameAsDerHex(t *testing.T, b64 string) string {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return hexUpper(der)
}

func dclRevBytes(t *testing.T, h string) []byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// dclRevService is service() (:48-53) over a ledger with no roots that
// knows nothing about any authority (fakeDCL answers 404, dclKnowsNothing
// :56-62). The constructor's revocations option has no Go counterpart, so
// a case that passes it installs the set right after opening.
func dclRevService(t *testing.T, set []RevocationSetEntry) (*CertificateStore, *fakeDCL) {
	t.Helper()
	f := newFakeDCL(t)
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": []any{}}})
	s := dclRevOpen(t, CertificateOptions{Client: f.client(), UpdateInterval: -1})
	if set != nil {
		if err := s.InstallRevocations(set); err != nil {
			t.Fatal(err)
		}
	}
	return s, f
}

func dclRevOpen(t *testing.T, opts CertificateOptions) *CertificateStore {
	t.Helper()
	opts.Clock = clocktest.New(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	s, err := OpenCertificateStore(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// dclRevAsked reports whether the ledger saw a request whose URI contains part.
func dclRevAsked(f *fakeDCL, part string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for uri := range f.hits {
		if strings.Contains(uri, part) {
			return true
		}
	}
	return false
}

func dclRevIsRevoked(t *testing.T, s *CertificateStore, akid string, serial []byte, issuerDN string) bool {
	t.Helper()
	revoked, err := s.IsRevoked(dclRevBytes(t, akid), serial, issuerDN)
	if err != nil {
		t.Fatal(err)
	}
	return revoked
}

func dclRevWantRefusal(t *testing.T, err error, part string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidRevocationSet) || !strings.Contains(err.Error(), part) {
		t.Fatalf("got %v, want a refusal naming %q", err, part)
	}
}

// TestDclCertificateServiceRevocationOutsideTheDCL is the describe
// "DclCertificateService revocation outside the DCL" (:31-257).
func TestDclCertificateServiceRevocationOutsideTheDCL(t *testing.T) {
	revoked := func(t *testing.T) []byte { t.Helper(); return dclRevBytes(t, dclRevRevokedSerial) }
	other := func(t *testing.T) []byte { t.Helper(); return dclRevBytes(t, dclRevOtherSerial) }

	t.Run("reports a serial an installed set names as revoked", func(t *testing.T) {
		s, _ := dclRevService(t, nil)
		if err := s.InstallRevocations([]RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
		}); err != nil {
			t.Fatal(err)
		}
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevPAIName)) {
			t.Fatal("installed serial not revoked")
		}
	})

	t.Run("installs what the constructor was given", func(t *testing.T) {
		f := newFakeDCL(t)
		f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": []any{}}})
		s := dclRevOpen(t, CertificateOptions{
			Client: f.client(), UpdateInterval: -1,
			Revocations: []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}}},
		}) // DclCertificateServiceRevocationTest.ts:77-86
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("constructor-given serial not revoked")
		}
	})

	t.Run("passes a serial the set does not name to the DCL, which here knows nothing", func(t *testing.T) {
		s, f := dclRevService(t, []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}}})
		if dclRevIsRevoked(t, s, dclRevPAIAKID, other(t), "") {
			t.Fatal("other serial revoked")
		}
		if !dclRevAsked(f, "/revocation-points/"+dclRevPAIAKID) {
			t.Fatal("the DCL was not asked")
		}
	})

	t.Run("answers a serial the set does name without asking the DCL", func(t *testing.T) {
		s, f := dclRevService(t, []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}}})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("installed serial not revoked")
		}
		if dclRevAsked(f, "/revocation-points/") {
			t.Fatal("the DCL was asked")
		}
	})

	t.Run("holds an authority's entries apart by the issuer name they carry", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
		})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevPAIName)) {
			t.Fatal("not revoked under its issuer")
		}
		if dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevOtherName)) {
			t.Fatal("revoked under another issuer")
		}
	})

	t.Run("keeps one authority's two issuers apart, each with its own serials", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevOtherName, RevokedSerialNumbers: []string{dclRevOtherSerial}},
		})
		pai, oth := dclRevNameAsDerHex(t, dclRevPAIName), dclRevNameAsDerHex(t, dclRevOtherName)
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), pai) || !dclRevIsRevoked(t, s, dclRevPAIAKID, other(t), oth) {
			t.Fatal("an issuer's own serial is not revoked")
		}
		if dclRevIsRevoked(t, s, dclRevPAIAKID, other(t), pai) || dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), oth) {
			t.Fatal("a serial is revoked under the other issuer")
		}
	})

	t.Run("installs the same set twice without stacking it up", func(t *testing.T) {
		set := []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}}}
		s, _ := dclRevService(t, set)
		for range 2 {
			if err := s.InstallRevocations(set); err != nil {
				t.Fatal(err)
			}
		}
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevPAIName)) {
			t.Fatal("not revoked")
		}
		if n := len(s.InstalledRevocations()[dclRevPAIAKID]); n != 1 {
			t.Fatalf("%d installed entries, want 1", n)
		}
		// A copy each time (:154-159): changing what a reader holds changes
		// neither the next copy nor what the store checks against.
		first := s.InstalledRevocations()[dclRevPAIAKID][0].Serials
		first[0] = "00"
		second := s.InstalledRevocations()[dclRevPAIAKID][0].Serials
		if !reflect.DeepEqual(second, []string{dclRevRevokedSerial}) {
			t.Fatalf("second copy %v", second)
		}
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("a reader's change reached the store")
		}
	})

	t.Run("matches a serial whose top bit is set, which a certificate states with a leading zero", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{"E1234567"}}})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, dclRevBytes(t, "00E1234567"), "") {
			t.Fatal("zero-padded serial not revoked")
		}
	})

	t.Run("matches on the key identifier alone where an entry states an empty issuer name", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: "", RevokedSerialNumbers: []string{dclRevRevokedSerial}}})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevOtherName)) {
			t.Fatal("not revoked")
		}
	})

	t.Run("installs nothing when an entry hands in a serial that is not hex", func(t *testing.T) {
		s, _ := dclRevService(t, nil)
		err := s.InstallRevocations([]RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
			{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{"nonsense"}},
		})
		dclRevWantRefusal(t, err, "revokedSerialNumbers[0]")
		if dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("half the set installed")
		}
	})

	t.Run("installs nothing when an entry states an issuer name that is not base64", func(t *testing.T) {
		s, _ := dclRevService(t, nil)
		err := s.InstallRevocations([]RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: "not base64!", RevokedSerialNumbers: []string{dclRevOtherSerial}},
		})
		dclRevWantRefusal(t, err, "entry 1")
		if dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevPAIName)) {
			t.Fatal("half the set installed")
		}
	})

	t.Run("matches on the key identifier alone where an entry names no issuer", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}}})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), dclRevNameAsDerHex(t, dclRevOtherName)) {
			t.Fatal("not revoked")
		}
	})

	t.Run("matches an entry that names an issuer even where the caller states none", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{
			{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}},
		})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("not revoked")
		}
	})

	t.Run("reads a serial stated with separators, and one stated as bytes", func(t *testing.T) {
		s, _ := dclRevService(t, []RevocationSetEntry{{
			IssuerSubjectKeyID:   "63:54:0E:47:F6:4B:1C:38:D1:38:84:A4:62:D1:6C:19:5D:8F:FB:3C",
			RevokedSerialNumbers: []string{"19:36:7d:97:8e:ac:53:3a"},
		}})
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, revoked(t), "") {
			t.Fatal("not revoked")
		}
	})
}

// TestDclCertificateServiceRevocationOffline is the describe "offline"
// (:259-299). Go's offline store is one without a Client (CertificateOptions
// .Client nil), so it has no request path at all; the cases assert what it
// answers.
func TestDclCertificateServiceRevocationOffline(t *testing.T) {
	t.Run("asks the DCL nothing, neither for certificates nor for a CRL", func(t *testing.T) {
		s := dclRevOpen(t, CertificateOptions{UpdateInterval: -1})
		if err := s.InstallRevocations([]RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, RevokedSerialNumbers: []string{dclRevRevokedSerial}}}); err != nil {
			t.Fatal(err)
		}
		if !dclRevIsRevoked(t, s, dclRevPAIAKID, dclRevBytes(t, dclRevRevokedSerial), "") {
			t.Fatal("installed serial not revoked")
		}
		if dclRevIsRevoked(t, s, dclRevPAIAKID, dclRevBytes(t, dclRevOtherSerial), "") {
			t.Fatal("other serial revoked")
		}
	})

	t.Run("answers a CD signer it does not hold without asking the DCL", func(t *testing.T) {
		s := dclRevOpen(t, CertificateOptions{UpdateInterval: -1})
		if key, ok := s.CDSigner(dclRevBytes(t, "62FA823359ACFAA9963E1CFA140ADDF504F37160")); ok || key != nil {
			t.Fatal("an unknown CD signer was answered")
		}
	})

	t.Run("still updates nothing when asked to", func(t *testing.T) {
		s := dclRevOpen(t, CertificateOptions{UpdateInterval: -1})
		if !s.Update(context.Background(), true) {
			t.Fatal("offline update reported a failure")
		}
		if n := len(s.Certificates()); n != 0 {
			t.Fatalf("%d certificates after an offline update", n)
		}
	})
}

// TestDclCertificateServiceRevocationParseRevocationSet is the describe
// "parseRevocationSet()" (:301-419). ImplementationError is
// ErrInvalidRevocationSet.
func TestDclCertificateServiceRevocationParseRevocationSet(t *testing.T) {
	t.Run("reads the set as the revocation-set tool writes it", func(t *testing.T) {
		got, err := ParseRevocationSet([]byte(`[{"type":"revocation_set","issuer_subject_key_id":"` + dclRevPAIAKID +
			`","issuer_name":"` + dclRevPAIName + `","revoked_serial_numbers":["` + dclRevRevokedSerial + `"],"crl_signer_cert":"MIIB..."}]`))
		want := []RevocationSetEntry{{IssuerSubjectKeyID: dclRevPAIAKID, IssuerName: dclRevPAIName, RevokedSerialNumbers: []string{dclRevRevokedSerial}}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v (%v)", got, err)
		}
	})

	t.Run("reads a set that revokes nothing", func(t *testing.T) {
		got, err := ParseRevocationSet([]byte("[]"))
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %#v (%v)", got, err)
		}
	})

	refusals := []struct{ name, json, part string }{
		{"refuses text that is not JSON", "not json", ""},
		{"refuses a set that is not a list of entries", `{"issuer_subject_key_id":"AB"}`, "must be an array"},
		{"refuses an entry that names no issuer key identifier", `[{"revoked_serial_numbers":["AB"]}]`, "issuer_subject_key_id"},
		{"refuses an entry that is not an object at all", "[5]", "entry 0 is not an object"},
		{"refuses an entry whose key identifier is not hex, which would match nothing", `[{"issuer_subject_key_id":"not hex","revoked_serial_numbers":["AB"]}]`, "issuer_subject_key_id"},
		{"refuses a serial that is not hex, which would match nothing", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","revoked_serial_numbers":["zz"]}]`, "revoked_serial_numbers[0]"},
		{"refuses hex that does not divide into bytes", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","revoked_serial_numbers":["ABC"]}]`, "even number of hex digits"},
		{"refuses an issuer name that is not base64", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","issuer_name":"not base64!","revoked_serial_numbers":["AB"]}]`, "issuer_name"},
		{"refuses an entry stating a type other than a revocation set", `[{"type":"certificate","issuer_subject_key_id":"` + dclRevPAIAKID + `","revoked_serial_numbers":["AB"]}]`, "revocation_set"},
		{"refuses a list where an entry should be", `[["not","an","entry"]]`, "entry 0 is not an object"},
		{"refuses an entry whose serial numbers are not a list", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","revoked_serial_numbers":"AB"}]`, "revoked_serial_numbers"},
		{"refuses an entry whose issuer name is not a string", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","issuer_name":123,"revoked_serial_numbers":["AB"]}]`, "issuer_name"},
		{"refuses an entry whose serial numbers are not a list of strings", `[{"issuer_subject_key_id":"` + dclRevPAIAKID + `","revoked_serial_numbers":[7]}]`, "revoked_serial_numbers"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRevocationSet([]byte(tc.json))
			if got != nil {
				t.Fatalf("read %+v", got)
			}
			dclRevWantRefusal(t, err, tc.part)
		})
	}
}
