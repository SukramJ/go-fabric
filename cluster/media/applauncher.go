// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
)

// AppLauncherState is the ApplicationLauncher attribute state
// (application-launcher.element.ts:25-32), the cluster's generated State.
// CatalogList (ApplicationPlatform, AP) has no default: nil refuses
// construction with AP ([ErrMissingState]). CurrentApp (optional,
// nullable) reads as null while nil.
type AppLauncherState = launcherdef.State

func appLauncherValues(st AppLauncherState) map[uint32]any {
	return map[uint32]any{
		launcherdef.AttrCatalogList: uint16sOr(st.CatalogList),
		launcherdef.AttrCurrentApp:  nullOr(st.CurrentApp),
	}
}

func cloneAppLauncher(st AppLauncherState) AppLauncherState {
	st.CatalogList = slices.Clone(st.CatalogList)
	return st
}

// AppLauncher is the ApplicationLauncher host port (the generated
// [launcherdef.AppLauncher], the declaration's AppLauncher): LaunchApp,
// StopApp and HideApp (:34-50), each answered by LauncherResponse
// (:51-55). On a content app platform (AP) the Application field names the
// app; the port makes it current in st and moves the app's
// ApplicationBasic.Status.
type AppLauncher = launcherdef.AppLauncher

// AppLauncherConfig carries the construction parameters of an
// ApplicationLauncher server.
type AppLauncherConfig struct {
	// Port carries out the commands; required.
	Port AppLauncher
	// Features is the FeatureMap (AP).
	Features launcherdef.Feature
	// Attributes lists the optional attributes served: AttrCurrentApp.
	Attributes []uint32
	// Initial is the state the server starts with.
	Initial AppLauncherState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// AppLauncherServer is an ApplicationLauncher (0x050C) server.
type AppLauncherServer struct {
	*spec.Server
	stateful[AppLauncherState]
}

// NewAppLauncher builds an ApplicationLauncher server over cfg.Port.
func NewAppLauncher(cfg AppLauncherConfig) (*AppLauncherServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: ApplicationLauncher", ErrNilPort)
	}
	s := &AppLauncherServer{}
	h := handlers{}
	port := cfg.Port
	handleState(h, &s.stateful, launcherdef.CmdLaunchApp, func(ctx context.Context, st *AppLauncherState, req launcherdef.LaunchAppRequest) (any, error) {
		return port.LaunchApp(ctx, st, req)
	})
	handleState(h, &s.stateful, launcherdef.CmdStopApp, func(ctx context.Context, st *AppLauncherState, req launcherdef.StopAppRequest) (any, error) {
		return port.StopApp(ctx, st, req)
	})
	handleState(h, &s.stateful, launcherdef.CmdHideApp, func(ctx context.Context, st *AppLauncherState, req launcherdef.HideAppRequest) (any, error) {
		return port.HideApp(ctx, st, req)
	})
	srv, err := newServer(launcherdef.Definition, spec.Options{Features: uint32(cfg.Features), Attributes: cfg.Attributes},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: ApplicationLauncher: %w", err)
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, appLauncherValues, cloneAppLauncher, storeState[AppLauncherState]); err != nil {
		return nil, fmt.Errorf("media: ApplicationLauncher: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}

// SetAttributes implements [spec.AttributeSetter] ([stateful.setAttributes]).
func (s *AppLauncherServer) SetAttributes(values map[uint32]any) error {
	return s.setAttributes(values)
}
