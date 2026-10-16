// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
)

// ContentLauncherState is the ContentLauncher attribute state
// (content-launcher.element.ts:31-42), served with UrlPlayback (UP):
// AcceptHeader, the content types the player takes, defaults to the empty
// list (:34); SupportedStreamingProtocols to none (:41).
type ContentLauncherState struct {
	AcceptHeader                []string
	SupportedStreamingProtocols launchdef.SupportedProtocolsBitmap
}

func (st ContentLauncherState) values() map[uint32]any {
	header := st.AcceptHeader
	if header == nil {
		header = []string{} // the element's default, :34
	}
	return map[uint32]any{
		launchdef.AttrAcceptHeader:                header,
		launchdef.AttrSupportedStreamingProtocols: st.SupportedStreamingProtocols,
	}
}

func (st ContentLauncherState) clone() ContentLauncherState {
	st.AcceptHeader = slices.Clone(st.AcceptHeader)
	return st
}

// ContentLauncher is the ContentLauncher host port: LaunchContent
// (ContentSearch, CS, :44-54) and LaunchURL (UrlPlayback, UP, :56-65),
// each answered by LauncherResponse (:67-71). A command whose feature the
// selection leaves out is not accepted, implemented or not.
type ContentLauncher interface {
	LaunchContent(ctx context.Context, st *ContentLauncherState, req launchdef.LaunchContentRequest) (launchdef.LauncherResponse, error)
	LaunchURL(ctx context.Context, st *ContentLauncherState, req launchdef.LaunchUrlRequest) (launchdef.LauncherResponse, error)
}

// ContentLauncherConfig carries the construction parameters of a
// ContentLauncher server.
type ContentLauncherConfig struct {
	// Port carries out the commands; required.
	Port ContentLauncher
	// Features is the FeatureMap (CS, UP, AS, TT, AT).
	Features launchdef.Feature
	// Initial is the state the server starts with.
	Initial ContentLauncherState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// ContentLauncherServer is a ContentLauncher (0x050A) server.
type ContentLauncherServer struct {
	*spec.Server
	stateful[ContentLauncherState]
}

// NewContentLauncher builds a ContentLauncher server over cfg.Port.
func NewContentLauncher(cfg ContentLauncherConfig) (*ContentLauncherServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: ContentLauncher", ErrNilPort)
	}
	s := &ContentLauncherServer{}
	h := handlers{}
	port := cfg.Port
	handleState(h, &s.stateful, launchdef.CmdLaunchContent, func(ctx context.Context, st *ContentLauncherState, req launchdef.LaunchContentRequest) (any, error) {
		return port.LaunchContent(ctx, st, req)
	})
	handleState(h, &s.stateful, launchdef.CmdLaunchUrl, func(ctx context.Context, st *ContentLauncherState, req launchdef.LaunchUrlRequest) (any, error) {
		return port.LaunchURL(ctx, st, req)
	})
	srv, err := newServer(launchdef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: ContentLauncher: %w", err)
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, ContentLauncherState.values, ContentLauncherState.clone); err != nil {
		return nil, fmt.Errorf("media: ContentLauncher: %w", err)
	}
	return s, nil
}
