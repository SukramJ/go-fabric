// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package esphome is a platform of the commissioner's BLE seam that needs no
// Bluetooth adapter: a [ble.Central] whose radios are ESPHome Bluetooth
// proxies, reached over the ESPHome native API on TCP. A host hands the
// central to controller.PairOptions.BLE as it would hand the BlueZ one
// (ADR 0024 decision 3: a platform is a module of its own; this one links
// one Noise library and no D-Bus).
//
// Neither matter.js nor connectedhomeip commissions through a proxy; the
// sequence the central runs is the one the BlueZ platform runs (scan,
// connect, find the Matter service and C1, C2 and C3, take the ATT_MTU,
// write C1, subscribe C2), spoken in the ESPHome API's messages. The wire —
// the plaintext and Noise framing, the message ids and fields, the request
// and reply pairs — follows aioesphomeapi (https://github.com/esphome/aioesphomeapi
// at 3b2a4a2d, MIT), the client Home Assistant runs; what the firmware does
// with each message follows ESPHome's bluetooth_proxy and
// bluetooth_connection components (https://github.com/esphome/esphome at
// 1cc6c9d1), cited by file and line where the behaviour matters. No source of
// either is copied: the messages are encoded by hand, as this module encodes
// TLV.
//
// # What a proxy needs
//
// A proxy is an ESP32 with `bluetooth_proxy: active: true` (connections need
// it; the feature flag ACTIVE_CONNECTIONS, without which [NewCentral] refuses
// the proxy) and, normally, an API key (`api: encryption: key:`), which
// [Proxy.Key] carries. Without a key the API is plaintext.
//
// # One subscriber per proxy
//
// A proxy forwards advertisements, and answers GATT requests, to one API
// client: the last one that subscribed to its advertisements
// (bluetooth_proxy.cpp:740-765, "the newest subscriber wins"). Home
// Assistant's Bluetooth integration is usually that client. Subscribing
// therefore takes the proxy away from Home Assistant for as long as this
// central scans or holds a connection, and Home Assistant takes it back when
// it resubscribes. The central logs ble.esphome.proxy_taken when it
// subscribes. It does not fight: a proxy that falls silent while subscribed
// is presumed taken; the central reconnects once after the backoff, and when
// the proxy is taken again it marks it busy — [ErrProxyBusy] — until the next
// scan starts. A proxy dedicated to commissioning, or a Home Assistant whose
// use of it may flap during a pairing, is the deployment this assumes. The
// silence watch counts advertisements of any kind, so in a BLE-quiet place
// (a shielded lab, a remote site) a proxy that nobody else uses is presumed
// taken as well: a host that sees ble.esphome.proxy_busy with no other
// client on the proxy raises Options.SilenceTimeout.
//
// # Discovery
//
// [Discover] finds the proxies over DNS-SD ([ServiceType], browsed with
// client/discovery's cache), connects to each device once and keeps those
// with ACTIVE_CONNECTIONS and REMOTE_CACHING; [Keys] gives each device's key
// by name or MAC. [Options.Discover] keeps a central's discovered set
// current ([DiscoverOptions]). Whether a device is a proxy is not in its TXT
// record (esphome/components/mdns/mdns_component.cpp:133-242): it is in the
// feature flags the device reports once connected.
package esphome

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// The errors of the central.
var (
	// ErrInvalidKey is a proxy that refused the API key.
	ErrInvalidKey = wire.ErrInvalidKey
	// ErrMalformedKey is a key that is not base64 of 32 bytes.
	ErrMalformedKey = wire.ErrMalformedKey
	// ErrRequiresEncryption is a proxy with an API key, reached without one.
	ErrRequiresEncryption = wire.ErrRequiresEncryption
	// ErrPlaintextDevice is a proxy without an API key, reached with one.
	ErrPlaintextDevice = wire.ErrPlaintextDevice
	// ErrNoProxies is NewCentral without a proxy.
	ErrNoProxies = errors.New("esphome: no Bluetooth proxy configured")
	// ErrNoActiveConnections is a proxy that forwards advertisements only:
	// its feature flags lack ACTIVE_CONNECTIONS (aioesphomeapi/model.py:140),
	// which `bluetooth_proxy: active: true` sets.
	ErrNoActiveConnections = errors.New("esphome: the proxy has no active connections (set bluetooth_proxy: active: true)")
	// ErrNoRemoteCaching is a proxy without REMOTE_CACHING, which selects
	// the V3 connect request; aioesphomeapi refuses such a proxy
	// (client.py:1330-1343).
	ErrNoRemoteCaching = errors.New("esphome: the proxy lacks REMOTE_CACHING (ESPHome before 2022.12)")
	// ErrIncompatibleAPI is a device whose API major version is above 2
	// (aioesphomeapi/connection.py:703-711).
	ErrIncompatibleAPI = errors.New("esphome: incompatible API version")
	// ErrProxyBusy is a proxy another API client — usually Home
	// Assistant — took over twice while this central used it.
	ErrProxyBusy = errors.New("esphome: the Bluetooth proxy is in use by another API client")
	// ErrProxyUnavailable is a proxy whose API connection is down.
	ErrProxyUnavailable = errors.New("esphome: the Bluetooth proxy is not connected")
	// ErrUnknownPeripheral is a Connect to an address no proxy reported.
	ErrUnknownPeripheral = errors.New("esphome: no proxy has seen the peripheral")
	// ErrNoMatterService is a peripheral whose GATT database has no Matter
	// service with C1, C2 and C2's CCCD — matter.js's "Missing required
	// Matter characteristics" (packages/nodejs-ble/src/NobleBleChannel.ts:378-384).
	ErrNoMatterService = errors.New("esphome: no Matter service with C1 and C2")
	// ErrNoC3 is ReadC3 on a peripheral without C3.
	ErrNoC3 = errors.New("esphome: the peripheral has no C3")
	// ErrTimeout is a proxy that did not answer a request in time.
	ErrTimeout = errors.New("esphome: the proxy did not answer in time")
)

// GATTError is an operation the proxy answered with
// BluetoothGATTErrorResponse (api.proto:2102-2110), or a connect it answered
// with connected=false and an error (BluetoothDeviceConnectionResponse,
// api.proto:1937-1946). Code is the stack's status: an ESP-IDF GATT status,
// or for a connect a BLEConnectionError (aioesphomeapi/ble_defs.py:6-17).
type GATTError struct {
	// Op names the operation: "connect", "services", "write", "read",
	// "notify", "cccd".
	Op string
	// Handle is the attribute handle; 0 for a connect.
	Handle uint32
	// Code is the error code.
	Code int32
}

func (e *GATTError) Error() string {
	if e.Op == "connect" {
		return fmt.Sprintf("esphome: connect failed (error 0x%x)", e.Code)
	}
	return fmt.Sprintf("esphome: GATT %s on handle 0x%04x failed (error 0x%x)", e.Op, e.Handle, e.Code)
}
