// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	admincommdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// Commissioning window parameters, as matter.js chooses them at 862ad75f.
const (
	// DefaultWindowTimeout is the window a [Node.OpenCommissioningWindow]
	// of timeout 0 opens: openEnhancedCommissioningWindow's and
	// openBasicCommissioningWindow's Seconds(900)
	// (packages/node/src/behavior/system/commissioning/CommissioningClient.ts:486, :508).
	DefaultWindowTimeout = 900 * time.Second
	// windowSaltLen is the enhanced window's salt, crypto.randomBytes(32)
	// (CommissioningClient.ts:524).
	windowSaltLen = 32
	// windowIterations is CRYPTO_PBKDF_ITERATIONS_MIN
	// (CommissioningClient.ts:525; packages/general/src/crypto/CryptoConstants.ts:24).
	windowIterations = spake2.IterationsMin
	// maxPasscodeAttempts is MAX_PASSCODE_GENERATION_ATTEMPTS
	// (packages/protocol/src/session/pase/PaseClient.ts:33).
	maxPasscodeAttempts = 100
)

// ErrNoBasicWindow is [WindowOptions].Basic on a node whose
// AdministratorCommissioning lacks the Basic (BC) feature — matter.js's
// ImplementationError of openBasicCommissioningWindow
// (CommissioningClient.ts:487-489 at 862ad75f).
var ErrNoBasicWindow = errors.New("client: the node does not support the basic commissioning method")

// WindowOptions configure [Node.OpenCommissioningWindow]; the zero value
// opens an enhanced window.
type WindowOptions struct {
	// Basic opens a Basic Commissioning Window, in which the node takes its
	// own passcode (the one printed on it), in place of an enhanced one
	// with a fresh passcode — openBasicCommissioningWindow. The node must
	// support the Basic feature ([ErrNoBasicWindow]); the window returned
	// carries the timeout alone.
	Basic bool
}

// CommissioningWindow is an open commissioning window — matter.js
// CommissioningClient.EnhancedCommissioningWindow
// (CommissioningClient.ts:820-833 at 862ad75f): the pairing codes, the
// passcode and long discriminator they encode, the node's vendor and
// product id, and the window's timeout. A basic window carries the
// timeout alone; the node's own code commissions it.
type CommissioningWindow struct {
	ManualCode string
	QRCode     string
	Passcode   uint32
	// Discriminator is the 12-bit long discriminator the node advertises
	// while the window is open.
	Discriminator       uint16
	VendorID, ProductID uint16
	Timeout             time.Duration
}

// OpenCommissioningWindow opens a commissioning window on the peer so
// another administrator can commission it for timeout (0 is
// [DefaultWindowTimeout]; at most 65535 s): a window open before is
// revoked first, a WindowNotOpen answer accepted; then
// OpenCommissioningWindow with a random discriminator and passcode and the
// PAKE verifier derived from them over a random 32-byte salt at 1000
// PBKDF2 iterations — CommissioningClient.openEnhancedCommissioningWindow
// (CommissioningClient.ts:507-567 at 862ad75f) with PaseClient's
// generateRandomDiscriminator, generateRandomPasscode and
// generatePakePasscodeVerifier (PaseClient.ts:49-71). The codes it returns
// are the manual code of the Standard flow and the QR code of version 0,
// Standard flow, discovery on the IP network, with the vendor and product
// id of the peer's BasicInformation.
//
// matter.js takes those ids from the node's seeded state and refuses an
// unseeded node; a node whose image lacks them reads them first. With
// opts.Basic it is openBasicCommissioningWindow (:486-497): the Basic
// feature of the image's AdministratorCommissioning (read when the image
// lacks it), the stale window revoked, OpenBasicCommissioningWindow.
// AdministratorCommissioning's commands are timed.
func (n *Node) OpenCommissioningWindow(ctx context.Context, timeout time.Duration, opts WindowOptions) (CommissioningWindow, error) {
	if timeout == 0 {
		timeout = DefaultWindowTimeout
	}
	secs := timeout / time.Second
	if secs < 1 || secs > 0xFFFF {
		return CommissioningWindow{}, fmt.Errorf("client: commissioning window of %v, want 1 to 65535 s", timeout)
	}
	t := uint16(secs) //nolint:gosec // bounded above
	if opts.Basic {
		return n.openBasicWindow(ctx, t)
	}
	vendor, err := n.rootUint(ctx, basicinformation.ClusterID, basicinformation.AttrVendorId)
	if err != nil {
		return CommissioningWindow{}, fmt.Errorf("client: BasicInformation VendorID: %w", err)
	}
	product, err := n.rootUint(ctx, basicinformation.ClusterID, basicinformation.AttrProductId)
	if err != nil {
		return CommissioningWindow{}, fmt.Errorf("client: BasicInformation ProductID: %w", err)
	}
	if _, err := n.RevokeCommissioningWindow(ctx); err != nil {
		return CommissioningWindow{}, err
	}
	discriminator, err := randomDiscriminator()
	if err != nil {
		return CommissioningWindow{}, err
	}
	passcode, err := randomPasscode()
	if err != nil {
		return CommissioningWindow{}, err
	}
	salt := make([]byte, windowSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return CommissioningWindow{}, err
	}
	verifier, err := pakePasscodeVerifier(passcode, salt, windowIterations)
	if err != nil {
		return CommissioningWindow{}, err
	}
	if _, err := n.Invoke(ctx, CommandAt(0, admincommdef.ClusterID, admincommdef.CmdOpenCommissioningWindow),
		admincommdef.OpenCommissioningWindowRequest{
			CommissioningTimeout: t, PakePasscodeVerifier: verifier,
			Discriminator: discriminator, Iterations: windowIterations, Salt: salt,
		}, InvokeOptions{Timed: true}); err != nil {
		return CommissioningWindow{}, fmt.Errorf("client: OpenCommissioningWindow: %w", err)
	}
	manual, err := setup.ManualCode(discriminator, passcode)
	if err != nil {
		return CommissioningWindow{}, err
	}
	w := CommissioningWindow{
		ManualCode: manual, Passcode: passcode, Discriminator: discriminator,
		VendorID: uint16(vendor), ProductID: uint16(product), Timeout: time.Duration(t) * time.Second, //nolint:gosec // 16-bit attributes
	}
	if w.QRCode, err = setup.QRCode(setup.Payload{
		VendorID: w.VendorID, ProductID: w.ProductID, DiscoveryCaps: setup.DiscoveryOnIP, Discriminator: discriminator, Passcode: passcode,
	}); err != nil {
		return CommissioningWindow{}, err
	}
	return w, nil
}

