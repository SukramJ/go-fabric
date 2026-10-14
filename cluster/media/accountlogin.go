// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
)

// AccountLogin is the AccountLogin host port (account-login.element.ts:
// 26-49). The three commands are fabric-scoped and timed ("F A T",
// "F O T"): the dispatcher refuses them outside a timed interaction, and
// the accessing fabric is on ctx (im.LookupFabricFilter).
type AccountLogin interface {
	// GetSetupPIN answers the setup PIN for TempAccountIdentifier (:26-37).
	GetSetupPIN(ctx context.Context, req logindef.GetSetupPinRequest) (logindef.GetSetupPinResponse, error)
	// Login logs the account in with the setup PIN (:39-44); status only.
	Login(ctx context.Context, req logindef.LoginRequest) error
	// Logout logs the account out (:46-49); status only.
	Logout(ctx context.Context, req logindef.LogoutRequest) error
}

// AccountLoginConfig carries the construction parameters of an
// AccountLogin server.
type AccountLoginConfig struct {
	// Port carries out the commands; required.
	Port AccountLogin
	// Events lists the optional events emitted: EventLoggedOut.
	Events []uint32
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// AccountLoginServer is an AccountLogin (0x050E) server; the cluster has no
// attributes beyond the globals.
type AccountLoginServer struct {
	*spec.Server
	events
}

// NewAccountLogin builds an AccountLogin server over cfg.Port.
func NewAccountLogin(cfg AccountLoginConfig) (*AccountLoginServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: AccountLogin", ErrNilPort)
	}
	srv, err := spec.NewServer(logindef.Definition, spec.Options{Events: cfg.Events}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: AccountLogin: %w", err)
	}
	port := cfg.Port
	handle(srv, logindef.CmdGetSetupPin, func(ctx context.Context, req logindef.GetSetupPinRequest) (any, error) {
		return port.GetSetupPIN(ctx, req)
	})
	handle(srv, logindef.CmdLogin, func(ctx context.Context, req logindef.LoginRequest) (any, error) {
		return nil, port.Login(ctx, req)
	})
	handle(srv, logindef.CmdLogout, func(ctx context.Context, req logindef.LogoutRequest) (any, error) {
		return nil, port.Logout(ctx, req)
	})
	return &AccountLoginServer{Server: srv}, nil
}

// EmitLoggedOut emits LoggedOut (event 0x00, critical; :21-24), which the
// server emits when cfg.Events lists it.
func (s *AccountLoginServer) EmitLoggedOut(ev logindef.LoggedOutEvent) error {
	return s.Emit(s.endpoint(), logindef.EventLoggedOut, ev)
}
