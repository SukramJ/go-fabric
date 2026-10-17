// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command dclsnapshot refreshes controller/dcl/snapshot/data from the CSA
// Distributed Compliance Ledger: `make update-dcl-snapshot`. It is a review
// step like the schema refresh, run by hand, never automatically: read the
// diff of the data directory before committing it.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "controller/dcl/snapshot/data", "the snapshot directory")
	ledger := flag.String("ledger", dcl.ProductionURL, "the ledger's base URL")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := dcl.NewClient(*ledger)
	client.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	src, err := snapshot.Fetch(ctx, client, snapshot.CDSignerKeyIDs, *out, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d PAAs, %d CD signers from %s at %s\n", *out, src.PAA.ExpectedCount, src.CDSigners.ExpectedCount, src.Ledger, src.BuiltAt.Format(time.RFC3339))
	return nil
}
