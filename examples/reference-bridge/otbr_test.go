// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// The --otbr flag: the URL's host and port, the default port, the
// refusals; the endpoint on a fake OTBR, its refresh.
func TestOTBRFlag(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"http://[fd00::1]:8081": "fd00::1:8081",
		"http://otbr.local":     "otbr.local:8081",
		"http://10.0.0.2:9000":  "10.0.0.2:9000",
	} {
		host, port, err := parseOTBRURL(raw)
		if err != nil || fmt.Sprintf("%s:%d", host, port) != want {
			t.Errorf("%s: %s:%d, %v", raw, host, port, err)
		}
	}
	for _, bad := range []string{"https://otbr", "otbr:8081", "http://otbr:x", "http://", "%zz"} {
		if _, _, err := parseOTBRURL(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	srv := otbrtest.New(otbrtest.Options{})
	defer srv.Close()
	b, err := newDemoBorderRouter(fmt.Sprintf("http://%s:%d", srv.Host(), srv.Port()), nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if b.device.ThreadBorderRouterManagement().Get().BorderRouterName != "go-fabric reference OTBR" {
		t.Error("the endpoint does not serve the border router")
	}
	b.refresh(context.Background())
	srv.SetDown(true)
	b.refresh(context.Background())
	if b.device.ThreadBorderRouterManagement().Get().InterfaceEnabled {
		t.Error("a silent border router's interface is enabled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.watch(ctx)
	if _, err := newDemoBorderRouter("http://127.0.0.1:1", nil, slog.Default()); err == nil {
		t.Error("an unreachable border router was accepted")
	}
	if _, err := newDemoBorderRouter("ftp://x", nil, slog.Default()); err == nil {
		t.Error("a non-http URL was accepted")
	}
}
