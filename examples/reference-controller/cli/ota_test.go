// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx/otaimage"
	"thingfabric.dev/go-fabric/secure/setup"
)

// TestOTAServeUndoesItsSubscriptionOnAnError holds R2-C2-err-2651: "ota
// serve --announce" switches the node's persisted default subscription on
// for its run, and an error after the switch — here the operator declines
// — switches it off again, so a later command on the same database does
// not run the node with a subscription the tool never asked for. Not
// parallel.
func TestOTAServeUndoesItsSubscriptionOnAnError(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	tl.run("pair", "--code", manual, "--node-id", "0x4321")
	image, err := otaimage.Create(otaimage.CreateOptions{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0", Payload: []byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.ota")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	baseContext = func() context.Context { return ctx }
	defer func() { baseContext = context.Background }()
	if out, err := tl.runIn("n\n", "ota", "serve", "--image", path, "--announce", "0x4321"); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("ota serve, declined: %v\n%s", err, out)
	}

	opts := defaultOptions()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	opts.register(fs)
	if err := fs.Parse(tl.args()); err != nil {
		t.Fatal(err)
	}
	e := &env{ctx: ctx, opts: opts, stdin: strings.NewReader(""), stdout: &strings.Builder{}, log: slog.New(slog.DiscardHandler)}
	var auto bool
	if err := withStack(e, func(s *stack) error {
		n, _, err := s.node("0x4321")
		if err != nil {
			return err
		}
		auto = n.AutoSubscribe()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if auto {
		t.Fatal("the declined update left the node's persisted default subscription on")
	}
}
