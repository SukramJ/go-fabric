// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
)

// renderings is every way v, and ptr pointing at it, reach a log or a
// serialiser unasked.
func renderings(t *testing.T, v, ptr any) []string {
	t.Helper()
	out := []string{}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%X", "%q", "%d"} {
		out = append(out, fmt.Sprintf(verb, v), fmt.Sprintf(verb, ptr))
	}
	for _, h := range []func(*bytes.Buffer) slog.Handler{
		func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var b bytes.Buffer
		slog.New(h(&b)).Info("x", slog.Any("v", v))
		out = append(out, b.String())
	}
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return append(out, string(j))
}

// TestICDRegistrationIsRedacted (review II, R2-P6-api-2312): the Check-In
// keys of a registration reach no fmt verb, slog record or JSON document.
func TestICDRegistrationIsRedacted(t *testing.T) {
	t.Parallel()
	key, pending := bytes.Repeat([]byte{0xA7}, 16), bytes.Repeat([]byte{0x5C}, 16)
	reg := client.ICDRegistration{Key: key, PendingKey: pending, CounterStart: 7, MonitoredSubject: 0xabcd}
	for _, s := range renderings(t, reg, &reg) {
		for _, k := range [][]byte{key, pending} {
			for _, form := range []string{hex.EncodeToString(k), strings.ToUpper(hex.EncodeToString(k)), string(k), fmt.Sprintf("%v", k)} {
				if strings.Contains(s, form) {
					t.Fatalf("a rendering carries a key: %s", s)
				}
			}
		}
		if !strings.Contains(s, "redacted") {
			t.Fatalf("a rendering does not say redacted: %s", s)
		}
	}
	if s := reg.String(); !strings.Contains(s, "43981") {
		t.Fatalf("String %q drops the monitored subject", s)
	}
}

// TestCommissioningWindowIsRedacted (review II, R2-P6-api-2312): a
// window's pairing codes and passcode reach no fmt verb, slog record or
// JSON document; the fields still hold them.
func TestCommissioningWindowIsRedacted(t *testing.T) {
	t.Parallel()
	w := client.CommissioningWindow{ManualCode: "34970112332", QRCode: "MT:Y.K9042C00KA0648G00", Passcode: 20202021, Discriminator: 3840, VendorID: 0xFFF1, Timeout: 3 * time.Minute}
	for _, s := range renderings(t, w, &w) {
		for _, secret := range []string{w.ManualCode, w.QRCode, strconv.FormatUint(uint64(w.Passcode), 10), strconv.FormatUint(uint64(w.Passcode), 16)} {
			if strings.Contains(s, secret) {
				t.Fatalf("a rendering carries a secret %q: %s", secret, s)
			}
		}
	}
	if w.ManualCode != "34970112332" || w.Passcode != 20202021 {
		t.Fatal("the fields lost their values")
	}
}
