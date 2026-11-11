// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// The D-Bus names this package speaks: wpa_supplicant's service at its
// root object (connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:63-64
// at 6170af84) and the interfaces of src/platform/Linux/dbus/wpa/DBusWpa.xml,
// DBusWpaInterface.xml and DBusWpaBss.xml there.
const (
	service         = "fi.w1.wpa_supplicant1"
	rootPath        = dbus.ObjectPath("/fi/w1/wpa_supplicant1")
	ifaceRoot       = "fi.w1.wpa_supplicant1"
	ifaceInterface  = "fi.w1.wpa_supplicant1.Interface"
	ifaceBSS        = "fi.w1.wpa_supplicant1.BSS"
	ifaceProperties = "org.freedesktop.DBus.Properties"
	methodGet       = ifaceProperties + ".Get"
	methodGetAll    = ifaceProperties + ".GetAll"
	sigPropsChanged = ifaceProperties + ".PropertiesChanged"
	sigScanDone     = ifaceInterface + ".ScanDone"
)

// The interface states the connect follows (DBusWpaInterface.xml's State;
// wpa_supplicant's doc/dbus.doxygen lists them).
const (
	stateAssociating       = "associating"
	stateCompleted         = "completed"
	stateDisconnected      = "disconnected"
	stateInterfaceDisabled = "interface_disabled"
)

// Defaults of [Options]: connectedhomeip's Linux driver's — the interface
// CHIP_DEVICE_CONFIG_WIFI_STATION_IF_NAME and the maxima the cluster
// advertises (src/platform/Linux/NetworkCommissioningDriver.h:85-87 at
// 6170af84).
const (
	DefaultInterface      = "wlan0"
	DefaultScanTimeout    = 10 * time.Second
	DefaultConnectTimeout = 20 * time.Second
)

// assocMaxRetries and wlanStatusAuthTimeout: an association that ends in
// the IEEE 802.11 status AUTH_TIMEOUT is retried up to eight times
// (connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:56-61,
// src/platform/Linux/WirelessDefs.h:39 at 6170af84).
const (
	assocMaxRetries       = 8
	wlanStatusAuthTimeout = 16
)

const (
	statusSuccess = uint8(netcommdef.NetworkCommissioningStatusSuccess)
	statusUnknown = uint8(netcommdef.NetworkCommissioningStatusUnknownError)
)

// Options configures [New]. The zero value is wlan0 with connectedhomeip's
// maxima.
type Options struct {
	// Interface is the Wi-Fi interface wpa_supplicant manages ("" is
	// wlan0).
	Interface string
	// ScanTimeout bounds a scan (0 is 10 s); ConnectTimeout bounds a join
	// (0 is 20 s). The cluster's own bounds apply as well, through the
	// call's context.
	ScanTimeout, ConnectTimeout time.Duration
	// Logger receives the driver's diagnostics; nil is slog.Default(). The
	// Wi-Fi credential never reaches it.
	Logger *slog.Logger
}

// Driver is a contract.WiFiDriver over wpa_supplicant's D-Bus interface:
// node.Network.WiFi's Driver on a Linux host. Build it with [New]; it
// holds one system-bus connection until [Driver.Close]. A scan and a join
// run one at a time each; a second one while one runs is UnknownError, as
// in connectedhomeip.
type Driver struct {
	bus            bus
	iface          dbus.ObjectPath
	scanTimeout    time.Duration
	connectTimeout time.Duration
	log            *slog.Logger

	mu      sync.Mutex
	scan    chan struct{} // non-nil while a scan waits for ScanDone
	connect *attempt      // non-nil while a join follows State

	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
	loopDone  chan struct{}
}

var _ contract.WiFiDriver = (*Driver)(nil)

// attempt is one join's view of the interface's State changes.
type attempt struct {
	states chan string
	done   chan struct{}
}

