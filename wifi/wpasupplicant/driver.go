// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"bytes"
	"context"
	"encoding/hex"
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

// Defaults of [Options]. DefaultInterface is connectedhomeip's
// CHIP_DEVICE_CONFIG_WIFI_STATION_IF_NAME, DefaultConnectTimeout the
// connect maximum its Linux driver advertises
// (src/platform/Linux/NetworkCommissioningDriver.h:85-87 at 6170af84).
//
// DefaultScanTimeout is a safety net, not the advertised value: the
// cluster keeps advertising ScanMaxTimeSeconds 10 (chip's
// GetScanNetworkTimeoutSeconds, :86), but chip's driver sets no timer of
// its own — it answers at wpa_supplicant's ScanDone — and a scan while the
// station is associated took 10.008 s and 9.990 s on the ble-virtual job's
// mac80211_hwsim radio (run 43137917486; 3.72 s unassociated). 30 s ends
// only a scan that never reports.
const (
	DefaultInterface      = "wlan0"
	DefaultScanTimeout    = 30 * time.Second
	DefaultConnectTimeout = 20 * time.Second
)

// scanReadTimeout bounds reading a finished scan's BSSs: their own
// context derived from the caller's, so a ScanDone close to the scan's
// bound still yields every result.
const scanReadTimeout = 5 * time.Second

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
// maxima. The interface's D-Bus path is resolved once, at New: when
// wpa_supplicant restarts, the Driver's calls fail and the host builds a
// new Driver (its process supervision).
type Options struct {
	// Interface is the Wi-Fi interface wpa_supplicant manages ("" is
	// wlan0).
	Interface string
	// ScanTimeout bounds the wait for a scan's end (0 is 30 s, see
	// DefaultScanTimeout); ConnectTimeout bounds a join
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

	mu         sync.Mutex
	scanning   bool          // a scan runs
	scan       chan struct{} // non-nil from the running scan's arming, before its Scan call: it waits for ScanDone
	connecting bool          // a join runs
	connect    *attempt      // non-nil while a join follows State

	// register hands the run loop a join's attempt once the join has
	// selected nothing yet (listen).
	register chan *attempt
	// armScan hands the run loop a scan's waiter before its Scan call
	// (ScanNetworks).
	armScan chan *scanWaiter
	// calls counts the scans and joins in flight: Close lets them finish
	// (a failed join removes its network) before it releases the bus.
	calls sync.WaitGroup

	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
	loopDone  chan struct{}
}

var _ contract.WiFiDriver = (*Driver)(nil)

// scanWaiter is one scan's wait for ScanDone; ready closes once the run
// loop installed it.
type scanWaiter struct {
	done  chan struct{}
	ready chan struct{}
}

// attempt is one join's view of the interface's State changes.
type attempt struct {
	states chan string
	// ready is closed once the run loop installed the attempt.
	ready chan struct{}
	done  chan struct{}
}

// removeTimeout bounds the RemoveNetwork of a failed join's network, which
// runs after the join's own context may have ended.
const removeTimeout = 2 * time.Second

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
		register: make(chan *attempt), armScan: make(chan *scanWaiter),
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
		d.log.Info("wpasupplicant: interface not found, creating it", slog.String("iface", ifname), slog.Any("err", err))
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
		d.log.Warn("wpasupplicant: failed to stop auto scan", slog.Any("err", err))
	}
	d.log.Info("wpasupplicant: interface ready", slog.String("iface", ifname), slog.String("path", string(path)))
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
		case w := <-d.armScan:
			// A scan arms before it calls Scan, as connectedhomeip sets its
			// one-shot scan callback before wpa_supplicant_1_interface_call_scan_sync
			// (src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1269-1270
			// at 6170af84). The signals queued now were sent before that
			// call: a ScanDone among them ended an earlier scan and is
			// dropped. Every ScanDone after the arming ends the wait — also
			// one sent right behind the Scan reply, which a wait armed only
			// after the call returned could find queued already and drop.
			// The accepted consequence, as in connectedhomeip: a ScanDone
			// of a scan another client started that arrives after the
			// arming ends this wait early, and the BSS read returns that
			// scan's results.
			for n := len(sigs); n > 0; n-- {
				s, ok := <-sigs
				if !ok {
					return
				}
				d.handle(s)
			}
			d.mu.Lock()
			d.scan = w.done
			d.mu.Unlock()
			close(w.ready)
		case a := <-d.register:
			// The signals already queued were sent before the reply of the
			// join's last call before SelectNetwork (godbus delivers a
			// signal into the channel before it reads the next message):
			// they belong to the network that was current, to its removal
			// and to the driver's own Disconnect, not to this attempt.
			// They are handled without it — a scan still sees its
			// ScanDone — and the attempt follows only what comes after.
			for n := len(sigs); n > 0; n-- {
				s, ok := <-sigs
				if !ok {
					return
				}
				d.handle(s)
			}
			d.mu.Lock()
			d.connect = a
			d.mu.Unlock()
			close(a.ready)
		}
	}
}

