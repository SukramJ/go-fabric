// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
)

// The secrets of a registration and a window stay out of every rendering
// a host does not ask for: fmt under every verb, a log record of either
// slog handler and encoding/json render them as "redacted". The fields
// stay what they are — a host that shows a window's codes, or keeps a
// registration's key, reads the field (review II, R2-P6-api-2312; a
// SecretCode and an InventoryEntry are redacted the same way).

// redactedOr is redacted for a secret that is present, "" for none.
func redactedOr(present bool) string {
	if present {
		return redacted
	}
	return ""
}

// String renders the registration with its keys redacted.
func (r ICDRegistration) String() string {
	pending := "none"
	if len(r.PendingKey) > 0 {
		pending = redacted
	}
	return "ICDRegistration(key " + redactedOr(len(r.Key) > 0) + ", pending key " + pending +
		", counter start " + strconv.FormatUint(uint64(r.CounterStart), 10) +
		", last offset " + strconv.FormatUint(uint64(r.LastOffset), 10) +
		", monitored subject " + strconv.FormatUint(r.MonitoredSubject, 10) +
		", client type " + strconv.FormatUint(uint64(r.ClientType), 10) + ")"
}

// Format renders the registration as String under every fmt verb, so no
// verb (%x, %+v, %#v) reaches the key bytes.
func (r ICDRegistration) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, r.String()) }

// LogValue renders the registration in a log record, its keys redacted.
func (r ICDRegistration) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("key", redactedOr(len(r.Key) > 0)),
		slog.String("pending_key", redactedOr(len(r.PendingKey) > 0)),
		slog.Uint64("counter_start", uint64(r.CounterStart)),
		slog.Uint64("last_offset", uint64(r.LastOffset)),
		slog.Uint64("monitored_subject", r.MonitoredSubject),
		slog.Uint64("client_type", uint64(r.ClientType)),
	)
}

// MarshalJSON renders the registration for encoding/json, its keys
// redacted. There is no UnmarshalJSON: a redacted key does not come back.
func (r ICDRegistration) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Key              string `json:"key,omitempty"`
		PendingKey       string `json:"pendingKey,omitempty"`
		CounterStart     uint32 `json:"counterStart"`
		LastOffset       uint32 `json:"lastOffset"`
		MonitoredSubject uint64 `json:"monitoredSubject"`
		ClientType       uint8  `json:"clientType"`
	}{
		redactedOr(len(r.Key) > 0), redactedOr(len(r.PendingKey) > 0),
		r.CounterStart, r.LastOffset, r.MonitoredSubject, uint8(r.ClientType),
	})
}

// String renders the window with its codes and passcode redacted.
func (w CommissioningWindow) String() string {
	return "CommissioningWindow(manual code " + redactedOr(w.ManualCode != "") +
		", QR code " + redactedOr(w.QRCode != "") +
		", passcode " + redactedOr(w.Passcode != 0) +
		", discriminator " + strconv.FormatUint(uint64(w.Discriminator), 10) +
		", vendor " + strconv.FormatUint(uint64(w.VendorID), 10) +
		", product " + strconv.FormatUint(uint64(w.ProductID), 10) +
		", timeout " + w.Timeout.String() + ")"
}

// Format renders the window as String under every fmt verb.
func (w CommissioningWindow) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, w.String()) }

// LogValue renders the window in a log record, its codes and passcode
// redacted.
func (w CommissioningWindow) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("manual_code", redactedOr(w.ManualCode != "")),
		slog.String("qr_code", redactedOr(w.QRCode != "")),
		slog.String("passcode", redactedOr(w.Passcode != 0)),
		slog.Uint64("discriminator", uint64(w.Discriminator)),
		slog.Uint64("vendor_id", uint64(w.VendorID)),
		slog.Uint64("product_id", uint64(w.ProductID)),
		slog.Duration("timeout", w.Timeout),
	)
}

// MarshalJSON renders the window for encoding/json, its codes and passcode
// redacted; a host that sends a window's codes to a UI copies the fields.
func (w CommissioningWindow) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ManualCode    string `json:"manualCode,omitempty"`
		QRCode        string `json:"qrCode,omitempty"`
		Passcode      string `json:"passcode,omitempty"`
		Discriminator uint16 `json:"discriminator"`
		VendorID      uint16 `json:"vendorId"`
		ProductID     uint16 `json:"productId"`
		TimeoutMs     int64  `json:"timeoutMs"`
	}{
		redactedOr(w.ManualCode != ""), redactedOr(w.QRCode != ""), redactedOr(w.Passcode != 0),
		w.Discriminator, w.VendorID, w.ProductID, w.Timeout.Milliseconds(),
	})
}
