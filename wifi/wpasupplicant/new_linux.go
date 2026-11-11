// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package wpasupplicant

import "context"

// New connects to wpa_supplicant on the system bus and brings
// opts.Interface up as the driver's interface: GetInterface, or
// CreateInterface when wpa_supplicant does not manage it yet. It fails
// when the system bus or wpa_supplicant's D-Bus service is unreachable or
// the interface cannot be had. The process needs the system bus's
// permission to talk to fi.w1.wpa_supplicant1.
func New(ctx context.Context, opts Options) (*Driver, error) {
	b, err := dialSystemBus()
	if err != nil {
		return nil, err
	}
	d, err := newDriver(ctx, b, opts)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	return d, nil
}