func (d *Driver) handle(s *dbus.Signal) {
	if s == nil || s.Path != d.iface {
		return
	}
	switch s.Name {
	case sigScanDone:
		// The signal's success argument is not read: connectedhomeip's
		// _OnWpaInterfaceScanDone reads the interface's BSSs whatever it
		// says (ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:1545-1595
		// at 6170af84; success is unused). A ScanDone of a scan another
		// client started ends a waiting scan as well, as there.
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
// :1568-1573) at 6170af84. The wait for ScanDone is armed before the Scan
// call, as connectedhomeip sets its scan callback first (:1269-1270): a
// ScanDone sent right behind the call's reply ends it ([Driver.run]); a
// Scan call that fails disarms it. A scan that does not end within the
// timeout or the call's context is UnknownError.
func (d *Driver) ScanNetworks(ctx context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	d.mu.Lock()
	switch {
	case d.isClosed():
		d.mu.Unlock()
		return statusUnknown, "the driver is closed", nil
	case d.scanning:
		d.mu.Unlock()
		return statusUnknown, "a scan is in progress", nil
	}
	d.scanning = true
	d.calls.Add(1)
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.scanning, d.scan = false, nil
		d.mu.Unlock()
		d.calls.Done()
	}()
	// The bound covers the Scan call and the wait for ScanDone only; the
	// reads after it get their own context (scanReadTimeout).
	waitCtx, cancel := context.WithTimeout(ctx, d.scanTimeout)
	defer cancel()

	// Armed before the call, so no ScanDone of this scan can precede it.
	w := &scanWaiter{done: make(chan struct{}, 1), ready: make(chan struct{})}
	select {
	case d.armScan <- w:
	case <-d.closed:
		return statusUnknown, "the driver is closed", nil
	}
	select {
	case <-w.ready:
	case <-d.closed:
		return statusUnknown, "the driver is closed", nil
	}
	if _, err := d.bus.Call(waitCtx, d.iface, ifaceInterface+".Scan", map[string]dbus.Variant{"Type": dbus.MakeVariant("active")}); err != nil {
		// Disarmed at once: no waiter of a scan that never started is
		// left for a ScanDone to fill.
		d.mu.Lock()
		d.scan = nil
		d.mu.Unlock()
		d.log.Info("wpasupplicant: failed to start network scan", slog.Any("err", err))
		return statusUnknown, "scan: " + errName(err), nil
	}
	select {
	case <-w.done:
	case <-waitCtx.Done():
		return statusUnknown, "the scan did not end in time", nil
	case <-d.closed:
		return statusUnknown, "the driver is closed", nil
	}
	ctx, cancelRead := context.WithTimeout(ctx, scanReadTimeout)
	defer cancelRead()

	var paths []dbus.ObjectPath
	if v, err := d.property(ctx, "BSSs"); err == nil {
		paths, _ = v.Value().([]dbus.ObjectPath)
	} else {
		d.log.Debug("wpasupplicant: no network found", slog.Any("err", err))
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
// The attempt follows the State changes from SelectNetwork on only
// ([Driver.listen]). A join that does not end in Success removes the
// network it added ([Driver.removeAdded]); connectedhomeip keeps it. The
// network's arguments are [networkArgs]'s. The committed network is not
// saved into wpa_supplicant's configuration (no SaveConfig): the node's
// store keeps it and the cluster rejoins it at start.
func (d *Driver) ConnectNetwork(ctx context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	d.mu.Lock()
	switch {
	case d.isClosed():
		d.mu.Unlock()
		return statusUnknown, "the driver is closed", 0
	case d.connecting:
		d.mu.Unlock()
		return statusUnknown, "a connect is in progress", 0
	}
	d.connecting = true
	d.calls.Add(1)
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.connecting = false
		d.connect = nil
		d.mu.Unlock()
		d.calls.Done()
	}()
	ctx, cancel := context.WithTimeout(ctx, d.connectTimeout)
	defer cancel()

	ssidAttr := slog.String("ssid", fmt.Sprintf("%q", ssid))
	network, debugText := d.addNetwork(ctx, ssid, credentials, ssidAttr)
	if debugText != "" {
		return statusUnknown, debugText, 0
	}
	_, _ = d.bus.Call(ctx, d.iface, ifaceInterface+".Disconnect")
	a, ok := d.listen()
	if !ok {
		d.removeAdded(ctx, network)
		return statusUnknown, "the driver is closed", 0
	}
	defer close(a.done)
	if err := d.selectNetwork(ctx, network); err != nil {
		d.removeAdded(ctx, network)
		return statusUnknown, "select network: " + errName(err), 0
	}
	status, debugText, errorValue = d.await(ctx, a, network, ssidAttr)
	if status != statusSuccess {
		d.removeAdded(ctx, network)
	}
	return status, debugText, errorValue
}

// listen starts the join's attempt: from here on the interface's State
// changes are its own. It runs after the driver's Disconnect has returned
// and right before SelectNetwork, so a change caused by the network that
// was current, its removal or the Disconnect does not decide the join
// (the run loop drains them first). False when the driver closed.
func (d *Driver) listen() (*attempt, bool) {
	a := &attempt{states: make(chan string, 16), ready: make(chan struct{}), done: make(chan struct{})}
	select {
	case d.register <- a:
	case <-d.closed:
		return nil, false
	}
	select {
	case <-a.ready:
		return a, true
	case <-d.closed:
		return nil, false
	}
}

// removeAdded removes the network a join added once the join did not end
// in Success — its select failed, the interface disconnected, the timeout,
// the call's context or Close ended it. The credential lives in the node's
// store only, and wpa_supplicant must not keep trying a network whose
// join the node already reported as failed. connectedhomeip keeps the
// network (_ConnectWiFiNetworkAsync, :852-911 at 6170af84, removes only
// the current network at the next join). The call runs on a context of
// its own, since the join's may have ended; a failure is logged by its
// D-Bus error name.
func (d *Driver) removeAdded(ctx context.Context, network dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	if _, err := d.bus.Call(ctx, d.iface, ifaceInterface+".RemoveNetwork", network); err != nil {
		d.log.Warn("wpasupplicant: failed to remove the network of a failed join", slog.String("path", string(network)), slog.String("reason", errName(err)))
		return
	}
	d.log.Debug("wpasupplicant: removed the network of a failed join", slog.String("path", string(network)))
}

// addNetwork hands the network to wpa_supplicant: the part of
// ConnectNetwork before the State walk. A non-empty debugText is the
// failure; then nothing was added.
func (d *Driver) addNetwork(ctx context.Context, ssid, credentials []byte, ssidAttr slog.Attr) (network dbus.ObjectPath, debugText string) {
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
				d.log.Warn("wpasupplicant: failed to remove the current network", slog.Any("err", err))
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
		d.log.Warn("wpasupplicant: failed to add network", ssidAttr, slog.String("reason", errName(err)))
		return "", "add network: " + errName(err)
	}
	d.log.Info("wpasupplicant: added network", ssidAttr, slog.String("path", string(network)))
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
			d.log.Debug("wpasupplicant: disconnected", slog.Int("code", int(reason)), slog.Int("status_code", int(assoc)))
			if assoc == wlanStatusAuthTimeout && retries > 0 {
				retries--
				d.log.Debug("wpasupplicant: association timeout, retrying", slog.Int("left", retries))
				if err := d.selectNetwork(ctx, network); err == nil {
					continue
				}
			}
			d.log.Info("wpasupplicant: connect failed", ssidAttr, slog.Int("code", int(reason)))
			return statusUnknown, fmt.Sprintf("disconnected, reason %d", reason), reason
		}
	}
}

