// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// The API connection's bounds, aioesphomeapi's (connection.py and
// client_base.py at 3b2a4a2d).
const (
	// tcpConnectTimeout bounds the TCP connect (connection.py:166).
	tcpConnectTimeout = 60 * time.Second
	// handshakeTimeout bounds the Noise handshake (connection.py:161).
	handshakeTimeout = 60 * time.Second
	// helloTimeout bounds HelloRequest and the device information
	// (CONNECT_REQUEST_TIMEOUT, connection.py:162).
	helloTimeout = 30 * time.Second
	// pingInterval is KEEP_ALIVE_FREQUENCY (client_base.py:71).
	pingInterval = 20 * time.Second
	// pongTimeout is 4.5 times the interval (connection.py:138-146).
	pongTimeout = pingInterval * 9 / 2
	// apiVersionMajor and apiVersionMinor are the version the client
	// declares (connection.py:259-267).
	apiVersionMajor = 1
	apiVersionMinor = 19
)

// errPongTimeout is a device that answered nothing for pongTimeout.
var errPongTimeout = errors.New("esphome: no answer to the keep-alive ping")

// errDeviceDisconnect is a device that asked to close the connection.
var errDeviceDisconnect = errors.New("esphome: the device closed the API connection")

// apiConn is one ESPHome API connection: framed, handshaken, the device's
// identity and Bluetooth proxy feature flags read.
type apiConn struct {
	nc    net.Conn
	f     wire.Framer
	clk   clock.Clock
	log   *slog.Logger
	name  string
	flags uint32
	// handle receives every message the connection does not answer
	// itself; set before run.
	handle func(typ uint16, payload []byte)
	// lastRx is the clock time of the last message, in nanoseconds.
	lastRx atomic.Int64

	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// dialAPI connects to the proxy at addr — TCP, the Noise handshake when psk
// is set, HelloRequest, DeviceInfoRequest and, from API 1.15,
// DeviceCapabilitiesRequest — after aioesphomeapi's connect
// (connection.py:660-686 _connect_hello_login, client.py:505-545). Password
// authentication is gone from ESPHome since 2026.1.0 (api.proto:105-106), so
// no AuthenticationRequest follows the hello.
func dialAPI(ctx context.Context, addr string, psk []byte, o *Options) (*apiConn, error) {
	dctx, cancel := clock.WithTimeout(ctx, o.Clock, tcpConnectTimeout)
	nc, err := o.Dial(dctx, "tcp", addr)
	cancel()
	if err != nil {
		return nil, err
	}
	a := &apiConn{nc: nc, clk: o.Clock, log: o.Logger, done: make(chan struct{})}
	// The handshake and the hello read synchronously; the connection is
	// closed when they overrun, or when ctx ends.
	expired := atomic.Bool{}
	t := o.Clock.AfterFunc(handshakeTimeout+helloTimeout, func() { expired.Store(true); _ = nc.Close() })
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	err = a.open(psk, o.ClientInfo)
	t.Stop()
	stop()
	if err != nil {
		_ = nc.Close()
		if expired.Load() {
			return nil, fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	a.lastRx.Store(o.Clock.Now().UnixNano())
	return a, nil
}

func (a *apiConn) open(psk []byte, clientInfo string) error {
	if psk != nil {
		n, hello, err := wire.ClientHandshake(a.nc, psk, nil)
		if err != nil {
			return err
		}
		a.f, a.name = n, hello.Name
	} else {
		a.f = wire.NewPlaintext(a.nc)
	}
	if err := a.f.WriteMessage(&wire.HelloRequest{ClientInfo: clientInfo, APIVersionMajor: apiVersionMajor, APIVersionMinor: apiVersionMinor}); err != nil {
		return err
	}
	var hello wire.HelloResponse
	if err := a.await(wire.TypeHelloResponse, hello.Unmarshal); err != nil {
		return err
	}
	if hello.APIVersionMajor > 2 {
		return fmt.Errorf("%w (%d.%d)", ErrIncompatibleAPI, hello.APIVersionMajor, hello.APIVersionMinor)
	}
	if hello.Name != "" {
		a.name = hello.Name
	}
	if err := a.f.WriteMessage(wire.Empty(wire.TypeDeviceInfoRequest)); err != nil {
		return err
	}
	var info wire.DeviceInfoResponse
	if err := a.await(wire.TypeDeviceInfoResponse, info.Unmarshal); err != nil {
		return err
	}
	if info.Name != "" {
		a.name = info.Name
	}
	a.flags = info.BluetoothProxyFeatureFlags
	// From API 1.15 the flags live in DeviceCapabilitiesResponse; the
	// device still fills the DeviceInfoResponse field for older clients
	// (api.proto:265-269, :311-312). device_capabilities_compat
	// (client.py:520-545) picks the source by the version.
	if hello.APIVersionMajor > 1 || hello.APIVersionMinor >= 15 {
		if err := a.f.WriteMessage(wire.Empty(wire.TypeDeviceCapabilitiesRequest)); err != nil {
			return err
		}
		var caps wire.DeviceCapabilitiesResponse
		if err := a.await(wire.TypeDeviceCapabilitiesResponse, caps.Unmarshal); err != nil {
			return err
		}
		// A sub-message at its defaults is not sent; test the flags
		// (api.proto:379-385).
		if caps.BluetoothProxyFeatureFlags != 0 {
			a.flags = caps.BluetoothProxyFeatureFlags
		}
	}
	return nil
}

// await reads until a message of type want, answering pings and dropping
// the rest.
func (a *apiConn) await(want uint16, decode func([]byte) error) error {
	for {
		typ, payload, err := a.f.ReadMessage()
		if err != nil {
			return err
		}
		switch typ {
		case want:
			return decode(payload)
		case wire.TypePingRequest:
			if err := a.f.WriteMessage(wire.Empty(wire.TypePingResponse)); err != nil {
				return err
			}
		case wire.TypeDisconnectRequest:
			_ = a.f.WriteMessage(wire.Empty(wire.TypeDisconnectResponse))
			return errDeviceDisconnect
		}
	}
}

// run reads the connection until it ends, answering pings and the device's
// disconnect itself, and keeps it alive with a ping every pingInterval.
func (a *apiConn) run() {
	go a.keepAlive()
	for {
		typ, payload, err := a.f.ReadMessage()
		if err != nil {
			a.close(err)
			return
		}
		a.lastRx.Store(a.clk.Now().UnixNano())
		switch typ {
		case wire.TypePingRequest:
			_ = a.send(wire.Empty(wire.TypePingResponse))
		case wire.TypePingResponse:
		case wire.TypeDisconnectRequest:
			// Either side may close; both close after the response
			// (api.proto:176-205).
			_ = a.send(wire.Empty(wire.TypeDisconnectResponse))
			a.close(errDeviceDisconnect)
			return
		default:
			a.handle(typ, payload)
		}
	}
}

// keepAlive pings every pingInterval and closes the connection when
// nothing arrived for pongTimeout (connection.py:138-146).
func (a *apiConn) keepAlive() {
	tk := a.clk.NewTicker(pingInterval)
	defer tk.Stop()
	for {
		select {
		case <-a.done:
			return
		case now := <-tk.C():
			if now.Sub(time.Unix(0, a.lastRx.Load())) > pongTimeout {
				a.close(errPongTimeout)
				return
			}
			_ = a.send(wire.Empty(wire.TypePingRequest))
		}
	}
}

// send writes m; a write that fails closes the connection.
func (a *apiConn) send(m wire.Message) error {
	select {
	case <-a.done:
		return ErrProxyUnavailable
	default:
	}
	if err := a.f.WriteMessage(m); err != nil {
		a.close(err)
		return fmt.Errorf("%w: %w", ErrProxyUnavailable, err)
	}
	return nil
}

// close ends the connection with err as its cause.
func (a *apiConn) close(err error) {
	a.closeOnce.Do(func() {
		a.errMu.Lock()
		a.err = err
		a.errMu.Unlock()
		_ = a.nc.Close()
		close(a.done)
	})
}

// cause is the error the connection ended with.
func (a *apiConn) cause() error {
	a.errMu.Lock()
	defer a.errMu.Unlock()
	return a.err
}
