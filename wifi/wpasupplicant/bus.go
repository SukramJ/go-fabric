// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// bus is the seam around the system bus: a method call on a
// fi.w1.wpa_supplicant1 object, the service's signals, and closing. The
// driver speaks only through it, so a test drives it over an in-process
// bus that plays wpa_supplicant.
type bus interface {
	// Call calls method (interface.member) on wpa_supplicant's object at
	// path and returns the reply's body.
	Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	// Signals delivers wpa_supplicant's interface signals, in order.
	Signals() <-chan *dbus.Signal
	// Close releases the bus.
	Close() error
}
