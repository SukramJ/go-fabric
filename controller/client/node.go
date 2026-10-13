// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"sync"

	"github.com/SukramJ/go-fabric/controller/exchange"
)

// Node is the image of one remote node — matter.js ClientNode
// (node/ClientNode.ts:55-409 at 0ceb202d) with the subscription and
// connection-state half of NetworkClient. It is safe for concurrent use.
type Node struct {
	cfg   Config
	log   *slog.Logger
	clock exchange.Clock
	rand  func() float64

	mu        sync.RWMutex
	settings  settings
	started   bool
	closed    bool
	connState ConnectionState
}

// settings are the node's own persisted state — matter.js
// NetworkClient.State (NetworkClient.ts:331-372, quality N).
type settings struct {
	disabled            bool
	autoSubscribe       bool
	defaultSubscription *SubscribeOptions
}

// New builds the node cfg describes. Mirrors ClientNode.initialize
// (ClientNode.ts:105-133): synchronous, nothing installed but the image.
func New(cfg Config) (*Node, error) {
	if cfg.Interaction == nil {
		return nil, fmt.Errorf("%w: no Interaction", ErrConfig)
	}
	n := &Node{cfg: cfg, log: cfg.Logger, clock: cfg.Clock, rand: cfg.Rand, connState: Disconnected}
	if n.log == nil {
		n.log = slog.New(slog.DiscardHandler)
	}
	n.log = n.log.With(slog.String("node", cfg.ID))
	if n.clock == nil {
		n.clock = exchange.SystemClock{}
	}
	if n.rand == nil {
		n.rand = mrand.Float64
	}
	n.settings.autoSubscribe = cfg.AutoSubscribe
	n.settings.defaultSubscription = cfg.DefaultSubscription
	return n, nil
}

// Start brings the node online — ClientNode start through
// NetworkClient.startup (NetworkClient.ts:60-102). A disabled node does
// not start: [ErrDisabled] (ClientNetworkRuntime.ts:41-43).
func (n *Node) Start(ctx context.Context) error {
	_ = ctx
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case n.closed:
		return ErrClosed
	case n.settings.disabled:
		return ErrDisabled
	}
	n.started = true
	return nil
}

// Stop takes the node offline.
func (n *Node) Stop(ctx context.Context) error {
	_ = ctx
	n.mu.Lock()
	defer n.mu.Unlock()
	n.started = false
	return nil
}

// Close stops the node and releases it; it is idempotent.
func (n *Node) Close(ctx context.Context) error {
	err := n.Stop(ctx)
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	return err
}

// Disable takes the node offline and keeps it so — ClientNode.disable
// (ClientNode.ts:236-250).
func (n *Node) Disable(ctx context.Context) error {
	n.mu.RLock()
	disabled := n.settings.disabled
	n.mu.RUnlock()
	if disabled {
		return nil
	}
	if err := n.Stop(ctx); err != nil {
		return err
	}
	n.mu.Lock()
	n.settings.disabled = true
	n.mu.Unlock()
	return nil
}

// Enable lifts Disable and starts the node — ClientNode.enable
// (ClientNode.ts:257-264).
func (n *Node) Enable(ctx context.Context) error {
	n.mu.Lock()
	if !n.settings.disabled {
		n.mu.Unlock()
		return nil
	}
	n.settings.disabled = false
	n.mu.Unlock()
	return n.Start(ctx)
}

// Disabled reports whether the node is disabled.
func (n *Node) Disabled() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.disabled
}

// Erase stops the node and forgets its image — ClientNode.erase
// (ClientNode.ts:227-229, :266-271).
func (n *Node) Erase(ctx context.Context) error {
	if err := n.Stop(ctx); err != nil {
		return err
	}
	n.mu.Lock()
	n.settings = settings{}
	n.mu.Unlock()
	return nil
}

// ConnectionState is the node's connection state
// (ClientNodeLifecycle.ts:80-82).
func (n *Node) ConnectionState() ConnectionState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.connState
}

// ID is the node's local id.
func (n *Node) ID() string { return n.cfg.ID }
