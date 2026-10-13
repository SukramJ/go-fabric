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
	cfg     Config
	log     *slog.Logger
	clock   exchange.Clock
	rand    func() float64
	store   Store
	persist *persister

	mu        sync.RWMutex
	settings  settings
	started   bool
	closed    bool
	connState ConnectionState
}

// New builds the node cfg describes and loads its image from the store.
// Mirrors ClientNode.initialize (ClientNode.ts:105-133): synchronous,
// nothing installed but the image. Config.AutoSubscribe true and a
// Config.DefaultSubscription replace the stored settings.
func New(cfg Config) (*Node, error) {
	if cfg.Interaction == nil {
		return nil, fmt.Errorf("%w: no Interaction", ErrConfig)
	}
	n := &Node{cfg: cfg, log: cfg.Logger, clock: cfg.Clock, rand: cfg.Rand, store: cfg.Store, connState: Disconnected}
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
	if n.store == nil {
		n.store = &MemoryStore{}
	}
	n.persist = &persister{store: n.store, nodeID: cfg.ID, interval: cfg.FlushInterval, clock: n.clock, log: n.log, image: &n.mu}
	stored, err := n.store.Load(cfg.ID)
	if err != nil {
		return nil, fmt.Errorf("client: loading node %q: %w", cfg.ID, err)
	}
	for _, ep := range stored {
		if ep.Number != 0 {
			continue
		}
		for _, c := range ep.Clusters {
			if c.ID != settingsCluster {
				continue
			}
			if n.settings, err = decodeSettings(c.Values); err != nil {
				return nil, fmt.Errorf("client: loading node %q: %w", cfg.ID, err)
			}
		}
	}
	changed := false
	if cfg.AutoSubscribe && !n.settings.autoSubscribe {
		n.settings.autoSubscribe, changed = true, true
	}
	if cfg.DefaultSubscription != nil {
		n.settings.defaultSubscription, changed = cfg.DefaultSubscription.clone(), true
	}
	if changed {
		n.saveSettingsLocked()
	}
	return n, nil
}

// saveSettingsLocked persists the settings; a store error is logged. The
// caller holds n.mu (or owns n exclusively).
func (n *Node) saveSettingsLocked() {
	values, err := n.settings.encode()
	if err == nil {
		err = n.store.Save(n.cfg.ID, 0, settingsCluster, values, nil)
	}
	if err != nil {
		n.log.Warn("matter.controller.client.store_write_failed", slog.String("what", "settings"), slog.String("err", err.Error()))
	}
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
	n.persist.restart()
	return nil
}

// Stop takes the node offline and flushes its store
// (ClientCacheBuffer.ts:94-101).
func (n *Node) Stop(ctx context.Context) error {
	_ = ctx
	n.mu.Lock()
	n.started = false
	n.mu.Unlock()
	n.persist.stop()
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

// Disable takes the node offline and keeps it so, persisted
// (NetworkClient.ts:344-349) — ClientNode.disable (ClientNode.ts:236-250).
func (n *Node) Disable(ctx context.Context) error {
	if n.Disabled() {
		return nil
	}
	if err := n.Stop(ctx); err != nil {
		return err
	}
	n.mu.Lock()
	n.settings.disabled = true
	n.saveSettingsLocked()
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
	n.saveSettingsLocked()
	n.mu.Unlock()
	return n.Start(ctx)
}

// Disabled reports whether the node is disabled.
func (n *Node) Disabled() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.disabled
}

// Erase stops the node and forgets its image, its store included —
// ClientNode.erase (ClientNode.ts:227-229, :266-271).
func (n *Node) Erase(ctx context.Context) error {
	if err := n.Stop(ctx); err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.settings = settings{}
	if err := n.store.EraseNode(n.cfg.ID); err != nil {
		n.log.Warn("matter.controller.client.store_erase_failed", slog.String("err", err.Error()))
	}
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
