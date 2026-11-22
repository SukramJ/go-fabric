// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel_test

import (
	"testing"

	"thingfabric.dev/go-fabric/secure/channel"
)

// TestPrivacyKeystreamRefusesALengthItsCounterCannotCover holds
// R2-P6-api-0115 (c): the keystream's block counter is 16 bits, so it
// covers 65535 blocks of 16 bytes. A longer keystream is refused rather
// than allocated and repeated after the counter wraps; the longest
// length is the control.
func TestPrivacyKeystreamRefusesALengthItsCounterCannotCover(t *testing.T) {
	t.Parallel()
	key := make([]byte, channel.PrivacyKeySize)
	mic := make([]byte, 16)
	const most = 0xFFFF * channel.PrivacyKeySize
	if _, err := channel.PrivacyKeystream(key, 1, mic, most); err != nil {
		t.Fatalf("control: %d bytes: %v", most, err)
	}
	for _, n := range []int{most + 1, most + channel.PrivacyKeySize + 1} {
		if _, err := channel.PrivacyKeystream(key, 1, mic, n); err == nil {
			t.Errorf("%d bytes: accepted, want a refusal", n)
		}
	}
}
