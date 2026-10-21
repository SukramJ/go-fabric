// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
)

// LowPower is the LowPower host port (the generated [lowpowerdef.LowPower],
// the declaration's LowPower): Sleep (low-power.element.ts:19, status
// only) puts the device into its low-power mode.
type LowPower = lowpowerdef.LowPower

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
	h := handlers{}
	port := cfg.Port
	handle(h, lowpowerdef.CmdSleep, func(ctx context.Context, _ lowpowerdef.SleepRequest) (any, error) {
		return nil, port.Sleep(ctx)
	})
	srv, err := newServer(lowpowerdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: LowPower: %w", err)
	}
	return &LowPowerServer{Server: srv}, nil
}
