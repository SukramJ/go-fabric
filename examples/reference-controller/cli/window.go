// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	admincommdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	basicinfodef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// Window parameters, as matter.js chooses them at 0ceb202d.
const (
	// defaultWindowTimeout is openBasicCommissioningWindow's and
	// openEnhancedCommissioningWindow's default commissioningTimeout,
	// Seconds(900) — packages/node/src/behavior/system/commissioning/CommissioningClient.ts:454,477
	// (packages/matter.js/src/device/PairedNode.ts:1376,1403, "15 minutes").
	defaultWindowTimeout = 900 * time.Second
	// windowSaltLen is the enhanced window's salt, crypto.randomBytes(32)
	// (CommissioningClient.ts:492).
	windowSaltLen = 32
	// windowIterations is CRYPTO_PBKDF_ITERATIONS_MIN (CommissioningClient.ts:493;
	// packages/general/src/crypto/CryptoConstants.ts:24 = 1000).
	windowIterations = spake2.IterationsMin
	// maxPasscodeAttempts is MAX_PASSCODE_GENERATION_ATTEMPTS
	// (packages/protocol/src/session/pase/PaseClient.ts:33).
	maxPasscodeAttempts = 100
)

// cmdWindow dispatches "window open" and "window revoke".
func cmdWindow(e *env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: %s", commands["window"].usage)
	}
	switch args[0] {
	case "open":
		return cmdWindowOpen(e, args[1:])
	case "revoke":
		return cmdWindowRevoke(e, args[1:])
	}
	return fmt.Errorf("usage: %s", commands["window"].usage)
}

// cmdWindowOpen opens a commissioning window on a paired node so a second
// administrator can add it — matter.js CommissioningClient
// openEnhancedCommissioningWindow / openBasicCommissioningWindow
// (packages/node/src/behavior/system/commissioning/CommissioningClient.ts:454-510
// at 0ceb202d; PairedNode.ts:1376-1420 the same for the legacy API).
func cmdWindowOpen(e *env, args []string) error {
	args, timeout, err := windowTimeout(args)
	if err != nil {
		return err
	}
	var basic bool
	pos, err := parseArgs(e, "window open", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&basic, "basic", false, "open a basic window (the device's own passcode) instead of an enhanced one")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: %s", commands["window"].usage)
	}
	return withStack(e, func(s *stack) error {
		n, id, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		if basic {
			if err := openBasicWindow(ctx, n, uint16(timeout)); err != nil { //nolint:gosec // windowTimeout parses 16 bits
				return err
			}
			e.printf("basic commissioning window open on node %d (0x%016X) for %d s: commission with the device's own code\n", id, id, timeout)
			return nil
		}
		w, err := openEnhancedWindow(ctx, n, uint16(timeout)) //nolint:gosec // windowTimeout parses 16 bits
		if err != nil {
			return err
		}
		e.printf("commissioning window open on node %d (0x%016X) for %d s\n", id, id, timeout)
		e.printf("discriminator: %d\n", w.discriminator)
		e.printf("passcode:      %d\n", w.passcode)
		e.printf("manual code:   %s\n", w.manual)
		e.printf("QR code:       %s\n", w.qr)
		return nil
	})
}

// windowTimeout takes "window open"'s --timeout (seconds, default 900) out
// of args. The global --timeout (the network deadline) has the same name,
// so after "window open" the flag is the window's; the network deadline
// goes before the command.
func windowTimeout(args []string) (rest []string, seconds uint, err error) {
	seconds = uint(defaultWindowTimeout / time.Second)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(rest, args[i:]...), seconds, nil
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || name != "timeout" {
			rest = append(rest, a)
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return nil, 0, errors.New("--timeout needs a value (seconds)")
			}
			i++
			value = args[i]
		}
		v, perr := strconv.ParseUint(value, 10, 16)
		if perr != nil || v == 0 {
			return nil, 0, fmt.Errorf("--timeout %q: want 1 to 65535 seconds", value)
		}
		seconds = uint(v)
	}
	return rest, seconds, nil
}

// cmdWindowRevoke closes a node's commissioning window — RevokeCommissioning.
// No open window (WindowNotOpen) is not an error, as matter.js's
// #revokeStaleCommissioningWindow accepts it (CommissioningClient.ts:538-549).
func cmdWindowRevoke(e *env, args []string) error {
	pos, err := parseArgs(e, "window revoke", args, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: %s", commands["window"].usage)
	}
	return withStack(e, func(s *stack) error {
		n, id, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		open, err := revokeWindow(ctx, n)
		if err != nil {
			return err
		}
		if open {
			e.printf("commissioning window on node %d (0x%016X) revoked\n", id, id)
		} else {
			e.printf("node %d (0x%016X) had no commissioning window open\n", id, id)
		}
		return nil
	})
}

// enhancedWindow is what openEnhancedWindow opened — matter.js
// CommissioningClient.EnhancedCommissioningWindow (CommissioningClient.ts:788, returned at :521-533).
type enhancedWindow struct {
	discriminator uint16
	passcode      uint32
	manual, qr    string
}

func adminCommissioningPath(cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: 0, Cluster: admincommdef.ClusterID, Command: cmd}
}

// AdministratorCommissioning's three commands are timed (schema/timed_gen.go,
// from the matter.js schema): every invoke sends a TimedRequest first.
var timedInvoke = interaction.InvokeOptions{Timed: true}

