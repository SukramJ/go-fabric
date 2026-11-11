// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !linux

package wpasupplicant

import (
	"context"
	"errors"
	"fmt"
)

// New fails off Linux with an error wrapping errors.ErrUnsupported:
// wpa_supplicant's D-Bus interface is Linux's.
func New(context.Context, Options) (*Driver, error) {
	return nil, fmt.Errorf("wpasupplicant: %w off Linux", errors.ErrUnsupported)
}
