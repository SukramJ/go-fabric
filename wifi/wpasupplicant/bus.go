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
	// Signals delivers wpa_supplicant's interface signals, in order: those
	// Watch subscribed to.
	Signals() <-chan *dbus.Signal
	// Watch subscribes to the signals of the interface object at iface —
	// its ScanDone and the PropertiesChanged of its
	// fi.w1.wpa_supplicant1.Interface properties, the rules of
	// [watchRules] — and to nothing else of the service.
	Watch(ctx context.Context, iface dbus.ObjectPath) error
	// Close releases the bus.
	Close() error
}

// watchRules are the match rules ([bus.Watch]) of the one interface the
// driver follows: its ScanDone, and the PropertiesChanged of its
// fi.w1.wpa_supplicant1.Interface properties (arg0, the interface name).
// A rule on the whole /fi/w1/wpa_supplicant1 namespace would carry every
// BSS's and every network's property change over the bus connection into
// the run loop, only to be dropped by path there — in bulk during a scan
// in a dense environment, the moment the hand-over's drain by length
// relies on the signal channel not overflowing ([Driver.run]; review II,
// R2-P6-bound-2020).
func watchRules(iface dbus.ObjectPath) []string {
	return []string{
		"type='signal',sender='" + service + "',interface='" + ifaceInterface + "',member='ScanDone',path='" + string(iface) + "'",
		"type='signal',sender='" + service + "',interface='" + ifaceProperties + "',member='PropertiesChanged',path='" + string(iface) + "',arg0='" + ifaceInterface + "'",
	}
}
