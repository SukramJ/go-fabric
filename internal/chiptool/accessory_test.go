// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// The CHIP YAML "accessory" protocol.
//
// The YAML runner's SystemCommands pseudo-cluster (Reboot, FactoryReset,
// Start, Stop) does not touch the device itself: its
// accessory_server_bridge.py calls an XML-RPC server the CHIP CI scripts run
// next to the sample app (scripts/tests/chiptest/accessories.py), at an
// address hard-coded to 10.10.10.5:9000 on Linux. These steps are gated on
// PICS_SDK_CI_ONLY, so a run that keeps that code at 1 has to answer them.
//
// Mirrors matter.js packages/testing/src/chip/accessory-server.ts
// (AccessoryServer) and state.ts:configureNetwork: a server on a loopback
// port of the host's choosing, and the bridge script in the container
// rewritten to it — the harness container shares the host network, so
// 127.0.0.1 reaches the server. Reboot and FactoryReset do what
// accessories.py does to a sample app (stop and start on the same storage;
// stop, delete the storage, start), here to the reference daemon of the case
// that is running.

// accessoryBridgeScript is the YAML runner's client of the protocol inside
// the harness image (matter.js packages/testing/src/chip/config.ts
// accessoryClient).
const accessoryBridgeScript = "/usr/local/lib/python3.12/dist-packages/matter/yamltests/pseudo_clusters/clusters/accessory_server_bridge.py"

// errAccessoryUnsupported answers a method the suite does not implement;
// the step that called it fails (HTTP 404), as in matter.js.
var errAccessoryUnsupported = errors.New("unsupported accessory method")

// accessoryServer is the harness-wide XML-RPC endpoint. The case that is
// running attaches the handler for its own daemon.
type accessoryServer struct {
	srv  *http.Server
	port int

	mu      sync.Mutex
	handler func(method string) error
}

// startAccessoryServer listens on a loopback port of the kernel's choosing.
func startAccessoryServer() (*accessoryServer, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	a := &accessoryServer{port: ln.Addr().(*net.TCPAddr).Port}
	a.srv = &http.Server{Handler: a, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = a.srv.Serve(ln) }()
	return a, nil
}

// attach routes the protocol's methods to handler until detach.
func (a *accessoryServer) attach(handler func(method string) error) {
	a.mu.Lock()
	a.handler = handler
	a.mu.Unlock()
}

func (a *accessoryServer) detach() { a.attach(nil) }

func (a *accessoryServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.srv.Shutdown(ctx)
}

// xmlrpcCall is the part of an XML-RPC methodCall the protocol needs; the
// parameters (the register key, start options) name the one accessory a
// case has and are not used.
type xmlrpcCall struct {
	MethodName string `xml:"methodName"`
}

const xmlrpcTrue = "<?xml version=\"1.0\"?>\n<methodResponse><params><param><value><boolean>1</boolean></value></param></params></methodResponse>"

// ServeHTTP answers one methodCall.
func (a *accessoryServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var call xmlrpcCall
	if err == nil {
		err = xml.Unmarshal(body, &call)
	}
	if err != nil || call.MethodName == "" {
		http.Error(w, "missing or invalid accessory method name", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	handler := a.handler
	a.mu.Unlock()
	if handler == nil {
		http.Error(w, fmt.Sprintf("accessory method %s called with no case running", call.MethodName), http.StatusServiceUnavailable)
		return
	}
	switch err := handler(call.MethodName); {
	case errors.Is(err, errAccessoryUnsupported):
		http.Error(w, err.Error()+" "+call.MethodName, http.StatusNotFound)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		_, _ = io.WriteString(w, xmlrpcTrue)
	}
}

// pointAccessoryBridge rewrites the container's bridge script to the
// server: the address CHIP hard-codes on Linux and the port (matter.js
// state.ts:configureNetwork, the same two sed expressions).
func (h *harness) pointAccessoryBridge(ctx context.Context, port int) error {
	out, err := h.exec(ctx, "sed", "-i",
		"-e", "s/10.10.10.5/127.0.0.1/g",
		"-e", fmt.Sprintf("s/_PORT = 9000/_PORT = %d/g", port),
		accessoryBridgeScript)
	if err != nil {
		return fmt.Errorf("rewriting %s: %w\n%s", accessoryBridgeScript, err, out)
	}
	return nil
}

// attachAccessory routes the accessory protocol to br for the case that is
// running, the way accessories.py drives a sample app: Reboot stops and
// starts the daemon on its database, FactoryReset starts it on a fresh one,
// Stop and Start do one half each. The returned function is the daemon
// running now.
func attachAccessory(t *testing.T, h *harness, br *bridgeProcess) func() *bridgeProcess {
	t.Helper()
	var mu sync.Mutex
	cur := br
	h.accessory.attach(func(method string) error {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "reboot", "start":
			t.Logf("accessory %s: (re)starting the daemon on its database", method)
			cur = cur.restart(t)
		case "factoryReset":
			t.Logf("accessory factoryReset: restarting the daemon on a fresh database")
			cur = cur.factoryReset(t)
		case "stop":
			t.Logf("accessory stop: stopping the daemon")
			cur.stop()
		default:
			return errAccessoryUnsupported
		}
		return nil
	})
	return func() *bridgeProcess {
		mu.Lock()
		defer mu.Unlock()
		return cur
	}
}