// revokeWindow sends RevokeCommissioning; open is false when the node
// answered WindowNotOpen — the error #revokeStaleCommissioningWindow
// swallows: Status Failure with cluster status WindowNotOpen
// (CommissioningClient.ts:576-587; PairedNode.ts:1390-1400).
func revokeWindow(ctx context.Context, n *client.Node) (open bool, err error) {
	_, err = n.Invoke(ctx, adminCommissioningPath(admincommdef.CmdRevokeCommissioning), nil, timedInvoke)
	var se *interaction.StatusError
	if errors.As(err, &se) && se.Status == im.StatusFailure && se.ClusterStatus != nil &&
		admincommdef.StatusCodeEnum(*se.ClusterStatus) == admincommdef.StatusCodeWindowNotOpen {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("RevokeCommissioning: %w", err)
	}
	return true, nil
}

// openBasicWindow mirrors openBasicCommissioningWindow
// (CommissioningClient.ts:454-465): the node must advertise the Basic
// (BC) feature, a stale window is revoked, then
// OpenBasicCommissioningWindow.
func openBasicWindow(ctx context.Context, n *client.Node, timeout uint16) error {
	features, err := readUint(ctx, n, admincommdef.ClusterID, spec.AttrFeatureMap)
	if err != nil {
		return fmt.Errorf("AdministratorCommissioning FeatureMap: %w", err)
	}
	if features&uint64(admincommdef.FeatureBasic) == 0 {
		return errors.New("the node does not support the basic commissioning method (AdministratorCommissioning feature BC); open an enhanced window instead")
	}
	if _, err := revokeWindow(ctx, n); err != nil {
		return err
	}
	if _, err := n.Invoke(ctx, adminCommissioningPath(admincommdef.CmdOpenBasicCommissioningWindow),
		admincommdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: timeout}, timedInvoke); err != nil {
		return fmt.Errorf("OpenBasicCommissioningWindow: %w", err)
	}
	return nil
}

// openEnhancedWindow mirrors openEnhancedCommissioningWindow
// (CommissioningClient.ts:475-534): BasicInformation's vendor and product
// ids are read first (they go into the QR code, and matter.js fails before
// opening anything without them), a stale window is revoked, a random
// discriminator, passcode and salt are drawn, the PAKE verifier is derived
// at CRYPTO_PBKDF_ITERATIONS_MIN, OpenCommissioningWindow is sent, and the
// manual code (Standard flow, no vendor and product) and the QR code
// (version 0, Standard flow, on-IP discovery) are returned.
func openEnhancedWindow(ctx context.Context, n *client.Node, timeout uint16) (enhancedWindow, error) {
	vendor, err := readUint(ctx, n, basicinfodef.ClusterID, basicinfodef.AttrVendorId)
	if err != nil {
		return enhancedWindow{}, fmt.Errorf("BasicInformation VendorID: %w", err)
	}
	product, err := readUint(ctx, n, basicinfodef.ClusterID, basicinfodef.AttrProductId)
	if err != nil {
		return enhancedWindow{}, fmt.Errorf("BasicInformation ProductID: %w", err)
	}
	if _, err := revokeWindow(ctx, n); err != nil {
		return enhancedWindow{}, err
	}
	discriminator, err := randomDiscriminator()
	if err != nil {
		return enhancedWindow{}, err
	}
	passcode, err := randomPasscode()
	if err != nil {
		return enhancedWindow{}, err
	}
	salt := make([]byte, windowSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return enhancedWindow{}, err
	}
	verifier, err := pakePasscodeVerifier(passcode, salt, windowIterations)
	if err != nil {
		return enhancedWindow{}, err
	}
	if _, err := n.Invoke(ctx, adminCommissioningPath(admincommdef.CmdOpenCommissioningWindow),
		admincommdef.OpenCommissioningWindowRequest{
			CommissioningTimeout: timeout, PakePasscodeVerifier: verifier,
			Discriminator: discriminator, Iterations: windowIterations, Salt: salt,
		}, timedInvoke); err != nil {
		return enhancedWindow{}, fmt.Errorf("OpenCommissioningWindow: %w", err)
	}
	manual, err := setup.ManualCode(discriminator, passcode)
	if err != nil {
		return enhancedWindow{}, err
	}
	qr, err := setup.QRCode(setup.Payload{
		Version: 0, VendorID: uint16(vendor), ProductID: uint16(product), //nolint:gosec // 16-bit attributes
		DiscoveryCaps: setup.DiscoveryOnIP, Discriminator: discriminator, Passcode: passcode,
	})
	if err != nil {
		return enhancedWindow{}, err
	}
	return enhancedWindow{discriminator: discriminator, passcode: passcode, manual: manual, qr: qr}, nil
}

// readUint reads one root-endpoint attribute as an unsigned integer.
func readUint(ctx context.Context, n *client.Node, cluster, attribute uint32) (uint64, error) {
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}}, IncludeKnownVersions: true})
	if err != nil {
		return 0, err
	}
	if v, ok := rep.Value(0, cluster, attribute); ok && !v.El.IsNull && !v.El.IsContainer {
		return v.El.Uint, nil
	}
	if len(rep.Statuses) > 0 {
		return 0, fmt.Errorf("read: %s", rep.Statuses[0].Status)
	}
	return 0, errors.New("read: no value")
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
	return 0, fmt.Errorf("no valid passcode in %d tries: the entropy source is broken", maxPasscodeAttempts)
}

// pakePasscodeVerifier is PaseClient.generatePakePasscodeVerifier:
// w0 as a 32-byte big-endian scalar followed by L, the 65-byte
// uncompressed point (PaseClient.ts:50-53) — the layout
// spake2.NewVerifierFromValue splits on the device side.
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
