// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// connectedhomeip's commissioner DUT test vectors and CD certificates at
// 6170af84 (credentials/development/commissioner_dut/<name>/
// test_case_vector.json, attestation/TestCredentials-FFF1-8001.json,
// cd-certs/*.der), as testdata/chip-commissioner-dut/vectors.json holds
// them, with the vector sets matter.js db021da0 names in
// packages/protocol/test/certificate/ChipAttestationProfileVectors.ts and
// ChipCdTestVectors.ts.

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// chipVector is one test_case_vector.json.
type chipVector struct {
	Description              string `json:"description"`
	BasicInfoPID             uint16 `json:"basic_info_pid"`
	DACCert                  string `json:"dac_cert"`
	PAICert                  string `json:"pai_cert"`
	DACPrivateKey            string `json:"dac_private_key"`
	CertificationDeclaration string `json:"certification_declaration"`
}

// chipVectorFile is the testdata file.
type chipVectorFile struct {
	Groups  map[string][]string   `json:"groups"`
	Vectors map[string]chipVector `json:"vectors"`
	CDCerts map[string]string     `json:"cdCerts"`
}

var (
	chipVectorsOnce sync.Once
	chipVectorsData chipVectorFile
	chipVectorsErr  error
)

// chipVectors loads the file once.
func chipVectors(t *testing.T) *chipVectorFile {
	t.Helper()
	chipVectorsOnce.Do(func() {
		b, err := os.ReadFile(filepath.Join("testdata", "chip-commissioner-dut", "vectors.json"))
		if err != nil {
			chipVectorsErr = err
			return
		}
		chipVectorsErr = json.Unmarshal(b, &chipVectorsData)
	})
	if chipVectorsErr != nil {
		t.Fatal(chipVectorsErr)
	}
	return &chipVectorsData
}

// chipVectorNamed is the vector name.
func chipVectorNamed(t *testing.T, name string) chipVector {
	t.Helper()
	v, ok := chipVectors(t).Vectors[name]
	if !ok {
		t.Fatalf("no test vector %s", name)
	}
	return v
}

// chipCDCert is the DER of chip's cd-certs/<name>.
func chipCDCert(t *testing.T, name string) []byte {
	t.Helper()
	h, ok := chipVectors(t).CDCerts[name]
	if !ok {
		t.Fatalf("no CD certificate %s", name)
	}
	return unhex(t, h)
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// encodeDER re-encodes a decoded node tree, so an edited node gets correct
// container lengths — the reencode of AttestationCertificateProfileTest.ts
// :79-89 at db021da0.
func encodeDER(n derNode) []byte {
	content := n.bytes
	if n.tag&0x20 != 0 {
		content = nil
		for _, c := range n.elements {
			content = append(content, encodeDER(c)...)
		}
	}
	return derTLV(n.tag, content...)
}

// derTLV is one DER element of identifier tag over the concatenated
// content.
func derTLV(tag byte, content ...byte) []byte {
	out := []byte{tag}
	switch l := len(content); {
	case l < 0x80:
		out = append(out, byte(l))
	case l < 0x100:
		out = append(out, 0x81, byte(l))
	default:
		out = append(out, 0x82, byte(l>>8), byte(l))
	}
	return append(out, content...)
}

// editTBS lets edit change the TBSCertificate elements of a certificate;
// the signature no longer matches (:91-96 at db021da0).
func editTBS(t *testing.T, der []byte, edit func(tbs *[]derNode)) []byte {
	t.Helper()
	cert, err := decodeDER(der)
	if err != nil {
		t.Fatal(err)
	}
	edit(&cert.elements[0].elements)
	return encodeDER(cert)
}

// editExtensions lets edit change the extension list (:98-106 at
// db021da0).
func editExtensions(t *testing.T, der []byte, edit func(exts *[]derNode)) []byte {
	t.Helper()
	return editTBS(t, der, func(tbs *[]derNode) {
		for i := range *tbs {
			if (*tbs)[i].tag == 0xa3 {
				edit(&(*tbs)[i].elements[0].elements)
				return
			}
		}
		t.Fatal("certificate has no extensions")
	})
}

// extensionNamed is the index of the extension whose OID content is oidHex
// (:108-113 at db021da0).
func extensionNamed(t *testing.T, exts []derNode, oidHex string) int {
	t.Helper()
	for i, e := range exts {
		if len(e.elements) > 0 && hex.EncodeToString(e.elements[0].bytes) == oidHex {
			return i
		}
	}
	t.Fatalf("certificate has no extension %s", oidHex)
	return -1
}

// withExtensionValue replaces the value of an extension with DER given as
// hex (:119-124 at db021da0).
func withExtensionValue(t *testing.T, der []byte, oidHex, valueHex string) []byte {
	t.Helper()
	return editExtensions(t, der, func(exts *[]derNode) {
		e := &(*exts)[extensionNamed(t, *exts, oidHex)]
		e.elements[len(e.elements)-1].bytes = unhex(t, valueHex)
	})
}
