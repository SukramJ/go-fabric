// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"

	"github.com/SukramJ/go-fabric/im"
)

// Vendor ID verification (Matter 1.4.2 §6.4.10, OperationalCredentials
// commands SetVIDVerificationStatement 0x0C and SignVIDVerificationRequest
// 0x0D), ported from matter.js OperationalCredentialsServer
// setVidVerificationStatement / signVidVerificationRequest,
// Fabric.updateVendorVerificationData and VendorIdVerification.dataToSign.

const (
	// vidVerificationStatementSize is matter.js VERIFICATION_STATEMENT_SIZE.
	vidVerificationStatementSize = 85
	// fabricBindingVersion is matter.js FABRIC_BINDING_VERSION
	// (MATTER_CRYPTO_PRIMITIVES_VERSION).
	fabricBindingVersion uint8 = 1
	// vidClientChallengeSize is the ClientChallenge length the schema
	// fixes (octstr, 32).
	vidClientChallengeSize = 32
)

// vidVerificationSettings is the optional capability of a [StoreFacade] to
// persist a fabric's vendor verification data (store.Store has it); the
// data then survives a restart, as matter.js persists it with the fabric.
// Without it the data lives in memory.
type vidVerificationSettings interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
}

// fabricVendorIDUpdater is the optional capability of a [StoreFacade] to
// change a fabric's vendor id (SetVIDVerificationStatement.VendorID).
type fabricVendorIDUpdater interface {
	UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error
}

type vidData struct {
	statement []byte
	vvsc      []byte
}

type vidVerificationMemory struct {
	mu     sync.Mutex
	fabric map[uint8]vidData
}

func vidSettingKey(fabricIndex uint8, field string) string {
	return "matter.fabric." + strconv.Itoa(int(fabricIndex)) + ".vid." + field
}

// vidVerification returns the fabric's statement and VVSC (nil when unset).
func (o *OperationalCredentials) vidVerification(ctx context.Context, fabricIndex uint8) vidData {
	if st, ok := o.store.(vidVerificationSettings); ok {
		var d vidData
		if v, ok, err := st.GetSetting(ctx, vidSettingKey(fabricIndex, "statement")); err == nil && ok && v != "" {
			d.statement, _ = hex.DecodeString(v)
		}
		if v, ok, err := st.GetSetting(ctx, vidSettingKey(fabricIndex, "vvsc")); err == nil && ok && v != "" {
			d.vvsc, _ = hex.DecodeString(v)
		}
		return d
	}
	o.vid.mu.Lock()
	defer o.vid.mu.Unlock()
	return o.vid.fabric[fabricIndex]
}

func (o *OperationalCredentials) storeVidVerification(ctx context.Context, fabricIndex uint8, d vidData) error {
	if st, ok := o.store.(vidVerificationSettings); ok {
		if err := st.SetSetting(ctx, vidSettingKey(fabricIndex, "statement"), hex.EncodeToString(d.statement)); err != nil {
			return err
		}
		return st.SetSetting(ctx, vidSettingKey(fabricIndex, "vvsc"), hex.EncodeToString(d.vvsc))
	}
	o.vid.mu.Lock()
	defer o.vid.mu.Unlock()
	if o.vid.fabric == nil {
		o.vid.fabric = map[uint8]vidData{}
	}
	if len(d.statement) == 0 && len(d.vvsc) == 0 {
		delete(o.vid.fabric, fabricIndex)
	} else {
		o.vid.fabric[fabricIndex] = d
	}
	return nil
}

func (o *OperationalCredentials) forgetVidVerification(fabricIndex uint8) {
	_ = o.storeVidVerification(context.Background(), fabricIndex, vidData{})
}

// isValidVendorID is matter.js VendorId.isValid: 0..0xFFF4.
func isValidVendorID(v uint16) bool { return v <= 0xFFF4 }

// handleSetVidVerificationStatement is matter.js setVidVerificationStatement
// with Fabric.updateVendorVerificationData, for the accessing fabric: at
// least one field (InvalidCommand), a valid VendorID (ConstraintError), a
// VVSC only on a fabric without ICAC (InvalidCommand), a statement of 0
// (clears) or 85 bytes (ConstraintError otherwise), an empty VVSC clears.
func (o *OperationalCredentials) handleSetVidVerificationStatement(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(SetVidVerificationStatementRequest)
	if !ok {
		return nil, fmt.Errorf("%w: SetVidVerificationStatementRequest expected, got %T", errOpcredsInvalidArg, fields)
	}
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	if fabricIndex == 0 {
		return nil, opcredsUnsupportedAccessErr{"matter: SetVIDVerificationStatement needs an accessing fabric"}
	}
	if !req.HasVendorID && !req.HasVidVerificationStatement && !req.HasVvsc {
		return nil, opcredsInvalidCommandErr{"matter: SetVIDVerificationStatement: at least one of VendorID, VIDVerificationStatement or VVSC must be provided"}
	}
	if req.HasVendorID && !isValidVendorID(req.VendorID) {
		return nil, opcredsConstraintErr{msg: fmt.Sprintf("matter: SetVIDVerificationStatement: invalid VendorID 0x%04X", req.VendorID)}
	}
	if req.HasVvsc {
		id, err := o.store.GetIdentity(ctx, fabricIndex)
		if err != nil {
			return nil, fmt.Errorf("matter: SetVIDVerificationStatement: identity: %w", err)
		}
		if len(id.ICAC) > 0 {
			return nil, opcredsInvalidCommandErr{"matter: SetVIDVerificationStatement: a VVSC is only allowed without an ICAC"}
		}
	}
	d := o.vidVerification(ctx, fabricIndex)
	if req.HasVidVerificationStatement {
		switch len(req.VidVerificationStatement) {
		case 0:
			d.statement = nil
		case vidVerificationStatementSize:
			d.statement = append([]byte(nil), req.VidVerificationStatement...)
		default:
			return nil, opcredsConstraintErr{msg: "matter: SetVIDVerificationStatement: the statement must be 0 or 85 bytes long"}
		}
	}
	if req.HasVvsc {
		d.vvsc = nil
		if len(req.Vvsc) > 0 {
			d.vvsc = append([]byte(nil), req.Vvsc...)
		}
	}
	if req.HasVendorID {
		up, ok := o.store.(fabricVendorIDUpdater)
		if !ok {
			return nil, opcredsInvalidCommandErr{"matter: SetVIDVerificationStatement: the store cannot change a fabric's vendor id"}
		}
		if err := up.UpdateFabricVendorID(ctx, fabricIndex, req.VendorID); err != nil {
			return nil, fmt.Errorf("matter: SetVIDVerificationStatement: vendor id: %w", err)
		}
	}
	if err := o.storeVidVerification(ctx, fabricIndex, d); err != nil {
		return nil, fmt.Errorf("matter: SetVIDVerificationStatement: persist: %w", err)
	}
	o.dataVersion.Bump()
	return nil, nil
}