// newDriver brings the interface up over b and starts following its
// signals. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:464-520
// at 6170af84 (_OnWpaInterfaceReady): GetInterface(ifname), on failure
// CreateInterface({"Ifname": ifname}); then auto scan is switched off
// (:453-461, StopAutoScan :1232-1246, "or it will block our network
// scan"), a failure of that logged, not fatal.
func newDriver(ctx context.Context, b bus, opts Options) (*Driver, error) {
	d := &Driver{
		bus: b, scanTimeout: opts.ScanTimeout, connectTimeout: opts.ConnectTimeout, log: opts.Logger,
		closed: make(chan struct{}), loopDone: make(chan struct{}),
	}
	ifname := opts.Interface
	if ifname == "" {
		ifname = DefaultInterface
	}
	if d.scanTimeout <= 0 {
		d.scanTimeout = DefaultScanTimeout
	}
	if d.connectTimeout <= 0 {
		d.connectTimeout = DefaultConnectTimeout
	}
	if d.log == nil {
		d.log = slog.Default()
	}
	var path dbus.ObjectPath
	body, err := b.Call(ctx, rootPath, ifaceRoot+".GetInterface", ifname)
	if err == nil {
		err = dbus.Store(body, &path)
	}
	if err != nil {
		d.log.Info("wpasupplicant: interface not found, creating it", slog.String("interface", ifname), slog.String("error", errText(err)))
		body, err = b.Call(ctx, rootPath, ifaceRoot+".CreateInterface", map[string]dbus.Variant{"Ifname": dbus.MakeVariant(ifname)})
		if err == nil {
			err = dbus.Store(body, &path)
		}
		if err != nil {
			return nil, fmt.Errorf("wpasupplicant: create interface %s: %w", ifname, err)
		}
	}
	d.iface = path
	if _, err := b.Call(ctx, path, ifaceInterface+".AutoScan", ""); err != nil {
		d.log.Warn("wpasupplicant: failed to stop auto scan", slog.String("error", errText(err)))
	}
	d.log.Info("wpasupplicant: interface ready", slog.String("interface", ifname), slog.String("path", string(path)))
	go d.run()
	return d, nil
}

// run follows the interface's signals until Close: the scan's end is the
// interface's ScanDone, the State changes come from
// org.freedesktop.DBus.Properties.PropertiesChanged for
// fi.w1.wpa_supplicant1.Interface — connectedhomeip connects "scan-done"
// and GDBus's "g-properties-changed"
// (src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:434-444
// at 6170af84), not wpa_supplicant's own PropertiesChanged signal.
func (d *Driver) run() {
	defer close(d.loopDone)
	sigs := d.bus.Signals()
	for {
		select {
		case <-d.closed:
			return
		case s, ok := <-sigs:
			if !ok {
				return
			}
			d.handle(s)
		}
	}
}

func (d *Driver) handle(s *dbus.Signal) {
	if s == nil || s.Path != d.iface {
		return
	}
	switch s.Name {
	case sigScanDone:
		d.mu.Lock()
		ch := d.scan
		d.mu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	case sigPropsChanged:
		if len(s.Body) < 2 {
			return
		}
		if name, _ := s.Body[0].(string); name != ifaceInterface {
			return
		}
		changed, _ := s.Body[1].(map[string]dbus.Variant)
		v, ok := changed["State"]
		if !ok {
			return
		}
		state, ok := v.Value().(string)
		if !ok {
			return
		}
		d.log.Debug("wpasupplicant: interface state", slog.String("state", state))
		d.mu.Lock()
		a := d.connect
		d.mu.Unlock()
		if a == nil {
			return
		}
		select {
		case a.states <- state:
		case <-a.done:
		case <-d.closed:
		}
	}
}

