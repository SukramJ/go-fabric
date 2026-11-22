// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"thingfabric.dev/go-fabric/cluster/spec"
	logindef "thingfabric.dev/go-fabric/cluster/spec/accountlogin"
)

// AccountLogin is the AccountLogin host port (the generated
// [logindef.AccountLogin], the declaration's AccountLogin;
// account-login.element.ts:26-49): GetSetupPIN answers the setup PIN for
// TempAccountIdentifier (:26-37), Login logs the account in with it
// (:39-44) and Logout out (:46-49), both status only. The three commands
// are fabric-scoped and timed ("F A T", "F O T"): the dispatcher refuses
// them outside a timed interaction, and the accessing fabric is on ctx
// (im.LookupFabricFilter).
type AccountLogin = logindef.AccountLogin

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
}

// NewAccountLogin builds an AccountLogin server over cfg.Port.
func NewAccountLogin(cfg AccountLoginConfig) (*AccountLoginServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: AccountLogin", ErrNilPort)
	}
	h := handlers{}
	port := cfg.Port
	handle(h, logindef.CmdGetSetupPin, func(ctx context.Context, req logindef.GetSetupPinRequest) (any, error) {
		return port.GetSetupPIN(ctx, req)
	})
	handle(h, logindef.CmdLogin, func(ctx context.Context, req logindef.LoginRequest) (any, error) {
		return nil, port.Login(ctx, req)
	})
	handle(h, logindef.CmdLogout, func(ctx context.Context, req logindef.LogoutRequest) (any, error) {
		return nil, port.Logout(ctx, req)
	})
	srv, err := newServer(logindef.Definition, spec.Options{Events: cfg.Events}, spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("media: AccountLogin: %w", err)
	}
	return &AccountLoginServer{Server: srv}, nil
}

// EmitLoggedOut emits LoggedOut (event 0x00, critical; :21-24), which the
// server emits when cfg.Events lists it.
func (s *AccountLoginServer) EmitLoggedOut(ev logindef.LoggedOutEvent) error {
	return s.EmitEvent(logindef.EventLoggedOut, ev)
}

// SetEndpoint implements [contract.EndpointReceiver]: the endpoint the
// server's events are addressed to, the store's ([spec.Server.EmitEvent]).
func (s *AccountLoginServer) SetEndpoint(endpoint uint16) { s.Server.SetEndpoint(endpoint) }
