// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	observerdef "github.com/SukramJ/go-fabric/cluster/spec/contentappobserver"
)

// ContentAppObserver is the ContentAppObserver host port: ContentAppMessage
// with its Data and EncodingHint (content-app-observer.element.ts:22-29),
// answered by ContentAppMessageResponse (:31-36).
type ContentAppObserver interface {
	ContentAppMessage(ctx context.Context, req observerdef.ContentAppMessageRequest) (observerdef.ContentAppMessageResponse, error)
}

// ContentAppObserverConfig carries the construction parameters of a
// ContentAppObserver server.
type ContentAppObserverConfig struct {
	// Port answers ContentAppMessage; required.
	Port ContentAppObserver
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// ContentAppObserverServer is a ContentAppObserver (0x0510) server.
type ContentAppObserverServer struct {
	*spec.Server
}

// NewContentAppObserver builds a ContentAppObserver server over cfg.Port.
func NewContentAppObserver(cfg ContentAppObserverConfig) (*ContentAppObserverServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: ContentAppObserver", ErrNilPort)
	}
	srv, err := spec.NewServer(observerdef.Definition, spec.Options{}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: ContentAppObserver: %w", err)
	}
	port := cfg.Port
	handle(srv, observerdef.CmdContentAppMessage, func(ctx context.Context, req observerdef.ContentAppMessageRequest) (any, error) {
		return port.ContentAppMessage(ctx, req)
	})
	return &ContentAppObserverServer{Server: srv}, nil
}