// handleSignVidVerificationRequest is matter.js signVidVerificationRequest:
// the fabric must exist (ConstraintError); the response signs
// VendorIdVerification.dataToSign with the fabric's operational key.
func (o *OperationalCredentials) handleSignVidVerificationRequest(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(SignVidVerificationRequest)
	if !ok {
		return nil, fmt.Errorf("%w: SignVidVerificationRequest expected, got %T", errOpcredsInvalidArg, fields)
	}
	if len(req.ClientChallenge) != vidClientChallengeSize {
		return nil, opcredsConstraintErr{msg: "matter: SignVIDVerificationRequest: ClientChallenge must be 32 bytes"}
	}
	fabric, err := o.store.GetFabric(ctx, req.FabricIndex)
	if err != nil {
		return nil, opcredsConstraintErr{msg: fmt.Sprintf("matter: SignVIDVerificationRequest: fabric %d does not exist", req.FabricIndex)}
	}
	id, err := o.store.GetIdentity(ctx, req.FabricIndex)
	if err != nil {
		return nil, fmt.Errorf("matter: SignVIDVerificationRequest: identity: %w", err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), id.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("matter: SignVIDVerificationRequest: operational key: %w", err)
	}
	challenge := InvokeAttestationChallengeFromContext(ctx)
	if challenge == nil {
		o.mu.RLock()
		challenge = append([]byte(nil), o.attestationChalleng...)
		o.mu.RUnlock()
	}
	tbs := vidVerificationTBS(req.ClientChallenge, challenge, req.FabricIndex, fabric.RootPublicKey, fabric.FabricID, fabric.VendorID,
		o.vidVerification(ctx, req.FabricIndex).statement)
	digest := sha256.Sum256(tbs)
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		return nil, fmt.Errorf("matter: SignVIDVerificationRequest: sign: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return SignVidVerificationResponse{FabricIndex: req.FabricIndex, FabricBindingVersion: fabricBindingVersion, Signature: sig}, nil
}

// vidVerificationTBS is matter.js VendorIdVerification.dataToSign (big
// endian): version || clientChallenge || attestationChallenge ||
// fabricIndex || the fabric binding message (version || root public key
// || fabric id || vendor id) || the statement, when set.
func vidVerificationTBS(clientChallenge, attChallenge []byte, fabricIndex uint8, rootPublicKey []byte, fabricID uint64, vendorID uint16, statement []byte) []byte {
	out := make([]byte, 0, 1+len(clientChallenge)+len(attChallenge)+1+1+len(rootPublicKey)+8+2+len(statement))
	out = append(out, fabricBindingVersion)
	out = append(out, clientChallenge...)
	out = append(out, attChallenge...)
	out = append(out, fabricIndex, fabricBindingVersion)
	out = append(out, rootPublicKey...)
	out = binary.BigEndian.AppendUint64(out, fabricID)
	out = binary.BigEndian.AppendUint16(out, vendorID)
	return append(out, statement...)
}

type opcredsUnsupportedAccessErr struct{ msg string }

func (e opcredsUnsupportedAccessErr) Error() string { return e.msg }

func (opcredsUnsupportedAccessErr) MatterStatusCode() im.StatusCode {
	return im.StatusUnsupportedAccess
}

// invokeAttestationChallengeKey carries the invoking session's
// AttestationChallenge into command handlers.
type invokeAttestationChallengeKey struct{}

// WithInvokeAttestationChallenge returns ctx carrying the attestation
// challenge of the session the invoke arrived on (Matter §4.13.2.6,
// matter.js session.attestationChallengeKey); the bridge stamps it when its
// session table can name it. SignVIDVerificationRequest signs over it.
func WithInvokeAttestationChallenge(ctx context.Context, challenge []byte) context.Context {
	return context.WithValue(ctx, invokeAttestationChallengeKey{}, append([]byte(nil), challenge...))
}

// InvokeAttestationChallengeFromContext returns the challenge
// [WithInvokeAttestationChallenge] stamped, or nil.
func InvokeAttestationChallengeFromContext(ctx context.Context) []byte {
	if c, ok := ctx.Value(invokeAttestationChallengeKey{}).([]byte); ok {
		return c
	}
	return nil
}
