// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"testing"
)

// TestDumpAppleNOC decodes an Apple NOC captured during a live pair attempt
// and checks it against the Apple RCAC public key of the same capture: the
// signature verifies over the TBS's X.509 DER form, and the DN carries the
// identifiers the TLV encodes. The name is historical — the test used to
// print the certificate and assert nothing beyond decoding it (review II,
// R2-P5-tassert-06).
func TestDumpAppleNOC(t *testing.T) {
	t.Parallel()
	const appleNOCHex = "1530010101240201370326140ADC880726156903C46718260433FD9031240500370626156903C4672611389D8E48182407012408013009410494463D0A3EE3C4F263C725D9BDC3BE89C030568E52DCC56A1155AEA605510DAA2BEE9C83264F2DE9968B22E2ABC68A6DE32542D46E3301977C2C49B7468082F0370A350128011824020136030402040118300414A80B5BF3B11A3C6FBB7C9D3BB6A88C561B47ACCF300514E73FCB911EE49D8EF42A3908B2B9C9292A54282318300B40F955EDDD229218CB68247F7E7D2D309EA82667867523DA08E5ED79992A6FE96A49880FAC9CC99F0705F16F8928419733F1FF329B5BFDF6602C0B14D1EEF7DDF718"
	const appleRCACPubKeyHex = "041808BD63CB387CD0A4D72E56B0A2CC1AF6EE6C8F7B5CDDB5B9BBEFFD797A11C7834E1939871FBAA890D078241886EDF97143B75AB827C60D449FDB5EBCCBE44A"

	raw, err := hex.DecodeString(appleNOCHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	c, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode NOC: %v", err)
	}
	if !c.IsNOC() {
		t.Fatalf("IsRoot=%v IsICA=%v IsNOC=%v, want a NOC", c.IsRoot(), c.IsICA(), c.IsNOC())
	}

	// The DN values as the TLV spells them: issuer 26 14 0ADC8807 (rcac-id,
	// tag 20) and 26 15 6903C467 (fabric-id, tag 21); subject 26 15 6903C467
	// and 26 11 389D8E48 (node-id, tag 17), each a little-endian uint32.
	if !c.Issuer.HasRCACID || c.Issuer.MatterRCACID != 0x0788DC0A {
		t.Errorf("issuer rcac-id = %v/%#x, want 0x0788DC0A", c.Issuer.HasRCACID, c.Issuer.MatterRCACID)
	}
	if !c.Issuer.HasFabricID || c.Issuer.MatterFabricID != 0x67C40369 {
		t.Errorf("issuer fabric-id = %v/%#x, want 0x67C40369", c.Issuer.HasFabricID, c.Issuer.MatterFabricID)
	}
	if !c.Subject.HasFabricID || c.Subject.MatterFabricID != 0x67C40369 {
		t.Errorf("subject fabric-id = %v/%#x, want 0x67C40369", c.Subject.HasFabricID, c.Subject.MatterFabricID)
	}
	if !c.Subject.HasNodeID || c.Subject.MatterNodeID != 0x488E9D38 {
		t.Errorf("subject node-id = %v/%#x, want 0x488E9D38", c.Subject.HasNodeID, c.Subject.MatterNodeID)
	}

	pub, err := hex.DecodeString(appleRCACPubKeyHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	rcac, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pub)
	if err != nil {
		t.Fatalf("Apple RCAC public key: %v", err)
	}
	if err := verifySignature(c, rcac); err != nil {
		t.Fatalf("the Apple NOC does not verify against its RCAC: %v", err)
	}
}
