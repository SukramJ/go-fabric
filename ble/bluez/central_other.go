// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !linux

package bluez

import "github.com/SukramJ/go-fabric/controller/ble"

// NewCentral returns [ErrUnsupported]: BlueZ is Linux's (ADR 0024
// decision 6 keeps macOS and Windows out while they need cgo).
func NewCentral(string) (ble.Central, error) { return nil, ErrUnsupported }
