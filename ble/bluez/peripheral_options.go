// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bluez

import "log/slog"

// PeripheralOptions configures [NewPeripheralWithOptions]. The zero value
// is [NewPeripheral] on hci0.
type PeripheralOptions struct {
	// Adapter is the BlueZ adapter ("" is hci0).
	Adapter string
	// Logger receives the peripheral's diagnostics; nil is slog.Default().
	// A registration the peripheral repeats on its own — after BlueZ
	// released the advertisement, after the adapter came back — that BlueZ
	// refuses is logged at Warn on its first failure and on each change of
	// the error, and at Info once it held; the adapter's Powered going
	// false is logged at Warn, its return to true at Info.
	Logger *slog.Logger
}