// ScanNetworks implements contract.WiFiDriver. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1251-1280
// (StartWiFiScan: one scan at a time, Scan({"Type": "active"})) and
// :1545-1595 (_OnWpaInterfaceScanDone: the BSSs read once the scan is
// done, each one's properties, a directed scan filtered by SSID at
// :1568-1573) at 6170af84. A scan that does not end within the timeout or
// the call's context is UnknownError.
func (d *Driver) ScanNetworks(ctx context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	done := make(chan struct{}, 1)
	d.mu.Lock()
	switch {
	case d.isClosed():
		d.mu.Unlock()
		return statusUnknown, "the driver is closed", nil
	case d.scan != nil:
		d.mu.Unlock()
		return statusUnknown, "a scan is in progress", nil
	}
	d.scan = done
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.scan = nil
		d.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, d.scanTimeout)
	defer cancel()

	if _, err := d.bus.Call(ctx, d.iface, ifaceInterface+".Scan", map[string]dbus.Variant{"Type": dbus.MakeVariant("active")}); err != nil {
		d.log.Info("wpasupplicant: failed to start network scan", slog.String("error", errText(err)))
		return statusUnknown, "scan: " + errName(err), nil
	}
	select {
	case <-done:
	case <-ctx.Done():
		return statusUnknown, "the scan did not end in time", nil
	case <-d.closed:
		return statusUnknown, "the driver is closed", nil
	}

	var paths []dbus.ObjectPath
	if v, err := d.property(ctx, "BSSs"); err == nil {
		paths, _ = v.Value().([]dbus.ObjectPath)
	} else {
		d.log.Debug("wpasupplicant: no network found", slog.String("error", errText(err)))
	}
	for _, p := range paths {
		props, err := d.getAll(ctx, p, ifaceBSS)
		if err != nil {
			d.log.Debug("wpasupplicant: BSS not found", slog.String("path", string(p)))
			continue
		}
		r, ok := scanResult(props)
		if !ok {
			d.log.Debug("wpasupplicant: BSS dropped", slog.String("path", string(p)))
			continue
		}
		if len(ssid) == 0 || bytes.Equal(r.SSID, ssid) {
			results = append(results, r)
		}
	}
	return statusSuccess, "", results
}

// ConnectNetwork implements contract.WiFiDriver. Mirrors connectedhomeip
// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp
// at 6170af84: ConnectWiFiNetworkAsync (:914-943; one join at a time,
// :932; the network {ssid, psk, key_mgmt "SAE WPA-PSK"}, :936-940) and
// _ConnectWiFiNetworkAsync (:852-911): a disabled interface fails
// (WpaSupplicantClient.cpp:209-214), a CurrentNetwork other than "/" is
// removed first (:861-877), then AddNetwork, Disconnect with its errors
// ignored (:892-895) and SelectNetwork (:897-904). The verdict follows the
// interface's State (_OnWpaPropertiesChanged, :287-418): "associating"
// starts the attempt (:297-300); "completed" after it is Success
// (:402-416); "disconnected" after it fails with UnknownError and the
// interface's DisconnectReason as errorValue (:301-303, :378-379) — unless
// AssocStatusCode is AUTH_TIMEOUT with retries left, which selects the
// network again (:324-348). The timeout, the call's context and Close end
// it with UnknownError and errorValue 0.
//
// Empty credentials join an open network: key_mgmt "NONE" without a psk
// (connectedhomeip sends psk "" with "SAE WPA-PSK"). An SSID that is not
// valid UTF-8 or holds a NUL is refused: chip hands the SSID as a D-Bus
// string, and wpa_supplicant's D-Bus documentation (doc/dbus.doxygen,
// fi.w1.wpa_supplicant1.Interface AddNetwork) names no other form for a
// network-block entry than its "appropriate type". The committed network
// is not saved into wpa_supplicant's configuration (no SaveConfig): the
// node's store keeps it and the cluster rejoins it at start.
func (d *Driver) ConnectNetwork(ctx context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	a := &attempt{states: make(chan string, 16), done: make(chan struct{})}
	d.mu.Lock()
	switch {
	case d.isClosed():
		d.mu.Unlock()
		return statusUnknown, "the driver is closed", 0
	case d.connect != nil:
		d.mu.Unlock()
		return statusUnknown, "a connect is in progress", 0
	}
	d.connect = a
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.connect = nil
		d.mu.Unlock()
		close(a.done)
	}()
	ctx, cancel := context.WithTimeout(ctx, d.connectTimeout)
	defer cancel()

	ssidAttr := slog.String("ssid", fmt.Sprintf("%q", ssid))
	network, debugText := d.addAndSelect(ctx, ssid, credentials, ssidAttr)
	if debugText != "" {
		return statusUnknown, debugText, 0
	}
	return d.await(ctx, a, network, ssidAttr)
}

