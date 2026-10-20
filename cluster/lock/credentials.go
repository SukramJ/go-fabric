// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"context"
	"crypto/subtle"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// validateSetCredential is the request validation matter.js runs before
// setCredential, remapped to INVALID_COMMAND (DoorLockServer.ts:1494-1511):
// the enum constraints and UserIndex "1 to numberOfTotalUsersSupported".
func (s *DoorLockServer) validateSetCredential(req lockdef.SetCredentialRequest) error {
	if err := s.checkEnum("OperationType", lockdef.DataOperationTypeEnumDef, uint64(req.OperationType), dataOperationAddModify...); err != nil {
		return err
	}
	if err := s.checkEnum("CredentialType", lockdef.CredentialTypeEnumDef, uint64(req.Credential.CredentialType)); err != nil {
		return err
	}
	if !req.UserIndex.Null {
		if maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported); req.UserIndex.Value > maxUsers {
			return spec.Errorf(im.StatusInvalidCommand, "lock: UserIndex %d is outside 1 to %d", req.UserIndex.Value, maxUsers)
		}
	}
	if !req.UserStatus.Null {
		if err := s.checkEnum("UserStatus", lockdef.UserStatusEnumDef, uint64(req.UserStatus.Value), requestUserStatuses...); err != nil {
			return err
		}
	}
	if !req.UserType.Null {
		if err := s.checkEnum("UserType", lockdef.UserTypeEnumDef, uint64(req.UserType.Value), setCredentialUserTypes...); err != nil {
			return err
		}
	}
	return nil
}

