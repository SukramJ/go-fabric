// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
)

// defaultWindowTimeout is the window "window open" opens without
// --timeout: the facade's client.DefaultWindowTimeout, matter.js's 900 s.
const defaultWindowTimeout = client.DefaultWindowTimeout

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
// administrator can add it, through the facade's
// client.Node.OpenCommissioningWindow (matter.js CommissioningClient
// openEnhancedCommissioningWindow / openBasicCommissioningWindow).
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
		w, err := n.OpenCommissioningWindow(ctx, time.Duration(timeout)*time.Second, client.WindowOptions{Basic: basic}) //nolint:gosec // windowTimeout parses 16 bits
		if err != nil {
			return err
		}
		if basic {
			e.printf("basic commissioning window open on node %d (0x%016X) for %d s: commission with the device's own code\n", id, id, timeout)
			return nil
		}
		e.printf("commissioning window open on node %d (0x%016X) for %d s\n", id, id, timeout)
		e.printf("discriminator: %d\n", w.Discriminator)
		e.printf("passcode:      %d\n", w.Passcode)
		e.printf("manual code:   %s\n", w.ManualCode)
		e.printf("QR code:       %s\n", w.QRCode)
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

// cmdWindowRevoke closes a node's commissioning window — RevokeCommissioning
// through client.Node.RevokeCommissioningWindow. No open window
// (WindowNotOpen) is not an error, as matter.js's
// #revokeStaleCommissioningWindow accepts it.
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
		open, err := n.RevokeCommissioningWindow(ctx)
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
