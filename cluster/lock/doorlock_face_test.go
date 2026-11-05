// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestFaceLockMatchesTheDefinition: a lock whose only credential feature is
// FACE (USR's conformance "PIN | RID | FGP | FACE | ALIRO" holds through
// it) serves the definition's FeatureMap and attribute set, and FACE adds no
// attribute of its own.
func TestFaceLockMatchesTheDefinition(t *testing.T) {
	t.Parallel()
	locked := lockdef.LockStateLocked
	cfg := lock.DoorLockConfig{
		Features:                            lock.FeatureUSR | lock.FeatureFACE,
		LockState:                           &locked,
		LockType:                            lockdef.LockTypeDeadBolt,
		OperatingMode:                       lockdef.OperatingModeNormal,
		NumberOfTotalUsersSupported:         10,
		NumberOfCredentialsSupportedPerUser: 5,
	}
	srv, err := lock.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, lockdef.Definition, uint32(cfg.Features))
	if fm, ok := srv.MatterRead(0xFFFC); !ok || fm != uint32(lock.FeatureUSR|lock.FeatureFACE) {
		t.Errorf("FeatureMap %v (%v), want USR|FACE 0x%X", fm, ok, uint32(lock.FeatureUSR|lock.FeatureFACE))
	}

	with, _ := newLocal(t, func(c *lock.DoorLockConfig) { c.Features |= lock.FeatureFACE })
	without, _ := newLocal(t, nil)
	if a, b := with.MatterAttributes(), without.MatterAttributes(); !slices.Equal(a, b) {
		t.Errorf("FACE moved the attribute list:\n with    %v\n without %v", a, b)
	}
}

// TestFaceCredentialTypeFollowsTheFeature: the Face value of
// CredentialTypeEnum has conformance FACE, so a lock without the feature
// refuses it as a field its validation rejects, with INVALID_COMMAND
// (matter.js remaps the validation error, throwInvalidCommand at
// DoorLockServer.ts:1494-1511), and a lock with it takes it.
func TestFaceCredentialTypeFollowsTheFeature(t *testing.T) {
	t.Parallel()
	ctx := withPairFabric()
	without, _ := newLocal(t, nil)
	if _, st := invokeStatus(t, without, ctx, lockdef.CmdSetCredential, addFace(1, "template", user1)); st != im.StatusInvalidCommand {
		t.Errorf("Face without FACE: 0x%02X, want INVALID_COMMAND", st)
	}
	with, _ := newLocal(t, func(c *lock.DoorLockConfig) { c.Features |= lock.FeatureFACE })
	res, st := invokeStatus(t, with, ctx, lockdef.CmdSetCredential, addFace(1, "template", user1))
	if st != im.StatusSuccess || res.(lockdef.SetCredentialResponse).Status != lockdef.StatusSuccess {
		t.Errorf("Face with FACE: 0x%02X %+v, want Success", st, res)
	}
}

// TestFaceCredentialRoundTrip walks a face credential through every path a
// credential takes: SetCredential (its LockUserChange names LockDataType
// Face, LockAuth.ts:104-121), GetCredentialStatus, the user's credential
// list, the per-user count (NumberOfCredentialsSupportedPerUser), the
// typed and the wildcard ClearCredential (DoorLockServer.ts:604-621) — and
// that a face template is no PIN: UnlockDoor with the template's bytes as
// PINCode is a wrong code (#identifyPin compares PIN credentials only,
// :1145-1167).
func TestFaceCredentialRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := withPairFabric()
	srv, em := newLocal(t, func(c *lock.DoorLockConfig) {
		c.Features |= lock.FeatureFACE
		c.WrongCodeEntryLimit = 10
	})

	res, _ := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addFace(3, "5678", user1))
	if got := res.(lockdef.SetCredentialResponse); got.Status != lockdef.StatusSuccess {
		t.Fatalf("SetCredential Face 3: %+v", got)
	}
	var changed *lockdef.LockUserChangeEvent
	for _, c := range em.calls {
		if ev, ok := c.data.(lockdef.LockUserChangeEvent); ok {
			changed = &ev
		}
	}
	if changed == nil || changed.LockDataType != lockdef.LockDataTypeFace || changed.DataOperationType != lockdef.DataOperationTypeAdd ||
		changed.DataIndex.Null || changed.DataIndex.Value != 3 {
		t.Errorf("LockUserChange %+v, want Face Add of index 3", changed)
	}

	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypeFace, 3)})
	if got := res.(lockdef.GetCredentialStatusResponse); !got.CredentialExists || got.UserIndex.Null || got.UserIndex.Value != 1 {
		t.Errorf("GetCredentialStatus Face 3: %+v, want user 1's", got)
	}
	if u, ok := srv.User(1); !ok || !slices.Contains(u.Credentials, store.DoorLockCredentialRef{Type: uint8(lockdef.CredentialTypeFace), Index: 3}) {
		t.Errorf("user 1 credentials %+v, want Face 3 among them", u.Credentials)
	}

	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{PinCode: pinPtr("5678")}); st == im.StatusSuccess {
		t.Error("UnlockDoor with a face template as PINCode succeeded, want a wrong code")
	}

	// User 1 holds PIN 1 and Face 3; three more fill the five slots.
	for i := uint16(4); i <= 6; i++ {
		res, _ := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addFace(i, "face-"+string(rune('0'+i)), user1))
		if got := res.(lockdef.SetCredentialResponse); got.Status != lockdef.StatusSuccess {
			t.Fatalf("Face %d: %+v", i, got)
		}
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addFace(7, "face-7", user1))
	if got := res.(lockdef.SetCredentialResponse); got.Status != lockdef.StatusResourceExhausted {
		t.Errorf("a sixth credential: %+v, want RESOURCE_EXHAUSTED", got)
	}

	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearCredential,
		lockdef.ClearCredentialRequest{Credential: spec.ValueOf(credential(lockdef.CredentialTypeFace, 3))}); st != im.StatusSuccess {
		t.Fatalf("ClearCredential Face 3: 0x%02X", st)
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypeFace, 3)})
	if res.(lockdef.GetCredentialStatusResponse).CredentialExists {
		t.Error("Face 3 exists after ClearCredential")
	}

	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearCredential,
		lockdef.ClearCredentialRequest{Credential: spec.NullOf[lockdef.CredentialStruct]()}); st != im.StatusSuccess {
		t.Fatalf("ClearCredential null: 0x%02X", st)
	}
	for i := uint16(4); i <= 6; i++ {
		res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypeFace, i)})
		if res.(lockdef.GetCredentialStatusResponse).CredentialExists {
			t.Errorf("Face %d exists after the wildcard ClearCredential", i)
		}
	}
}