func (s *DoorLockServer) handleSetCredential(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.SetCredentialRequest](fields)
	if err != nil {
		return nil, err
	}
	if err := s.validateSetCredential(req); err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	var resp lockdef.SetCredentialResponse
	err = s.commit(ctx, func() error {
		var err error
		resp, err = s.setCredentialLocked(ctx, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// setCredentialLocked mirrors matter.js DoorLockServer.ts:419-572
// (setCredential). Every response carries NextCredentialIndex, which
// § 5.2.10.21.3 states independently of the status (:425-430).
func (s *DoorLockServer) setCredentialLocked(ctx context.Context, req lockdef.SetCredentialRequest) (lockdef.SetCredentialResponse, error) {
	fabricIndex, err := requestFabric(ctx)
	if err != nil {
		return lockdef.SetCredentialResponse{}, err
	}
	credType := uint8(req.Credential.CredentialType)
	credIndex := req.Credential.CredentialIndex
	maxIndex := s.maxCredentialIndexForType(req.Credential.CredentialType)
	next := nullable(s.findNextAvailableCredentialIndex(credType, credIndex, maxIndex))
	respond := func(status lockdef.Status, userIndex *uint16) (lockdef.SetCredentialResponse, error) {
		return lockdef.SetCredentialResponse{Status: status, UserIndex: nullable(userIndex), NextCredentialIndex: &next}, nil
	}

	if !credentialIndexValid(req.Credential.CredentialType, credIndex, maxIndex) {
		return respond(invalidCommand, nil)
	}
	if !s.validateCredentialDataLength(req.Credential.CredentialType, req.CredentialData) {
		return respond(invalidCommand, nil)
	}
	if s.isDuplicateCredential(credType, req.CredentialData, credIndex) {
		return respond(duplicate, nil)
	}
	if !requestMatchesUseCase(req) {
		return respond(invalidCommand, nil)
	}

	ref := store.DoorLockCredentialRef{Type: credType, Index: credIndex}
	switch req.OperationType {
	case lockdef.DataOperationTypeAdd:
		return s.addCredentialCaseLocked(ctx, req, fabricIndex, ref, respond)
	case lockdef.DataOperationTypeModify:
		existing := s.findCredential(credType, credIndex)
		if existing == nil || fabricIndex != existing.CreatorFabricIndex {
			return respond(invalidCommand, nil)
		}
		if !req.UserIndex.Null {
			user := s.findUser(req.UserIndex.Value)
			if user == nil || fabricIndex != user.CreatorFabricIndex || !slices.Contains(user.Credentials, ref) {
				return respond(invalidCommand, nil)
			}
		}
		if err := s.updateCredentialDataLocked(credType, credIndex, req.CredentialData, fabricIndex); err != nil {
			return lockdef.SetCredentialResponse{}, err
		}
		owner := s.ownerOf(credType, credIndex)
		if !req.UserIndex.Null {
			owner = &req.UserIndex.Value
		}
		s.emitLockUserChangeLocked(ctx, credentialTypeToLockDataType(req.Credential.CredentialType), lockdef.DataOperationTypeModify, owner, &credIndex)
		return respond(success, nil)
	case lockdef.DataOperationTypeClear:
	}
	return respond(invalidCommand, nil)
}

// The statuses of a SetCredential response.
const (
	success           = lockdef.StatusSuccess
	invalidCommand    = lockdef.StatusInvalidCommand
	resourceExhausted = lockdef.StatusResourceExhausted
	duplicate         = lockdef.Status(lockdef.StatusCodeDuplicate)
	occupied          = lockdef.Status(lockdef.StatusCodeOccupied)
)

// addCredentialCaseLocked is the Add branch of the handler above: the
// credential is added, then its user created or extended
// (auth.addCredential and auth.addUser, DoorLockServer.ts:448-524).
func (s *DoorLockServer) addCredentialCaseLocked(ctx context.Context, req lockdef.SetCredentialRequest, fabricIndex uint8,
	ref store.DoorLockCredentialRef, respond func(lockdef.Status, *uint16) (lockdef.SetCredentialResponse, error),
) (lockdef.SetCredentialResponse, error) {
	credType, credIndex := ref.Type, ref.Index
	if s.findCredential(credType, credIndex) != nil {
		return respond(occupied, nil)
	}
	if err := s.addCredentialLocked(credType, credIndex, req.CredentialData, fabricIndex); err != nil {
		return lockdef.SetCredentialResponse{}, err
	}
	var created *uint16
	owner := req.UserIndex.Value
	if req.UserIndex.Null {
		maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
		newIndex, ok := s.findAvailableUserIndex(maxUsers)
		if !ok {
			s.removeCredentialLocked(credType, credIndex)
			return respond(occupied, nil)
		}
		user := store.DoorLockUser{
			UserIndex:               newIndex,
			UserStatus:              uint8(lockdef.UserStatusOccupiedEnabled),
			UserType:                uint8(lockdef.UserTypeUnrestrictedUser),
			CredentialRule:          uint8(lockdef.CredentialRuleSingle),
			Credentials:             []store.DoorLockCredentialRef{ref},
			CreatorFabricIndex:      fabricIndex,
			LastModifiedFabricIndex: fabricIndex,
		}
		if !req.UserStatus.Null {
			user.UserStatus = uint8(req.UserStatus.Value)
		}
		if !req.UserType.Null {
			user.UserType = uint8(req.UserType.Value)
		}
		s.addUserLocked(user)
		created, owner = &newIndex, newIndex
		s.emitLockUserChangeLocked(ctx, lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeAdd, &newIndex, &newIndex)
	} else {
		user := s.findUser(req.UserIndex.Value)
		if user == nil || fabricIndex != user.CreatorFabricIndex {
			s.removeCredentialLocked(credType, credIndex)
			return respond(invalidCommand, nil)
		}
		if perUser, _ := s.uint8Attr(lockdef.AttrNumberOfCredentialsSupportedPerUser); len(user.Credentials) >= int(perUser) {
			s.removeCredentialLocked(credType, credIndex)
			return respond(resourceExhausted, nil)
		}
		user.Credentials = append(user.Credentials, ref)
		user.LastModifiedFabricIndex = fabricIndex
		s.dirty = true
	}
	s.emitLockUserChangeLocked(ctx, credentialTypeToLockDataType(req.Credential.CredentialType), lockdef.DataOperationTypeAdd, &owner, &credIndex)
	return respond(success, created)
}

func (s *DoorLockServer) handleGetCredentialStatus(_ context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.GetCredentialStatusRequest](fields)
	if err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCredentialStatusLocked(req), nil
}

// getCredentialStatusLocked mirrors matter.js DoorLockServer.ts:574-602
// (getCredentialStatus).
func (s *DoorLockServer) getCredentialStatusLocked(req lockdef.GetCredentialStatusRequest) lockdef.GetCredentialStatusResponse {
	credType := uint8(req.Credential.CredentialType)
	index := req.Credential.CredentialIndex
	next := nullable(s.findNextOccupiedCredentialIndex(credType, index))
	resp := lockdef.GetCredentialStatusResponse{
		UserIndex:               spec.NullOf[uint16](),
		CreatorFabricIndex:      spec.NullOf[uint8](),
		LastModifiedFabricIndex: spec.NullOf[uint8](),
		NextCredentialIndex:     &next,
	}
	cred := s.findCredential(credType, index)
	if cred == nil {
		return resp
	}
	resp.CredentialExists = true
	resp.UserIndex = nullable(s.ownerOf(credType, index))
	resp.CreatorFabricIndex = spec.ValueOf(cred.CreatorFabricIndex)
	resp.LastModifiedFabricIndex = spec.ValueOf(cred.LastModifiedFabricIndex)
	return resp
}

func (s *DoorLockServer) handleClearCredential(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.ClearCredentialRequest](fields)
	if err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.commit(ctx, func() error { return s.clearCredentialLocked(ctx, req) })
}

// clearCredentialLocked mirrors matter.js DoorLockServer.ts:604-649
// (clearCredential).
func (s *DoorLockServer) clearCredentialLocked(ctx context.Context, req lockdef.ClearCredentialRequest) error {
	if _, err := requestFabric(ctx); err != nil {
		return err
	}
	if req.Credential.Null {
		for _, t := range []lockdef.CredentialTypeEnum{
			lockdef.CredentialTypePin, lockdef.CredentialTypeRfid, lockdef.CredentialTypeFingerprint,
			lockdef.CredentialTypeFingerVein, lockdef.CredentialTypeFace,
		} {
			s.clearAllCredentialsOfTypeLocked(ctx, t)
		}
		return nil
	}
	c := req.Credential.Value
	if c.CredentialType == lockdef.CredentialTypeProgrammingPin {
		return spec.Errorf(im.StatusInvalidCommand, "lock: cannot clear the programming PIN")
	}
	if c.CredentialIndex == clearAll {
		s.clearAllCredentialsOfTypeLocked(ctx, c.CredentialType)
		return nil
	}
	credType := uint8(c.CredentialType)
	if s.findCredential(credType, c.CredentialIndex) == nil {
		return spec.Errorf(im.StatusInvalidCommand, "lock: credential not found")
	}
	owner := s.ownerOf(credType, c.CredentialIndex)
	s.removeCredentialFromUsersLocked(credType, c.CredentialIndex)
	s.removeCredentialLocked(credType, c.CredentialIndex)
	index := c.CredentialIndex
	s.emitLockUserChangeLocked(ctx, credentialTypeToLockDataType(c.CredentialType), lockdef.DataOperationTypeClear, owner, &index)
	return nil
}

// clearAllCredentialsOfTypeLocked mirrors matter.js DoorLockServer.ts:1456-1470.
func (s *DoorLockServer) clearAllCredentialsOfTypeLocked(ctx context.Context, credType lockdef.CredentialTypeEnum) {
	t := uint8(credType)
	for _, c := range s.data.Credentials {
		if c.Type == t {
			s.removeCredentialFromUsersLocked(t, c.Index)
		}
	}
	s.data.Credentials = slices.DeleteFunc(s.data.Credentials, func(c store.DoorLockCredential) bool { return c.Type == t })
	s.dirty = true
	idx := clearAll
	s.emitLockUserChangeLocked(ctx, credentialTypeToLockDataType(credType), lockdef.DataOperationTypeClear, &idx, &idx)
}

// removeCredentialFromUsersLocked mirrors matter.js DoorLockServer.ts:1472-1483.
func (s *DoorLockServer) removeCredentialFromUsersLocked(credType uint8, index uint16) {
	owner, ok := s.findUserIndexForCredential(credType, index)
	if !ok {
		return
	}
	if user := s.findUser(owner); user != nil {
		user.Credentials = slices.DeleteFunc(user.Credentials, func(c store.DoorLockCredentialRef) bool {
			return c.Type == credType && c.Index == index
		})
		s.dirty = true
	}
}

// clearCredentialsForUserLocked mirrors matter.js DoorLockServer.ts:1447-1454.
func (s *DoorLockServer) clearCredentialsForUserLocked(userIndex uint16) {
	user := s.findUser(userIndex)
	if user == nil {
		return
	}
	for _, c := range user.Credentials {
		s.removeCredentialLocked(c.Type, c.Index)
	}
}

// ownerOf is the user a credential belongs to, nil for none.
func (s *DoorLockServer) ownerOf(credType uint8, index uint16) *uint16 {
	if owner, ok := s.findUserIndexForCredential(credType, index); ok {
		return &owner
	}
	return nil
}

// requestMatchesUseCase mirrors matter.js DoorLockServer.ts:1375-1401: of
// the SetCredential use cases of § 5.2.10.20 only the one that creates a
// user alongside the credential carries user fields, and the programming
// PIN is modified with UserType ProgrammingUser alone.
func requestMatchesUseCase(req lockdef.SetCredentialRequest) bool {
	switch {
	case req.OperationType == lockdef.DataOperationTypeAdd && req.UserIndex.Null:
		return req.UserType.Null || req.UserType.Value != lockdef.UserTypeProgrammingUser
	case req.OperationType == lockdef.DataOperationTypeModify && req.UserIndex.Null:
		return req.UserStatus.Null && !req.UserType.Null && req.UserType.Value == lockdef.UserTypeProgrammingUser &&
			req.Credential.CredentialType == lockdef.CredentialTypeProgrammingPin
	}
	return req.UserStatus.Null && req.UserType.Null
}

// credentialIndexValid mirrors matter.js DoorLockServer.ts:1403-1413:
// § 5.2.6.24.2 states index 0 for the programming PIN, the one type that
// indexes into nothing; every other type indexes from 1.
func credentialIndexValid(t lockdef.CredentialTypeEnum, index, maxIndex uint16) bool {
	if t == lockdef.CredentialTypeProgrammingPin {
		return index == 0
	}
	return index >= 1 && index <= maxIndex
}

// maxCredentialIndexForType mirrors matter.js DoorLockServer.ts:1415-1427.
func (s *DoorLockServer) maxCredentialIndexForType(t lockdef.CredentialTypeEnum) uint16 {
	var v uint16
	switch t {
	case lockdef.CredentialTypeProgrammingPin:
		// Indexes into nothing, so there is no slot to scan and none to report.
		return 0
	case lockdef.CredentialTypePin:
		v, _ = s.uint16Attr(lockdef.AttrNumberOfPinUsersSupported)
	case lockdef.CredentialTypeRfid:
		v, _ = s.uint16Attr(lockdef.AttrNumberOfRfidUsersSupported)
	default:
		v, _ = s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	}
	return v
}

// validateCredentialDataLength mirrors matter.js DoorLockServer.ts:1429-1445;
// a length attribute the selection does not serve admits no data.
func (s *DoorLockServer) validateCredentialDataLength(t lockdef.CredentialTypeEnum, data []byte) bool {
	var minAttr, maxAttr uint32
	switch t {
	case lockdef.CredentialTypePin, lockdef.CredentialTypeProgrammingPin:
		minAttr, maxAttr = lockdef.AttrMinPinCodeLength, lockdef.AttrMaxPinCodeLength
	case lockdef.CredentialTypeRfid:
		minAttr, maxAttr = lockdef.AttrMinRfidCodeLength, lockdef.AttrMaxRfidCodeLength
	default:
		return true
	}
	lo, okMin := s.uint8Attr(minAttr)
	hi, okMax := s.uint8Attr(maxAttr)
	return okMin && okMax && len(data) >= int(lo) && len(data) <= int(hi)
}

// credentialTypeToLockDataType mirrors matter.js LockAuth.ts:104-121.
func credentialTypeToLockDataType(t lockdef.CredentialTypeEnum) lockdef.LockDataTypeEnum {
	switch t {
	case lockdef.CredentialTypeProgrammingPin:
		return lockdef.LockDataTypeProgrammingCode
	case lockdef.CredentialTypePin:
		return lockdef.LockDataTypePin
	case lockdef.CredentialTypeRfid:
		return lockdef.LockDataTypeRfid
	case lockdef.CredentialTypeFingerprint:
		return lockdef.LockDataTypeFingerprint
	case lockdef.CredentialTypeFingerVein:
		return lockdef.LockDataTypeFingerVein
	case lockdef.CredentialTypeFace:
		return lockdef.LockDataTypeFace
	default:
		return lockdef.LockDataTypeUnspecified
	}
}

// findCredential is LockAuth.Store.findCredential (:191-193).
func (s *DoorLockServer) findCredential(credType uint8, index uint16) *store.DoorLockCredential {
	for i := range s.data.Credentials {
		if c := &s.data.Credentials[i]; c.Type == credType && c.Index == index {
			return c
		}
	}
	return nil
}

// findNextAvailableCredentialIndex is LockAuth.Store :195-203.
func (s *DoorLockServer) findNextAvailableCredentialIndex(credType uint8, after, maxIndex uint16) *uint16 {
	for i := uint32(after) + 1; i <= uint32(maxIndex); i++ {
		if s.findCredential(credType, uint16(i)) == nil { //nolint:gosec // i <= maxIndex, a uint16
			idx := uint16(i) //nolint:gosec // as above
			return &idx
		}
	}
	return nil
}

// findNextOccupiedCredentialIndex is LockAuth.Store :205-215.
func (s *DoorLockServer) findNextOccupiedCredentialIndex(credType uint8, after uint16) *uint16 {
	var next *uint16
	for _, c := range s.data.Credentials {
		if c.Type == credType && c.Index > after && (next == nil || c.Index < *next) {
			idx := c.Index
			next = &idx
		}
	}
	return next
}

// isDuplicateCredential is LockAuth.Store :217-225: another credential of
// the type carries the same data.
func (s *DoorLockServer) isDuplicateCredential(credType uint8, data []byte, exclude uint16) bool {
	for _, c := range s.data.Credentials {
		if c.Type == credType && c.Index != exclude && s.matches(c, data) {
			return true
		}
	}
	return false
}

// matches reports whether a stored credential decrypts to data.
func (s *DoorLockServer) matches(c store.DoorLockCredential, data []byte) bool {
	plain, err := s.cipher.Decrypt(c.Data)
	return err == nil && subtle.ConstantTimeCompare(plain, data) == 1
}

// addCredentialLocked is LockAuth.Store.addCredential (:229-238).
func (s *DoorLockServer) addCredentialLocked(credType uint8, index uint16, data []byte, fabricIndex uint8) error {
	enc, err := s.cipher.Encrypt(data)
	if err != nil {
		return spec.Errorf(im.StatusFailure, "lock: encrypt the credential: %v", err)
	}
	s.data.Credentials = append(s.data.Credentials, store.DoorLockCredential{
		Type: credType, Index: index, Data: enc, CreatorFabricIndex: fabricIndex, LastModifiedFabricIndex: fabricIndex,
	})
	s.dirty = true
	return nil
}

// updateCredentialDataLocked is LockAuth.Store.updateCredentialData (:240-247).
func (s *DoorLockServer) updateCredentialDataLocked(credType uint8, index uint16, data []byte, fabricIndex uint8) error {
	enc, err := s.cipher.Encrypt(data)
	if err != nil {
		return spec.Errorf(im.StatusFailure, "lock: encrypt the credential: %v", err)
	}
	if c := s.findCredential(credType, index); c != nil {
		c.Data, c.LastModifiedFabricIndex = enc, fabricIndex
		s.dirty = true
	}
	return nil
}

// removeCredentialLocked is LockAuth.Store.removeCredential (:249-253).
func (s *DoorLockServer) removeCredentialLocked(credType uint8, index uint16) {
	s.data.Credentials = slices.DeleteFunc(s.data.Credentials, func(c store.DoorLockCredential) bool {
		return c.Type == credType && c.Index == index
	})
	s.dirty = true
}