// openBasicWindow is openBasicCommissioningWindow (CommissioningClient.ts:486-497).
func (n *Node) openBasicWindow(ctx context.Context, timeout uint16) (CommissioningWindow, error) {
	features, err := n.rootUint(ctx, admincommdef.ClusterID, spec.AttrFeatureMap)
	if err != nil {
		return CommissioningWindow{}, fmt.Errorf("client: AdministratorCommissioning FeatureMap: %w", err)
	}
	if features&uint64(admincommdef.FeatureBasic) == 0 {
		return CommissioningWindow{}, ErrNoBasicWindow
	}
	if _, err := n.RevokeCommissioningWindow(ctx); err != nil {
		return CommissioningWindow{}, err
	}
	if _, err := n.Invoke(ctx, CommandAt(0, admincommdef.ClusterID, admincommdef.CmdOpenBasicCommissioningWindow),
		admincommdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: timeout}, InvokeOptions{Timed: true}); err != nil {
		return CommissioningWindow{}, fmt.Errorf("client: OpenBasicCommissioningWindow: %w", err)
	}
	return CommissioningWindow{Timeout: time.Duration(timeout) * time.Second}, nil
}

// RevokeCommissioningWindow closes the peer's commissioning window with
// RevokeCommissioning (timed); open is false when the peer answered
// WindowNotOpen (Failure with that cluster status), which is no error —
// CommissioningClient.#revokeStaleCommissioningWindow
// (CommissioningClient.ts:570-582 at 862ad75f).
func (n *Node) RevokeCommissioningWindow(ctx context.Context) (open bool, err error) {
	_, err = n.Invoke(ctx, CommandAt(0, admincommdef.ClusterID, admincommdef.CmdRevokeCommissioning), nil, InvokeOptions{Timed: true})
	var se *StatusError
	if errors.As(err, &se) && se.Status == im.StatusFailure && se.ClusterStatus != nil &&
		admincommdef.StatusCodeEnum(*se.ClusterStatus) == admincommdef.StatusCodeWindowNotOpen {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("client: RevokeCommissioning: %w", err)
	}
	return true, nil
}

// rootUint is an unsigned attribute of the root endpoint: the image's, or
// read from the peer (the read applies it to the image) when the image
// lacks it.
func (n *Node) rootUint(ctx context.Context, cluster, attribute uint32) (uint64, error) {
	if ep := n.Endpoint(0); ep != nil {
		if v, ok := ep.Cluster(cluster).Value(attribute); ok && isUint(v) {
			return v.El.Uint, nil
		}
	}
	var req ReadRequest
	req.Attributes = []AttributePath{AttributeAt(0, cluster, attribute)}
	req.IncludeKnownVersions = true
	rep, err := n.Read(ctx, req)
	if err != nil {
		return 0, err
	}
	if v, ok := rep.Value(0, cluster, attribute); ok && isUint(v) {
		return v.El.Uint, nil
	}
	if len(rep.Statuses) > 0 {
		return 0, fmt.Errorf("client: read: %s", rep.Statuses[0].Status)
	}
	return 0, errors.New("client: read: no value")
}

// randomDiscriminator is PaseClient.generateRandomDiscriminator:
// crypto.randomUint16 % 4096 (PaseClient.ts:69-71).
func randomDiscriminator() (uint16, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b[:]) % 4096, nil
}

// randomPasscode is PaseClient.generateRandomPasscode: 27-bit candidates,
// invalid ones rejected (no modulo bias), at most
// MAX_PASSCODE_GENERATION_ATTEMPTS tries (PaseClient.ts:55-67).
func randomPasscode() (uint32, error) {
	var b [4]byte
	for range maxPasscodeAttempts {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		if p := binary.LittleEndian.Uint32(b[:]) & 0x07FF_FFFF; setup.IsValidPasscode(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("client: no valid passcode in %d tries: the entropy source is broken", maxPasscodeAttempts)
}

// pakePasscodeVerifier is PaseClient.generatePakePasscodeVerifier: w0 as a
// 32-byte big-endian scalar followed by L, the 65-byte uncompressed point
// (PaseClient.ts:49-52) — the layout spake2.NewVerifierFromValue splits on
// the device side.
func pakePasscodeVerifier(passcode uint32, salt []byte, iterations int) ([]byte, error) {
	vc, err := spake2.NewVerifierContext(passcode, salt, iterations)
	if err != nil {
		return nil, err
	}
	l, err := vc.L.Bytes()
	if err != nil {
		return nil, err
	}
	out := make([]byte, spake2.VerifierW0Size, spake2.VerifierW0Size+spake2.VerifierLSize)
	vc.W0.FillBytes(out)
	return append(out, l...), nil
}