// selectNetwork selects network, logging a failure.
func (d *Driver) selectNetwork(ctx context.Context, network dbus.ObjectPath) error {
	_, err := d.bus.Call(ctx, d.iface, ifaceInterface+".SelectNetwork", network)
	if err != nil {
		d.log.Warn("wpasupplicant: failed to select network", slog.Any("err", err))
	}
	return err
}

// rawPSKHexLength is the length of a raw WPA PSK written as hex digits:
// 32 bytes.
const rawPSKHexLength = 64

// networkArgs is AddNetwork's argument for the network, or why it is
// refused. The refusal names no byte of the credential.
//
// wpa_supplicant turns a D-Bus string entry into a quoted configuration
// value unless its key is in dont_quote ("ssid" and "psk" are not), and a
// byte array into an unquoted hex string (hostap
// wpa_supplicant/dbus/dbus_new_handlers.c:141-170, dont_quote and
// should_quote_opt, and :225-257 in set_network_properties, at 6d3d778).
// A quoted psk is a passphrase of 8 to 63 characters, an unquoted one
// 64 hex digits, the raw PSK (wpa_supplicant/config.c:620-695,
// wpa_config_parse_psk; wpa_supplicant/wpa_supplicant.conf:1171-1175); an
// ssid is a quoted string or a hex string of its octets
// (wpa_supplicant.conf:971-974). So:
//
//   - the SSID goes as a string when it is valid UTF-8 without NUL —
//     connectedhomeip's form (:936-940 at 6170af84) and what its test mock
//     sees — and as a byte array otherwise, which reaches wpa_supplicant as
//     the hex form of its octets;
//   - a credential of exactly 64 hex digits is the raw PSK and goes as a
//     byte array of its 32 bytes: as a string (connectedhomeip's form)
//     wpa_supplicant would quote it and refuse it as an over-long
//     passphrase. Every other credential goes as a string; one that is not
//     valid UTF-8 or holds a NUL cannot be a D-Bus string and is refused.
//
// Empty credentials join an open network: key_mgmt "NONE" without a psk
// (connectedhomeip sends psk "" with "SAE WPA-PSK").
func networkArgs(ssid, credentials []byte) (args map[string]dbus.Variant, refusal string) {
	args = map[string]dbus.Variant{}
	if utf8.Valid(ssid) && bytes.IndexByte(ssid, 0) < 0 {
		args["ssid"] = dbus.MakeVariant(string(ssid))
	} else {
		args["ssid"] = dbus.MakeVariant(bytes.Clone(ssid))
	}
	switch {
	case len(credentials) == 0:
		args["key_mgmt"] = dbus.MakeVariant("NONE")
		return args, ""
	case len(credentials) == rawPSKHexLength:
		if psk, err := hex.DecodeString(string(credentials)); err == nil {
			args["psk"] = dbus.MakeVariant(psk)
			args["key_mgmt"] = dbus.MakeVariant("SAE WPA-PSK")
			return args, ""
		}
	}
	if !utf8.Valid(credentials) || bytes.IndexByte(credentials, 0) >= 0 {
		return nil, "the credential is not valid UTF-8 or holds a NUL; wpa_supplicant's D-Bus AddNetwork takes a passphrase as a string"
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
		d.mu.Lock()
		close(d.closed)
		d.mu.Unlock()
		// A join ended by Close removes its network over the bus first.
		d.calls.Wait()
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
