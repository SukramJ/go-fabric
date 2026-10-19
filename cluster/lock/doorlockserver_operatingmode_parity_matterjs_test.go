// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("OperatingMode") (:1419-1471) inside
// "authorization of lock operations" — SupportedOperatingModes and the
// writes it admits, on setUpScheduledLock (doorlockserver_harness_test.go).
// matter.js writes through the controller's client endpoint (setStateOf);
// the port writes over the wire.

package lock_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// enum8 is an enum value to write.
type enum8 uint8

func (v enum8) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, uint64(v)) }

// writeOperatingMode writes OperatingMode over the wire.
func (f *lockFixture) writeOperatingMode(mode lockdef.OperatingModeEnum) error {
	f.t.Helper()
	path := im.ConcreteAttributePath{
		Endpoint: f.ep, Cluster: lockdef.ClusterID, Attribute: lockdef.AttrOperatingMode,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	return fabrictest.Write(f.ctx, f.pair, path, enum8(mode))
}

// operatingModeNow reads OperatingMode.
func (f *lockFixture) operatingModeNow() any {
	v, _ := f.srv.MatterRead(lockdef.AttrOperatingMode)
	return v
}

func TestDoorLockServerOperatingModeParity(t *testing.T) {
	t.Run("refuses a write of an unsupported mode and accepts a supported one", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		err := f.writeOperatingMode(lockdef.OperatingModePrivacy)
		var se *interaction.StatusError
		if !errors.As(err, &se) || se.Status != im.StatusConstraintError {
			t.Errorf("write Privacy: %v, want CONSTRAINT_ERROR", err)
		}
		if got := f.operatingModeNow(); got != uint8(lockdef.OperatingModeNormal) {
			t.Errorf("operatingMode %v, want Normal", got)
		}
		if err := f.writeOperatingMode(lockdef.OperatingModeNoRemoteLockUnlock); err != nil {
			t.Fatalf("write NoRemoteLockUnlock: %v", err)
		}
		if got := f.operatingModeNow(); got != uint8(lockdef.OperatingModeNoRemoteLockUnlock) {
			t.Errorf("operatingMode %v, want NoRemoteLockUnlock", got)
		}
	})

	t.Run("switches a stored mode SupportedOperatingModes does not support to Normal", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) { c.OperatingMode = lockdef.OperatingModePrivacy },
			scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if got := f.operatingModeNow(); got != uint8(lockdef.OperatingModeNormal) {
			t.Errorf("operatingMode %v, want Normal", got)
		}
	})

	t.Run("refuses SupportedOperatingModes without Normal", func(t *testing.T) {
		// matter.js's node fails to start with the ImplementationError of
		// initialize (:1126-1128) as the cause; the Go server is refused at
		// construction, before any node.
		cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		cfg.SupportedOperatingModes = lockdef.OperatingModesAlwaysSet | lockdef.OperatingModesNormal
		if _, err := lock.New(cfg); !errors.Is(err, lock.ErrConfig) {
			t.Errorf("lock.New: %v, want ErrConfig", err)
		}
	})

	t.Run("accepts every mode SupportedOperatingModes supports", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			c.SupportedOperatingModes = lockdef.OperatingModesAlwaysSet | lockdef.OperatingModesPrivacy
		}, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for _, mode := range []lockdef.OperatingModeEnum{lockdef.OperatingModeVacation, lockdef.OperatingModePassage, lockdef.OperatingModeNoRemoteLockUnlock} {
			if err := f.writeOperatingMode(mode); err != nil {
				t.Fatalf("write %d: %v", mode, err)
			}
			if got := f.operatingModeNow(); got != uint8(mode) {
				t.Errorf("operatingMode %v, want %d", got, mode)
			}
		}
	})
}
