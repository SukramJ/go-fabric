// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// WiFiNetworkManagement (0x0451) after connectedhomeip
// src/app/clusters/wifi-network-management-server/
// WiFiNetworkManagementCluster.cpp at 6170af84, cited below by line.

// ErrInvalidCredentials is a Wi-Fi credential pair
// [WiFiManagementServer.SetNetworkCredentials] refuses: an SSID outside 1
// to 32 bytes, or a passphrase that is neither 8 to 63 bytes nor 64
// hexadecimal digits (chip's CHIP_ERROR_INVALID_ARGUMENT, :76-77).
var ErrInvalidCredentials = errors.New("netinfra: invalid Wi-Fi network credentials")

// maxSSIDLen is the SSID's capacity (WiFiNetworkManagementCluster.h:67,
// FixedByteBuffer<32>), the attribute's constraint "1 to 32".
const maxSSIDLen = 32

// WiFiManagementConfig carries the WiFiNetworkManagement server's
// construction parameters.
type WiFiManagementConfig struct {
	// Clock is the real-time clock the PassphraseSurrogate follows
	// (WiFiNetworkManagementCluster.cpp:95-99); the system clock when nil.
	Clock clock.Clock
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// WiFiManagementServer implements [contract.ClusterServer] for
// WiFiNetworkManagement. The credentials are the host's state: it sets
// them with [WiFiManagementServer.SetNetworkCredentials] and drops them
// with [WiFiManagementServer.ClearNetworkCredentials]; Ssid and
// PassphraseSurrogate read null without them (:114-117). The passphrase
// is never an attribute, never logged and never part of an error: only
// NetworkPassphraseRequest over a CASE session returns it (:129-138).
type WiFiManagementServer struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, write veto and
	// SetAttributes over srv.
	spec.SelfHeld

	srv   *spec.Server
	clock clock.Clock

	// mu guards ssid, passphrase and surrogate.
	mu         sync.Mutex
	ssid       []byte
	passphrase []byte
	surrogate  uint64
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*WiFiManagementServer)(nil)
	_ contract.ClusterDataVersion             = (*WiFiManagementServer)(nil)
	_ contract.ClusterAttributeLister         = (*WiFiManagementServer)(nil)
	_ contract.ClusterCommandLister           = (*WiFiManagementServer)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*WiFiManagementServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*WiFiManagementServer)(nil)
	_ contract.AttributeChangeNotifier        = (*WiFiManagementServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*WiFiManagementServer)(nil)
)

// NewWiFiManagement builds a WiFiNetworkManagement server without
// credentials: Ssid and PassphraseSurrogate null, FeatureMap 0 (:110-111),
// NetworkPassphraseRequest accepted (:152-159).
func NewWiFiManagement(cfg WiFiManagementConfig) (*WiFiManagementServer, error) {
	s := &WiFiManagementServer{clock: cfg.Clock}
	if s.clock == nil {
		s.clock = clock.System{}
	}
	srv, err := spec.NewServer(wdef.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Initial:     map[uint32]any{wdef.AttrSsid: nil, wdef.AttrPassphraseSurrogate: nil},
		Handlers:    map[uint32]spec.CommandHandler{wdef.CmdNetworkPassphraseRequest: s.networkPassphraseRequest},
	})
	if err != nil {
		return nil, fmt.Errorf("netinfra: %w", err)
	}
	s.Instance, s.SelfHeld, s.srv = srv.Instance, spec.NewSelfHeld(srv), srv
	return s, nil
}

