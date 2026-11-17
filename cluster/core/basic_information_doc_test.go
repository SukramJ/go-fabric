// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestValidateBasicInfoNamesTheSerialNumberCheck (review II,
// R2-P6-doc-1265): a SerialNumber in the VendorID-ProductID class form is
// reported as what it is, not as UniqueID equal to SerialNumber.
func TestValidateBasicInfoNamesTheSerialNumberCheck(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	validateBasicInfoAttributes(Config{VendorID: 0xFFF1, ProductID: 0x8001, SerialNumber: "FFF1-8001", UniqueID: "0123456789abcdef0123456789abcdef"}, log)
	if got := buf.String(); !strings.Contains(got, "class identifier") || strings.Contains(got, "UniqueID and SerialNumber are identical") {
		t.Errorf("log %q", got)
	}
}

// TestSoftwareVersionFromStringDomain (review II, R2-P6-api-1269): the
// encoding is monotonic inside 0-999 per component only; past it distinct
// versions share one number, as the doc states.
func TestSoftwareVersionFromStringDomain(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"0.999.0", "0.1000.0"}, {"1.0.999", "1.0.1000"}} {
		if a, b := SoftwareVersionFromString(pair[0]), SoftwareVersionFromString(pair[1]); a != b {
			t.Errorf("%s → %d, %s → %d: the doc says a component past 999 clamps", pair[0], a, pair[1], b)
		}
	}
	if SoftwareVersionFromString("0.998.0") >= SoftwareVersionFromString("0.999.0") {
		t.Error("not monotonic inside the domain")
	}
}
