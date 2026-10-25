// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// cmdDiscover searches DNS-SD for the commissionable devices a code or a
// discriminator selects (every one with neither) until the network
// deadline (--timeout) passes, or with --first until the first answers
// (controller.DiscoverWith, DiscoverOptions.First), and prints each.
func cmdDiscover(e *env, args []string) error {
	var code string
	discriminator := -1
	var first bool
	pos, err := parseArgs(e, "discover", args, func(fs *flag.FlagSet) {
		fs.StringVar(&code, "code", "", "the QR code (MT:…) or the manual code whose discriminator selects the device")
		fs.IntVar(&discriminator, "discriminator", -1, "the 12-bit long discriminator")
		fs.BoolVar(&first, "first", false, "stop at the first device that answers")
	})
	if err != nil {
		return err
	}
	if err := wantArgs("discover", pos, 0, 0); err != nil {
		return err
	}
	id := discovery.AnyCommissionable()
	switch {
	case code != "" && discriminator >= 0:
		return errors.New("--code and --discriminator both select the device: give one")
	case code != "":
		pc, err := controller.ParsePairingCode(code)
		if err != nil {
			return err
		}
		id = pc.Identifier()
	case discriminator >= 0:
		if discriminator > setup.MaxDiscriminator {
			return fmt.Errorf("--discriminator %d out of range", discriminator)
		}
		id = discovery.ByLongDiscriminator(uint16(discriminator))
	}
	return withStack(e, func(s *stack) error {
		found, err := s.ctl.DiscoverWith(e.ctx, id, e.opts.timeout.or(defaultTimeout), controller.DiscoverOptions{First: first})
		if err != nil {
			return fmt.Errorf("discovery: %w", err)
		}
		if len(found) == 0 {
			e.println("no commissionable device answered")
		}
		for i := range found {
			d := &found[i]
			addrs := make([]string, 0, len(d.Addresses))
			for _, a := range d.Addresses {
				addrs = append(addrs, a.AddrPort().String())
			}
			vp := "-"
			if d.VP != nil {
				vp = *d.VP
			}
			e.printf("%s discriminator %d vp %s cm %d at %s\n", d.InstanceID, d.D, vp, d.CM, strings.Join(addrs, ", "))
		}
		return nil
	})
}