// validWPAPersonalCredential is chip's IsValidWpaPersonalCredential
// (:44-56): a passphrase of 8 to 63 bytes, or a raw PSK of 64 hexadecimal
// digits.
func validWPAPersonalCredential(credential []byte) bool {
	if n := len(credential); n >= 8 && n <= 63 {
		return true
	}
	if len(credential) != 64 {
		return false
	}
	for _, c := range credential {
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// SetNetworkCredentials stores the network's SSID and passphrase
// (WiFiNetworkManagementCluster.cpp:74-103): a pair outside chip's limits
// is [ErrInvalidCredentials] and changes nothing; the same pair again
// reports nothing. A changed SSID reports Ssid; a changed passphrase
// advances PassphraseSurrogate — one past the last, or the real time in
// milliseconds when that is later (:94-99) — and reports it.
func (s *WiFiManagementServer) SetNetworkCredentials(ssid, passphrase []byte) error {
	if len(ssid) < 1 || len(ssid) > maxSSIDLen {
		return fmt.Errorf("%w: SSID length %d outside 1 to %d", ErrInvalidCredentials, len(ssid), maxSSIDLen)
	}
	if !validWPAPersonalCredential(passphrase) {
		// The length only: the passphrase itself never reaches an error.
		return fmt.Errorf("%w: passphrase of %d bytes is no WPA personal credential", ErrInvalidCredentials, len(passphrase))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ssidChanged := !bytes.Equal(s.ssid, ssid)
	passphraseChanged := !bytes.Equal(s.passphrase, passphrase)
	if !ssidChanged && !passphraseChanged {
		return nil
	}
	changes := map[uint32]any{}
	surrogate := s.surrogate
	if ssidChanged {
		changes[wdef.AttrSsid] = slices.Clone(ssid)
	}
	if passphraseChanged {
		surrogate++
		if now := s.clock.Now().UnixMilli(); now > 0 && uint64(now) > surrogate {
			surrogate = uint64(now)
		}
		changes[wdef.AttrPassphraseSurrogate] = surrogate
	}
	if err := s.srv.SetAttributes(changes); err != nil {
		return err
	}
	s.ssid, s.passphrase, s.surrogate = slices.Clone(ssid), slices.Clone(passphrase), surrogate
	return nil
}

// ClearNetworkCredentials drops the credentials (:62-72): Ssid and
// PassphraseSurrogate read null and are reported; without credentials it
// reports nothing. The surrogate's count survives, so the next passphrase
// still advances it.
func (s *WiFiManagementServer) ClearNetworkCredentials() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ssid == nil {
		return nil
	}
	if err := s.srv.SetAttributes(map[uint32]any{wdef.AttrSsid: nil, wdef.AttrPassphraseSurrogate: nil}); err != nil {
		return err
	}
	clear(s.passphrase)
	s.ssid, s.passphrase = nil, nil
	return nil
}

// HasNetworkCredentials reports whether credentials are set
// (WiFiNetworkManagementCluster.h:45).
func (s *WiFiManagementServer) HasNetworkCredentials() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ssid != nil
}

// SSID returns the stored SSID, nil without credentials (.h:49).
func (s *WiFiManagementServer) SSID() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ssid)
}

// Passphrase returns the stored passphrase, nil without credentials
// (.h:53).
func (s *WiFiManagementServer) Passphrase() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.passphrase)
}

// networkPassphraseRequest is chip's NetworkPassphraseRequest
// (:129-138): a session that is not CASE is UNSUPPORTED_ACCESS, a server
// without credentials INVALID_IN_STATE.
func (s *WiFiManagementServer) networkPassphraseRequest(ctx context.Context, _ any) (any, error) {
	if !isCASE(ctx) {
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "netinfra: NetworkPassphraseRequest needs a CASE session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ssid == nil {
		return nil, spec.Errorf(im.StatusInvalidInState, "netinfra: no Wi-Fi network credentials")
	}
	return wdef.NetworkPassphraseResponse{Passphrase: slices.Clone(s.passphrase)}, nil
}

// isCASE reports whether a request arrived over a CASE session: chip's
// `subjectDescriptor.authMode == Access::AuthMode::kCase` (:130). The
// bridge stamps the session's fabric and subject on every request and
// marks a PASE session and a group message (bridge/receive_dispatch.go:
// 511-515, bridge/group_messaging.go:339-350; im.IsPASEFromContext,
// im.GroupSubjectFromContext); a CASE request is one with a fabric and a
// node subject that is neither, as cluster/core's OTA provider tells it.
func isCASE(ctx context.Context) bool {
	if im.IsPASEFromContext(ctx) {
		return false
	}
	if _, group := im.GroupSubjectFromContext(ctx); group {
		return false
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	node, _ := im.SubjectFromContext(ctx)
	return fabric != 0 && node != 0
}

// SetAttributes implements [spec.AttributeSetter]: Ssid and
// PassphraseSurrogate are the credentials' ([spec.ErrNotHeld] for a value
// other than the one served), set through
// [WiFiManagementServer.SetNetworkCredentials].
func (s *WiFiManagementServer) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		cur, _ := s.srv.Value(id)
		if !sameValue(cur, values[id]) {
			return fmt.Errorf("%w: netinfra: WiFiNetworkManagement attribute 0x%04X follows the credentials (SetNetworkCredentials)", spec.ErrNotHeld, id)
		}
	}
	return nil
}

// sameValue compares a stored value with a host's: nil, bytes or a
// comparable scalar.
func sameValue(a, b any) bool {
	if ab, ok := a.([]byte); ok {
		bb, ok := b.([]byte)
		return ok && bytes.Equal(ab, bb)
	}
	if _, ok := b.([]byte); ok {
		return false
	}
	return a == b
}

// --- plumbing -------------------------------------------------------------

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *WiFiManagementServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *WiFiManagementServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead implements [contract.ClusterServer].
func (s *WiFiManagementServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer]: both attributes are
// read-only (:105-121 has no write), which the definition answers.
func (s *WiFiManagementServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer]: a host's handler
// first ([spec.SelfHeld.Handle]), then the server's own.
func (s *WiFiManagementServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *WiFiManagementServer) MatterAcceptedCommands() []uint32 {
	return s.srv.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (s *WiFiManagementServer) MatterGeneratedCommands() []uint32 {
	return s.srv.MatterGeneratedCommands()
}