// addAndSelect hands the network to wpa_supplicant and selects it: the
// part of ConnectNetwork before the State walk. A non-empty debugText is
// the failure.
func (d *Driver) addAndSelect(ctx context.Context, ssid, credentials []byte, ssidAttr slog.Attr) (network dbus.ObjectPath, debugText string) {
	args, refusal := networkArgs(ssid, credentials)
	if refusal != "" {
		d.log.Warn("wpasupplicant: network refused", slog.String("reason", refusal))
		return "", refusal
	}
	if v, err := d.property(ctx, "State"); err == nil {
		if s, _ := v.Value().(string); s == stateInterfaceDisabled {
			d.log.Warn("wpasupplicant: the Wi-Fi interface is disabled (blocked)")
			return "", "the Wi-Fi interface is disabled"
		}
	}
	if v, err := d.property(ctx, "CurrentNetwork"); err == nil {
		if cur, _ := v.Value().(dbus.ObjectPath); cur != "" && cur != "/" {
			if _, err := d.bus.Call(ctx, d.iface, ifaceInterface+".RemoveNetwork", cur); err != nil {
				d.log.Warn("wpasupplicant: failed to remove the current network", slog.String("error", errText(err)))
				return "", "remove network: " + errName(err)
			}
			d.log.Debug("wpasupplicant: removed network", slog.String("path", string(cur)))
		}
	}
	// The one call that carries the credential: neither its arguments nor
	// its error's text (which a service may build from them) is logged or
	// returned — only the D-Bus error name.
	body, err := d.bus.Call(ctx, d.iface, ifaceInterface+".AddNetwork", args)
	if err == nil {
		err = dbus.Store(body, &network)
	}
	if err != nil {
		d.log.Warn("wpasupplicant: failed to add network", ssidAttr, slog.String("error", errName(err)))
		return "", "add network: " + errName(err)
	}
	d.log.Info("wpasupplicant: added network", ssidAttr, slog.String("path", string(network)))
	_, _ = d.bus.Call(ctx, d.iface, ifaceInterface+".Disconnect")
	if err := d.selectNetwork(ctx, network); err != nil {
		return "", "select network: " + errName(err)
	}
	return network, ""
}

// await follows the interface's State to the join's verdict.
func (d *Driver) await(ctx context.Context, a *attempt, network dbus.ObjectPath, ssidAttr slog.Attr) (status uint8, debugText string, errorValue int32) {
	retries := assocMaxRetries
	started := false
	for {
		var state string
		select {
		case <-ctx.Done():
			d.log.Info("wpasupplicant: connect did not complete in time", ssidAttr)
			return statusUnknown, "the connect did not complete in time", 0
		case <-d.closed:
			return statusUnknown, "the driver is closed", 0
		case state = <-a.states:
		}
		switch state {
		case stateAssociating:
			started = true
		case stateCompleted:
			if started {
				d.log.Info("wpasupplicant: connected", ssidAttr)
				return statusSuccess, "", 0
			}
		case stateDisconnected:
			if !started {
				continue
			}
			started = false
			reason := d.int32Property(ctx, "DisconnectReason")
			assoc := d.int32Property(ctx, "AssocStatusCode")
			d.log.Debug("wpasupplicant: disconnected", slog.Int("reason", int(reason)), slog.Int("assocStatus", int(assoc)))
			if assoc == wlanStatusAuthTimeout && retries > 0 {
				retries--
				d.log.Debug("wpasupplicant: association timeout, retrying", slog.Int("retriesLeft", retries))
				if err := d.selectNetwork(ctx, network); err == nil {
					continue
				}
			}
			d.log.Info("wpasupplicant: connect failed", ssidAttr, slog.Int("reason", int(reason)))
			return statusUnknown, fmt.Sprintf("disconnected, reason %d", reason), reason
		}
	}
}

