// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestDecodeRefusesADNThatIsNotAList re-encodes a certificate's Issuer
// and then its Subject as a TLV structure instead of the list a Matter
// DN is, as TC-JFADMIN-1.2 step 16 does to an ICAC
// (`TC_JFADMIN_1_2.py:318-323` at 6170af84), and expects ErrMalformed:
// chip's ChipDN::DecodeFromTLV requires kTLVType_List (CHIPCert.cpp:782),
// matter.js's DN schema is a list (TlvObject.ts:205-207 refuses another
// type). The decoder accepted the structure, so an ICAC with such a
// Subject reached the public-key check instead of InvalidICAC.
func TestDecodeRefusesADNThatIsNotAList(t *testing.T) {
	t.Parallel()
	// The RCAC of the first chain in certificates_parity_matterjs_test.go.
	raw, err := hex.DecodeString("153001010024020137032414001826048012542826058015203b37062414001824070124080130094104d89eb7e3f3226d0918f4b85832457bb9981bca7aaef58c18fb5ec07525e472b2bd1617fb75ee41bd388f94ae6a6070efc896777516a5c54aff74ec0804cdde9d370a3501290118240260300414e766069362d7e35b79687161644d222bdde93a68300514e766069362d7e35b79687161644d222bdde93a6818300b404e8fb06526f0332b3e928166864a6d29cade53fb5b8918a6d134d0994bf1ae6dce6762dcba99e80e96249d2f1ccedb336b26990f935dba5a0b9e5b4c9e5d1d8f18")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(raw); err != nil {
		t.Fatalf("the unchanged certificate: %v", err)
	}
	list, structure := byte(0x20|tlv.TypeList), byte(0x20|tlv.TypeStructure) // context-tagged openers
	for _, c := range []struct {
		name string
		tag  byte
	}{{"issuer", 3}, {"subject", 6}} {
		opener := []byte{list, c.tag, 0x24, 0x14} // the list, then its first field (uint8 tag 0x14)
		if bytes.Count(raw, opener) != 1 {
			t.Fatalf("%s opener occurs %d times", c.name, bytes.Count(raw, opener))
		}
		patched := bytes.Replace(raw, opener, []byte{structure, c.tag, 0x24, 0x14}, 1)
		_, err := Decode(patched)
		if !errors.Is(err, ErrMalformed) {
			t.Errorf("%s as a structure: Decode = %v, want ErrMalformed", c.name, err)
		}
	}
}
