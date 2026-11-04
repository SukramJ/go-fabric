// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// failingICDStore fails the ICD client's registration writes while fail
// is set — matter.js IcdClientTest's GatedIcdStorage with
// failPendingKeyWrite.
type failingICDStore struct {
	client.Store
	fail atomic.Bool
}

func (s *failingICDStore) Save(id string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	if cluster == client.ICDClusterID && s.fail.Load() {
		return errors.New("storage refused")
	}
	return s.Store.Save(id, ep, cluster, values, version)
}

// TestIcdClientKeyStoreParity ports IcdClientTest's "offers no key and
// keeps the receive path on the committed state when storing the offered
// key fails" (packages/node/test/behavior/system/icd/IcdClientTest.ts:806-824
// at 862ad75f): the pending key the store refused stays committed in
// memory and in the Check-In receive path, but it is never offered, so
// the device keeps the old key (IcdClient.ts:882-901, :860-869).
func TestIcdClientKeyStoreParity(t *testing.T) {
	t.Parallel()
	st := &failingICDStore{}
	log := &msgLog{}
	p := newICDPair(t, icdPairOptions{controller: fabrictest.ControllerOptions{
		Logger: slog.New(log),
		Registry: func(c *client.RegistryConfig) {
			if c.Store != client.Store(st) {
				st.Store = c.Store
				c.Store = st
			}
		},
	}})
	p.register(t, uncovered)
	p.requestRefreshOnNextCheckIn(t)
	stored, _ := p.icd().Registration()
	storedKey := stored.Key
	st.fail.Store(true)
	p.wakeDevice(t)
	p.eventually(t, "the refresh's retry warning", func() bool { return log.has("matter.controller.client.icd_refresh_retry") })
	reg, _ := p.icd().Registration()
	if reg.PendingKey == nil {
		t.Fatal("the offered key is not pending in memory")
	}
	fed := p.fedPeer(t)
	if !bytes.Equal(fed.PendingKey, reg.PendingKey) {
		t.Fatal("the receive path does not hold the pending key")
	}
	if !bytes.Equal(fed.Key, storedKey) || !bytes.Equal(reg.Key, storedKey) {
		t.Fatal("the committed key moved")
	}
	if !bytes.Equal(p.deviceKey(t), storedKey) {
		t.Fatal("the device took the key the store refused")
	}
}