// selectNetwork selects network, logging a failure.
func (d *Driver) selectNetwork(ctx context.Context, network dbus.ObjectPath) error {
	_, err := d.bus.Call(ctx, d.iface, ifaceInterface+".SelectNetwork", network)
	if err != nil {
		d.log.Warn("wpasupplicant: failed to select network", slog.String("error", errText(err)))
	}
	return err
}

// networkArgs is AddNetwork's argument for the network, or why it is
// refused. The refusal names no byte of the credential.
func networkArgs(ssid, credentials []byte) (args map[string]dbus.Variant, refusal string) {
	if !utf8.Valid(ssid) || bytes.IndexByte(ssid, 0) >= 0 {
		return nil, "the SSID is not valid UTF-8 or holds a NUL; wpa_supplicant's D-Bus AddNetwork takes it as a string"
	}
	args = map[string]dbus.Variant{"ssid": dbus.MakeVariant(string(ssid))}
	if len(credentials) == 0 {
		args["key_mgmt"] = dbus.MakeVariant("NONE")
		return args, ""
	}
	if !utf8.Valid(credentials) || bytes.IndexByte(credentials, 0) >= 0 {
		return nil, "the credential is not valid UTF-8 or holds a NUL; wpa_supplicant's D-Bus AddNetwork takes it as a string"
	}
	args["psk"] = dbus.MakeVariant(string(credentials))
	args["key_mgmt"] = dbus.MakeVariant("SAE WPA-PSK")
	return args, ""
}

// int32Property is one of the interface's int properties, 0 when the
// service does not serve it (connectedhomeip's test mock serves neither
// DisconnectReason nor AssocStatusCode).
func (d *Driver) int32Property(ctx context.Context, name string) int32 {
	v, err := d.property(ctx, name)
	if err != nil {
		return 0
	}
	n, _ := v.Value().(int32)
	return n
}

// property reads one of the interface's properties
// (org.freedesktop.DBus.Properties.Get).
func (d *Driver) property(ctx context.Context, name string) (dbus.Variant, error) {
	body, err := d.bus.Call(ctx, d.iface, methodGet, ifaceInterface, name)
	if err != nil {
		return dbus.Variant{}, err
	}
	var v dbus.Variant
	if err := dbus.Store(body, &v); err != nil {
		return dbus.Variant{}, fmt.Errorf("%s.%s: %w", ifaceInterface, name, err)
	}
	return v, nil
}

// getAll reads an object's properties (org.freedesktop.DBus.Properties.GetAll),
// as a GDBus proxy loads them.
func (d *Driver) getAll(ctx context.Context, path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error) {
	body, err := d.bus.Call(ctx, path, methodGetAll, iface)
	if err != nil {
		return nil, err
	}
	var props map[string]dbus.Variant
	if err := dbus.Store(body, &props); err != nil {
		return nil, fmt.Errorf("%s: %w", iface, err)
	}
	return props, nil
}

// isClosed reports whether Close ran.
func (d *Driver) isClosed() bool {
	select {
	case <-d.closed:
		return true
	default:
		return false
	}
}

// Close ends a waiting scan or join (UnknownError) and releases the bus.
// It is idempotent.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		close(d.closed)
		d.closeErr = d.bus.Close()
		<-d.loopDone
	})
	return d.closeErr
}

// errName is err's D-Bus error name, "failed" when it has none: the text
// a debug text carries.
func errName(err error) string {
	var de dbus.Error
	var dep *dbus.Error
	switch {
	case errors.As(err, &de):
		return de.Name
	case errors.As(err, &dep):
		return dep.Name
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err.Error()
	}
	return "failed"
}

// errText is err's text for a log line of a call that carries no
// credential.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
