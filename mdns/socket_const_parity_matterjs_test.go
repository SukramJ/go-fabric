// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import "testing"

// TestMdnsSocketConstantsParityMatterJS mirrors
// packages/general/test/net/dns-sd/MdnsSocketTest.ts:741-745 at 862ad75f
// ("has correct mDNS broadcast addresses"): 224.0.0.251 and ff02::fb, port
// 5353.
func TestMdnsSocketConstantsParityMatterJS(t *testing.T) {
	t.Parallel()
	if mdnsGroup4.String() != "224.0.0.251:5353" || mdnsGroup6.String() != "[ff02::fb]:5353" {
		t.Fatalf("groups %s and %s", mdnsGroup4, mdnsGroup6)
	}
}
