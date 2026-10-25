// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command quickstart-controller is the smallest Matter controller on
// go-fabric: it keeps one fabric in a SQLite file, pairs a node by its QR or
// manual code, reads its NodeLabel, toggles its endpoint 1 OnOff and prints
// the OnOff reports of a subscription until Ctrl-C.
// docs/getting-started/controller.md walks through it; the reference
// controller (examples/reference-controller) is the complete tool.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
)

// openNames opens the DNS-SD cache; a test replaces it.
var openNames = discovery.Open

func main() {
	dbPath := flag.String("db", "quickstart-controller.db", "the SQLite file holding the fabric and the paired nodes")
	code := flag.String("code", "", "pair the node with this QR (MT:…) or manual code first")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *dbPath, *code)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, code string) error {
	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	c, err := open(ctx, db, "[::]:0", slog.Default())
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	if code != "" {
		noSubscribe := false // this program exits; a resident host keeps the default subscription
		if _, err := c.Pair(ctx, code, controller.PairOptions{AutoSubscribe: &noSubscribe}); err != nil {
			return err
		}
	}
	nodes := c.Nodes()
	if len(nodes) == 0 {
		return errors.New("no paired node: pass -code")
	}
	return use(ctx, nodes[0])
}

// openDB opens the SQLite file the controller keeps its fabric in, with
// the module's own driver.
func openDB(path string) (*sql.DB, error) { return controller.OpenDB(path) }

// open builds the controller over the database: the fabric (created on the
// first run, loaded afterwards), the socket, DNS-SD, peers, commissioner,
// registry — one call. The trust store is the CSA's test PAAs, for
// development devices; a production controller passes a DCL-backed one.
func open(ctx context.Context, db *sql.DB, listen string, log *slog.Logger) (*controller.Controller, error) {
	return controller.New(ctx, controller.Config{
		DB:          db,
		Attestation: attestation.TestStore(),
		Listen:      listen,
		Label:       "quickstart",
		Logger:      log,
		Names:       openNames,
		ManualStart: true, // the program reads and subscribes itself
	})
}

// use reads, invokes and subscribes through the node's client image.
func use(ctx context.Context, n *client.Node) error {
	var read client.ReadRequest
	read.Attributes = []client.AttributePath{client.ClusterAt(0, basicinfo.ClusterID)}
	if _, err := n.Read(ctx, read); err != nil {
		return err
	}
	// The typed read: the image's values as the generated State.
	if info, _ := basicinfo.StateFrom(n.Endpoint(0).Cluster(basicinfo.ClusterID)); info.NodeLabel != "" {
		fmt.Printf("NodeLabel: %s\n", info.NodeLabel)
	}
	toggle := client.CommandAt(1, onoffdef.ClusterID, onoffdef.CmdToggle)
	if _, err := n.Invoke(ctx, toggle, onoffdef.ToggleRequest{}, client.InvokeOptions{}); err != nil {
		return err
	}
	sub, err := n.Subscribe(ctx, client.SubscribeRequest{
		Attributes:         []client.AttributePath{client.AttributeAt(1, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		MaxIntervalCeiling: client.Interval(time.Minute),
		Updated: func(r *client.Report) { // keep it short: it runs on the report's exchange
			if _, ok := r.Value(1, onoffdef.ClusterID, onoffdef.AttrOnOff); ok {
				st, _ := onoffdef.StateFrom(r.Values(1, onoffdef.ClusterID))
				fmt.Printf("OnOff: %v\n", st.OnOff)
			}
		},
	})
	if err != nil {
		return err
	}
	defer sub.Close()
	select {
	case <-ctx.Done():
	case <-sub.Done():
	}
	return nil
}
