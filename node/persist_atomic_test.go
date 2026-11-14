// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// refusingKeys is a credential store that refuses, while refuse is set,
// every write of a key with one of the prefixes in keys — a single
// SetSetting and a SetSettings that names one alike, which then writes
// nothing (store.Settings.SetSettings).
type refusingKeys struct {
	store.Port
	keys   []string
	refuse atomic.Bool
}

var errKeyRefused = errors.New("key refused")

func (s *refusingKeys) refused(key string) bool {
	if !s.refuse.Load() {
		return false
	}
	for _, k := range s.keys {
		if strings.HasPrefix(key, k) {
			return true
		}
	}
	return false
}

func (s *refusingKeys) SetSetting(ctx context.Context, key, value string) error {
	if s.refused(key) {
		return errKeyRefused
	}
	return s.Port.SetSetting(ctx, key, value)
}

func (s *refusingKeys) SetSettings(ctx context.Context, values map[string]string) error {
	for k := range values {
		if s.refused(k) {
			return errKeyRefused
		}
	}
	return s.Port.SetSettings(ctx, values)
}

// TestRootLabelWriteLandsWholeOrNotAtAll: the root's NodeLabel and
// Location are stored in one write, so a refused write of one leaves the
// other as it was — a NodeLabel that answered FAILURE is not served after
// a restart (R2-P2-store-06).
func TestRootLabelWriteLandsWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"basic_information.location"}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	st.refuse.Store(true)
	if err := n.Root().BasicInformation.SetNodeLabel(ctx, "refused"); !errors.Is(err, errKeyRefused) {
		t.Fatalf("SetNodeLabel with the Location write refused: %v, want the store's error", err)
	}
	if v, ok, err := st.GetSetting(ctx, "basic_information.node_label"); err != nil || (ok && v == "refused") {
		t.Errorf("stored NodeLabel %q (found %v, %v) after a write that answered FAILURE", v, ok, err)
	}
}
