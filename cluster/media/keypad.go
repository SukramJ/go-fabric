// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	keypaddef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
)

// Keypad is the KeypadInput host port: SendKey with the CEC key code
// (keypad-input.element.ts:29-32), answered by SendKeyResponse with its
// Status (:33-36) — UnsupportedKey or InvalidKeyInCurrentState for a key
// the device does not take.
type Keypad interface {
	SendKey(ctx context.Context, req keypaddef.SendKeyRequest) (keypaddef.SendKeyResponse, error)
}

// KeypadConfig carries the construction parameters of a KeypadInput
// server.
type KeypadConfig struct {
	// Port carries out SendKey; required.
	Port Keypad
	// Features is the FeatureMap (NV, LK, NK; :22-27): the key groups the
	// device takes.
	Features keypaddef.Feature
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// KeypadServer is a KeypadInput (0x0509) server; the cluster has no
// attributes beyond the globals.
type KeypadServer struct {
	*spec.Server
}

// NewKeypad builds a KeypadInput server over cfg.Port.
func NewKeypad(cfg KeypadConfig) (*KeypadServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: KeypadInput", ErrNilPort)
	}
	h := handlers{}
	port := cfg.Port
	handle(h, keypaddef.CmdSendKey, func(ctx context.Context, req keypaddef.SendKeyRequest) (any, error) {
		return port.SendKey(ctx, req)
	})
	srv, err := newServer(keypaddef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: KeypadInput: %w", err)
	}
	return &KeypadServer{Server: srv}, nil
}
