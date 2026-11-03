// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllernode_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node/controllernode"
)

// TestObserveInstallsTheScriptedProvider: the update manager's command
// wrappers (softwareupdate.ProviderCommands, the scripted provider of a
// test harness) answer the own node's QueryImage and ApplyUpdateRequest
// for a CASE peer as the script says and report each exchange; a command
// without a CASE peer goes to the provider's own handler, which refuses
// it; fields of another type are INVALID_COMMAND; the function Observe
// returns restores the provider's own handlers; a provider configuration
// without wrappers installs nothing.
func TestObserveInstallsTheScriptedProvider(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "c.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var mu sync.Mutex
	var exchanges []softwareupdate.ProviderExchange
	delay := uint32(5)
	mgr, err := softwareupdate.New(softwareupdate.Config{
		Registry: &client.Registry{}, Images: softwareupdate.NewImages(softwareupdate.ImagesConfig{}),
		ScriptQueryImage: func(softwareupdate.Peer, softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool) {
			return softwareupdate.QueryImageScript{Response: &provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}}, true
		},
		ScriptApplyUpdate: func(softwareupdate.Peer, provdef.ApplyUpdateRequestRequest) (softwareupdate.ApplyUpdateScript, bool) {
			return softwareupdate.ApplyUpdateScript{Action: provdef.ApplyUpdateActionAwaitNextAction, DelayedActionTime: &delay}, true
		},
		OnProviderExchange: func(x softwareupdate.ProviderExchange) {
			mu.Lock()
			exchanges = append(exchanges, x)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := controller.OwnNodeSpec{
		NodeID: 0x43, Listen: "[::1]:0", Advertiser: mdns.NewNoop(),
		Passcode: 20202021, Discriminator: 3840, Salt: make([]byte, 32), Iterations: 1000,
		VendorID: 0xFFF1, ProductID: 0x8000, SerialNumber: "0000000000000043", DB: db,
		Provider: mgr.ProviderConfig(), Logger: slog.New(slog.DiscardHandler),
	}
	if spec.Provider.Commands == nil {
		t.Fatal("a manager with a script hands the own node no command wrappers")
	}
	ctx := context.Background()
	own, _, err := controllernode.Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = own.Stop(ctx) })
	p := controllernode.Node(own).OTAProvider()
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(exchanges)
	}
	// A CASE peer on fabric 1.
	caseCtx := im.WithSubject(im.WithFabricFilter(ctx, true, 1), 0x1234, nil)
	query := provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8000, SoftwareVersion: 1,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
	}
	resp, err := p.MatterInvoke(caseCtx, provdef.CmdQueryImage, &query)
	if err != nil {
		t.Fatalf("scripted QueryImage: %v", err)
	}
	if r, ok := resp.(provdef.QueryImageResponse); !ok || r.Status != provdef.StatusNotAvailable {
		t.Errorf("scripted QueryImage answered %#v, want the script's NotAvailable", resp)
	}
	if n := count(); n != 1 || exchanges[0].QueryImage == nil || exchanges[0].Peer.FabricIndex != 1 || exchanges[0].Peer.NodeID != 0x1234 {
		t.Errorf("exchanges after the query: %d %+v", n, exchanges)
	}
	apply := provdef.ApplyUpdateRequestRequest{UpdateToken: make([]byte, 16), NewVersion: 2}
	resp, err = p.MatterInvoke(caseCtx, provdef.CmdApplyUpdateRequest, apply)
	if err != nil {
		t.Fatalf("scripted ApplyUpdateRequest: %v", err)
	}
	if r, ok := resp.(provdef.ApplyUpdateResponse); !ok || r.Action != provdef.ApplyUpdateActionAwaitNextAction || r.DelayedActionTime != delay {
		t.Errorf("scripted ApplyUpdateRequest answered %#v, want AwaitNextAction after %d s", resp, delay)
	}
	if n := count(); n != 2 || exchanges[1].ApplyUpdate == nil {
		t.Errorf("exchanges after the apply: %d", n)
	}
	// NotifyUpdateApplied runs the provider's own handler (it takes the
	// notice of an update it did not stage, logging the mismatch) and is
	// reported once answered.
	_, err = p.MatterInvoke(caseCtx, provdef.CmdNotifyUpdateApplied, &provdef.NotifyUpdateAppliedRequest{UpdateToken: make([]byte, 16), SoftwareVersion: 2})
	if err != nil {
		t.Errorf("NotifyUpdateApplied: %v", err)
	}
	if n := count(); n != 3 || exchanges[2].NotifyUpdateApplied == nil {
		t.Errorf("exchanges after the notice: %d", n)
	}
	// Without a CASE peer the provider's own handler refuses the command.
	if _, err := p.MatterInvoke(ctx, provdef.CmdQueryImage, &query); err == nil {
		t.Error("QueryImage without a CASE session was answered")
	}
	// Fields of another type are INVALID_COMMAND.
	_, err = p.MatterInvoke(caseCtx, provdef.CmdQueryImage, "fields")
	var coded statusCoded
	if err == nil || !errors.As(err, &coded) || coded.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("QueryImage with fields of another type: %v, want INVALID_COMMAND", err)
	}
	if n := count(); n != 3 {
		t.Errorf("a refused QueryImage was recorded: %d exchanges", n)
	}
	// Observe again returns the function that restores the provider's own
	// handlers: afterwards a scripted answer is not asked for.
	restore := controllernode.Observe(controllernode.Node(own), spec)
	restore()
	if _, err := p.MatterInvoke(caseCtx, provdef.CmdQueryImage, &query); err != nil {
		t.Errorf("the provider's own QueryImage after restore: %v", err)
	}
	if n := count(); n != 3 {
		t.Errorf("the restored provider still reports exchanges: %d", n)
	}
	// A manager without a script hands over no wrappers; Observe installs
	// nothing and its restore is a no-op.
	plain, err := softwareupdate.New(softwareupdate.Config{Registry: &client.Registry{}, Images: softwareupdate.NewImages(softwareupdate.ImagesConfig{})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg := plain.ProviderConfig(); cfg.Commands != nil {
		t.Error("a manager without a script hands over command wrappers")
	} else {
		controllernode.Observe(controllernode.Node(own), controller.OwnNodeSpec{Provider: cfg})()
	}
}

// statusCoded is an error that names the IM status it is answered with.
type statusCoded interface{ MatterStatusCode() im.StatusCode }
