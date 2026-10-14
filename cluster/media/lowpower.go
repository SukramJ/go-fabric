// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
)

// LowPower is the LowPower host port: Sleep (low-power.element.ts:19,
// status only) puts the device into its low-power mode.
type LowPower interface {
	Sleep(ctx context.Context) error
}

// LowPowerConfig carries the construction parameters of a LowPower server.
type LowPowerConfig struct {
	// Port carries out Sleep; required.
	Port LowPower
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// LowPowerServer is a LowPower (0x0508) server.
//
// Mirrors matter.js packages/node/src/behaviors/low-power/
// LowPowerServer.ts:16-18: `sleep()` emits the server's
// `enterLowPowerMode` observable, the hook through which the application
// enters the mode, and answers SUCCESS. Here the port is that hook; an
// error it returns is the command's status.
type LowPowerServer struct {
	*spec.Server
}

// NewLowPower builds a LowPower server over cfg.Port.
func NewLowPower(cfg LowPowerConfig) (*LowPowerServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: LowPower", ErrNilPort)
	}
	srv, err := spec.NewServer(lowpowerdef.Definition, spec.Options{}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: LowPower: %w", err)
	}
	port := cfg.Port
	handle(srv, lowpowerdef.CmdSleep, func(ctx context.Context, _ lowpowerdef.SleepRequest) (any, error) {
		return nil, port.Sleep(ctx)
	})
	return &LowPowerServer{Server: srv}, nil
}
