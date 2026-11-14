// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestIncreaseConfigurationVersionsPersistsUnderACancelledContext: a
// version IncreaseConfigurationVersions raised and served is stored even
// when the caller's context is already cancelled, so it does not go back
// down after a restart.
func TestIncreaseConfigurationVersionsPersistsUnderACancelledContext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "node.db")
	n := newNode(t, minimalConfig(openDB(t, path)))
	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ep, _ := endpointOf(t, n, "plug:1")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := n.Aggregator().IncreaseConfigurationVersions(cancelled, endpoint.StringKey("plug:1")); err != nil {
		t.Fatalf("IncreaseConfigurationVersions under a cancelled context: %v", err)
	}
	raised := n.Bridge().Topology().FindByID(ep).ConfigurationVersion()
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	again := newNode(t, minimalConfig(openDB(t, path)))
	if err := again.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := again.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := again.Bridge().Topology().FindByID(ep).ConfigurationVersion(); got != raised {
		t.Errorf("ConfigurationVersion %v served before the restart, %v after", raised, got)
	}
}

// failingSettings is a credential store whose SetSetting and SetSettings
// fail once fail is set.
type failingSettings struct {
	store.Port
	fail atomic.Bool
}

var errSettingRefused = errors.New("setting refused")

func (s *failingSettings) SetSetting(ctx context.Context, key, value string) error {
	if s.fail.Load() {
		return errSettingRefused
	}
	return s.Port.SetSetting(ctx, key, value)
}

func (s *failingSettings) SetSettings(ctx context.Context, values map[string]string) error {
	if s.fail.Load() {
		return errSettingRefused
	}
	return s.Port.SetSettings(ctx, values)
}

// TestIncreaseConfigurationVersionsReturnsAFailedWrite: a raised version
// the store refuses is reported to the caller, not only logged.
func TestIncreaseConfigurationVersionsReturnsAFailedWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := &failingSettings{Port: memstore.New()}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	st.fail.Store(true)
	if err := n.Aggregator().IncreaseConfigurationVersions(ctx, endpoint.StringKey("plug:1")); !errors.Is(err, errSettingRefused) {
		t.Fatalf("IncreaseConfigurationVersions with the store refusing the write: %v, want the store's error", err)
	}
}
