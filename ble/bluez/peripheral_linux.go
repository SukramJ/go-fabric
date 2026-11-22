// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/transport/btp"
)

// The peripheral's BlueZ plumbing follows connectedhomeip's Linux
// platform at 6170af84 (src/platform/Linux/bluez/), the authority where
// matter.js — whose peripheral is bleno on a raw HCI socket — has nothing
// to mirror: a GATT application under an ObjectManager root, registered
// with GattManager1.RegisterApplication; C1 written through AcquireWrite
// and C2 indicated through AcquireNotify, each a socket pair whose other
// end bluetoothd holds; the advertisement an LEAdvertisement1 object
// registered with LEAdvertisingManager1.RegisterAdvertisement and updated
// in place through its properties.

// peripheralTimeout bounds each BlueZ call the peripheral makes on its own
// (registration, a disconnect, the teardown), as the central bounds its
// calls (central_linux.go).
const peripheralTimeout = 5 * time.Second

// ErrServing is a [contract.GATTPeripheral.RegisterService] while the
// Matter GATT service is registered — from its registration until its
// [contract.GATTService.Serve] returned, or Close ended a registration that
// was never served — and a second Serve of one registration. The node
// registers one service at a time.
var ErrServing = errors.New("bluez: the Matter GATT service is registered already")

// Method and signal names of the exported side.
const (
	methodProps       = ifaceProperties
	errInvalidArgs    = "org.bluez.Error.InvalidArguments"
	errFailed         = "org.bluez.Error.Failed"
	errNotPermitted   = "org.bluez.Error.NotPermitted"
	errInvalidOffset  = "org.bluez.Error.InvalidOffset"
	errDoesNotExist   = "org.bluez.Error.DoesNotExist"
	dbusErrInvalid    = "org.freedesktop.DBus.Error.InvalidArgs"
	deviceNamePrefix  = "MATTER-"
	appearanceUnknown = 0xffff
)

// NewPeripheral is a [contract.GATTPeripheral] on the BlueZ adapter (hci0
// when empty) over the system bus — the radio a device-side host hands its
// node (node.Network.GATT). The returned value also has Close, which ends
// the service's Serve, unregisters a service that is registered but not
// served, withdraws the advertisement and releases the bus. It needs the
// system-bus rights the central needs, and bluetoothd's LE advertising
// manager and GATT server. An indication's confirmation is read from the
// notify socket (BlueZ 5.80 and later) or taken from C2's Confirm method
// (BlueZ up to 5.73) — connectedhomeip's note on the versions between
// (src/platform/Linux/bluez/BluezConnection.cpp:220-223 at 6170af84)
// applies here too.
func NewPeripheral(adapter string) (contract.GATTPeripheral, error) {
	if adapter == "" {
		adapter = DefaultAdapter
	}
	path := adapterPath(adapter)
	b, err := dialSystemBus(path)
	if err != nil {
		return nil, err
	}
	p, err := newPeripheral(b, path)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	return p, nil
}

// peripheral is the BlueZ peripheral.
type peripheral struct {
	bus     bus
	adapter dbus.ObjectPath
	// root is the GATT application's ObjectManager root, one per
	// adapter (chip: "/chipoble/<pid>", BluezEndpoint.cpp:526).
	root dbus.ObjectPath
	done chan struct{}
	// dispatched is closed when the signal loop returned.
	dispatched chan struct{}

	// mu guards every field below.
	mu sync.Mutex
	// closed is set by Close.
	closed bool
	// adv is the advertisement's properties; registered whether BlueZ
	// holds it. advOwed is an UnregisterAdvertisement that failed: the
	// registration and its objects are kept, and the unregister is
	// retried by the next Advertise or StopAdvertising (review II,
	// R2-P6-err-2012).
	adv        map[string]dbus.Variant
	registered bool
	advOwed    bool
	// appOwed is an UnregisterApplication that failed at the end of a
	// service's Serve: the application stays exported, and the next
	// RegisterService retries the unregister before it registers
	// (R2-P6-err-2012).
	appOwed bool
	// svc is the Matter GATT service from its RegisterService until its
	// registration ended (Serve returned, or Close ended it unserved); nil
	// without one. c3 is its C3 value, RegisterService's copy.
	svc *gattService
	c3  []byte
	// conns are the centrals that use the Matter service, by device
	// path.
	conns map[dbus.ObjectPath]*pconn
	// writeAcquired and notifyAcquired are C1's WriteAcquired and C2's
	// NotifyAcquired as last published: true while a connection holds a
	// socket of the characteristic (refreshAcquired).
	writeAcquired, notifyAcquired bool
	// acquiredMu orders the flags' publication: the state is read, the
	// field set and the PropertiesChanged sent under it, so the signals
	// follow one another in the order of the states they report.
	acquiredMu sync.Mutex
	// advMu serialises Advertise and StopAdvertising, each of which reads
	// the registration, calls BlueZ and records the result (review II,
	// R2-P6-conc-2013). Taken before mu.
	advMu sync.Mutex
	// bluezOwner is the unique bus name that owns org.bluez as last read:
	// the one caller the exported methods serve (fromBluez).
	bluezOwner string
}

var (
	_ contract.GATTPeripheral = (*peripheral)(nil)
	_ contract.GATTService    = (*gattService)(nil)
)

// newPeripheral is the peripheral over b: the adapter must exist.
func newPeripheral(b bus, adapter dbus.ObjectPath) (*peripheral, error) {
	ctx, cancel := context.WithTimeout(context.Background(), peripheralTimeout)
	defer cancel()
	objs, err := getManagedObjects(ctx, b)
	if err != nil {
		return nil, err
	}
	if _, ok := objs[adapter][ifaceAdapter]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoAdapter, adapter)
	}
	name := string(adapter)[strings.LastIndex(string(adapter), "/")+1:]
	p := &peripheral{
		bus: b, adapter: adapter, root: dbus.ObjectPath("/gofabric/" + name),
		done: make(chan struct{}), dispatched: make(chan struct{}),
		conns: map[dbus.ObjectPath]*pconn{},
	}
	go p.dispatch()
	return p, nil
}

func (p *peripheral) servicePath() dbus.ObjectPath { return p.root + "/service" }
func (p *peripheral) charPath(n int) dbus.ObjectPath {
	return p.servicePath() + dbus.ObjectPath(fmt.Sprintf("/c%d", n))
}
func (p *peripheral) advPath() dbus.ObjectPath { return p.root + "/advertising" }

// Close ends the service's Serve, unregisters a service that is
// registered but not served (nothing else would: RegisterService's ctx
// bounds the registration only), withdraws the advertisement and releases
// the bus. A registration still in flight sees the close when BlueZ
// answered it and unregisters itself (RegisterService).
func (p *peripheral) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.dispatched
		return nil
	}
	p.closed = true
	unserved := p.svc
	if unserved != nil && unserved.state == svcRegistered {
		unserved.state = svcEnded
	} else {
		unserved = nil
	}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), peripheralTimeout)
	defer cancel()
	_ = p.StopAdvertising(ctx)
	if unserved != nil {
		close(unserved.ended)
		_ = p.unregisterApplication(ctx)
		p.endRegistration(unserved)
	}
	close(p.done)
	err := p.bus.Close()
	<-p.dispatched
	return err
}

// dispatch follows the devices: a central that disconnects ends its
// connection (chip UpdateConnectionTable on Connected=false,
// BluezEndpoint.cpp:296-323).
func (p *peripheral) dispatch() {
	defer close(p.dispatched)
	for {
		select {
		case <-p.done:
			p.dropAll(false)
			return
		case sig, ok := <-p.bus.Signals():
			if !ok {
				p.dropAll(false)
				return
			}
			p.handle(sig)
		}
	}
}

func (p *peripheral) handle(sig *dbus.Signal) {
	switch sig.Name {
	case sigIfacesRemoved:
		var path dbus.ObjectPath
		var ifaces []string
		if dbus.Store(sig.Body, &path, &ifaces) != nil {
			return
		}
		for _, i := range ifaces {
			if i == ifaceDevice {
				p.gone(path)
			}
		}
	case sigPropsChanged:
		if len(sig.Body) < 2 {
			return
		}
		iface, _ := sig.Body[0].(string)
		props, _ := sig.Body[1].(map[string]dbus.Variant)
		if iface != ifaceDevice {
			return
		}
		if on, ok := variantAs[bool](props["Connected"], hasKey(props, "Connected")); ok && !on {
			p.gone(sig.Path)
		}
	}
}

// gone ends the connection of the device at path.
func (p *peripheral) gone(path dbus.ObjectPath) {
	p.mu.Lock()
	c := p.conns[path]
	p.mu.Unlock()
	if c != nil {
		c.markGone()
	}
}

// forget removes a connection that ended.
func (p *peripheral) forget(c *pconn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns[c.device] == c {
		delete(p.conns, c.device)
	}
}

// dropAll ends every connection; disconnect also asks BlueZ to drop each
// device (chip CloseConnectionImpl, BluezConnection.cpp:148-160).
func (p *peripheral) dropAll(disconnect bool) {
	p.mu.Lock()
	conns := make([]*pconn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	for _, c := range conns {
		if disconnect {
			_ = c.Disconnect()
		} else {
			c.markGone()
		}
	}
}

// --- the advertisement --------------------------------------------------

// Advertise implements contract.GATTPeripheral: an LEAdvertisement1 of
// type peripheral with the Matter service UUID, the service data of the
// payload, Discoverable (so BlueZ sets "BR/EDR not supported"), the name
// MATTER-<discriminator>, no appearance and the interval as both minimum
// and maximum (chip BluezAdvertisement.cpp:43-90, :144-189;
// BLEManagerImpl.cpp:377 for the name). The first registers it
// (LEAdvertisingManager1.RegisterAdvertisement, :256-260); a later one
// changes its properties, which BlueZ applies to the running
// advertisement (:144-152; BLEManagerImpl.cpp:770-790). matter.js hands
// bleno the payload's AD structures as they are and sets no name
// (BlenoBleServer.ts:352-387 at b25ed960); BlueZ composes the
// advertisement from properties, so the service data is taken from them.
func (p *peripheral) Advertise(ctx context.Context, adv contract.BLEAdvertisement) error {
	sd, ok := matterServiceData(adv.Data)
	if !ok || len(sd) < 3 {
		return fmt.Errorf("bluez: advertisement %x carries no Matter service data", adv.Data)
	}
	ms := uint32(max(adv.Interval/time.Millisecond, 20)) //nolint:gosec // an advertising interval
	discriminator := binary.LittleEndian.Uint16(sd[1:3]) & 0x0fff
	props := map[string]dbus.Variant{
		"Type":         dbus.MakeVariant("peripheral"),
		"ServiceUUIDs": dbus.MakeVariant([]string{btp.ServiceUUIDShort}),
		"ServiceData":  dbus.MakeVariant(map[string]dbus.Variant{btp.ServiceUUIDShort: dbus.MakeVariant(sd)}),
		"Discoverable": dbus.MakeVariant(true),
		"LocalName":    dbus.MakeVariant(fmt.Sprintf("%s%04d", deviceNamePrefix, discriminator)),
		"Appearance":   dbus.MakeVariant(uint16(appearanceUnknown)),
		"MinInterval":  dbus.MakeVariant(ms),
		"MaxInterval":  dbus.MakeVariant(ms),
	}
	p.advMu.Lock()
	defer p.advMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errPeripheralClosed
	}
	registered, owed := p.registered, p.advOwed
	p.mu.Unlock()
	if owed {
		// A StopAdvertising whose unregister failed: BlueZ may hold the
		// registration or may have dropped it, so it is released before
		// a fresh one rather than changed in place.
		if err := p.unregisterAdvertisement(ctx); err != nil {
			return err
		}
		registered = false
	}
	p.mu.Lock()
	p.adv = props
	p.mu.Unlock()
	if registered {
		changed := map[string]dbus.Variant{"ServiceData": props["ServiceData"], "LocalName": props["LocalName"], "MinInterval": props["MinInterval"], "MaxInterval": props["MaxInterval"]}
		if err := p.bus.Emit(p.advPath(), sigPropsChanged, ifaceAdvertise, changed, []string{}); err != nil {
			return wrap(sigPropsChanged, p.advPath(), err)
		}
		return nil
	}
	//nolint:contextcheck // the exported handlers run on the bus's goroutines, outside the caller's context; fromBluez bounds its own call
	if err := p.exportAdvertisement(); err != nil {
		return err
	}
	if _, err := call(ctx, p.bus, p.adapter, ifaceAdvertising+".RegisterAdvertisement", p.advPath(), map[string]dbus.Variant{}); err != nil {
		p.releaseUnanswered(ctx, err, ifaceAdvertising+".UnregisterAdvertisement", p.advPath())
		p.unexportAdvertisement()
		return err
	}
	p.mu.Lock()
	p.registered = true
	p.mu.Unlock()
	return nil
}

// StopAdvertising implements contract.GATTPeripheral:
// LEAdvertisingManager1.UnregisterAdvertisement (chip
// BluezAdvertisement.cpp:294-318).
func (p *peripheral) StopAdvertising(ctx context.Context) error {
	p.advMu.Lock()
	defer p.advMu.Unlock()
	p.mu.Lock()
	registered := p.registered
	if !registered {
		p.adv = nil
	}
	p.mu.Unlock()
	if !registered {
		return nil
	}
	return p.unregisterAdvertisement(ctx)
}

// unregisterAdvertisement is LEAdvertisingManager1.UnregisterAdvertisement;
// the caller holds advMu. Answered — or DoesNotExist, released by BlueZ
// meanwhile — the advertisement is forgotten and unexported. Any other
// failure (a timeout, NoReply, a bus error) leaves BlueZ's side unknown:
// the registration, its properties and its objects are kept, so the
// advertisement BlueZ may still send stays the one this process answers
// for, and the unregister is owed to the next Advertise or
// StopAdvertising (review II, R2-P6-err-2012).
func (p *peripheral) unregisterAdvertisement(ctx context.Context) error {
	_, err := call(ctx, p.bus, p.adapter, ifaceAdvertising+".UnregisterAdvertisement", p.advPath())
	if err != nil && dbusName(err) != errDoesNotExist {
		p.mu.Lock()
		p.advOwed = p.registered // Release may have come meanwhile
		p.mu.Unlock()
		return err
	}
	p.mu.Lock()
	p.registered, p.advOwed, p.adv = false, false, nil
	p.mu.Unlock()
	p.unexportAdvertisement()
	return nil
}

// releaseUnanswered undoes a Register call that failed without BlueZ's
// answer — its context ended, the bus daemon gave up waiting, the
// connection broke — before the caller unexports the objects: bluetoothd
// may still carry such a call out, and the peripheral would believe
// nothing registered while BlueZ held the path. The matching Unregister
// runs on a fresh bounded context, its error (DoesNotExist when nothing
// was registered) ignored. A refusal BlueZ answered (an error with a
// D-Bus name other than NoReply) registered nothing and needs no undo.
// Whether bluetoothd completes a registration whose caller stopped
// waiting is not verified (review II, R2-P1-drv-03).
func (p *peripheral) releaseUnanswered(ctx context.Context, err error, unregister string, path dbus.ObjectPath) {
	if name := dbusName(err); name != "" && name != "org.freedesktop.DBus.Error.NoReply" {
		return
	}
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), peripheralTimeout)
	defer cancel()
	_, _ = call(uctx, p.bus, p.adapter, unregister, path)
}

func (p *peripheral) exportAdvertisement() error {
	path := p.advPath()
	if err := p.bus.Export(path, ifaceAdvertise, map[string]any{"Release": p.release}); err != nil {
		return wrap("export "+ifaceAdvertise, path, err)
	}
	if err := p.bus.Export(path, methodProps, p.propertiesOf(path)); err != nil {
		return wrap("export "+methodProps, path, err)
	}
	return nil
}

func (p *peripheral) unexportAdvertisement() {
	_ = p.bus.Export(p.advPath(), ifaceAdvertise, nil)
	_ = p.bus.Export(p.advPath(), methodProps, nil)
}

// release is LEAdvertisement1.Release: BlueZ dropped the advertisement
// (chip BluezAdvertisement.cpp:92-101).
func (p *peripheral) release(sender dbus.Sender) *dbus.Error {
	if derr := p.fromBluez(sender); derr != nil {
		return derr
	}
	p.mu.Lock()
	p.registered, p.advOwed = false, false
	p.mu.Unlock()
	p.unexportAdvertisement()
	return nil
}

// fromBluez admits a call on an exported method only from the owner of
// org.bluez: the methods create connections, sockets and goroutines
// (AcquireWrite, AcquireNotify), complete indications (Confirm), drop the
// advertisement (Release) and hand out C3 and the objects' properties
// (ReadValue, Properties.Get and GetAll, GetManagedObjects), and whoever
// else the system bus lets call this connection's objects is not
// bluetoothd (review II, R2-P6-sec-2014). bluetoothd reads the objects
// through the getters when it registers the application and the
// advertisement, from the connection that owns org.bluez. The owner is read with GetNameOwner and kept; a caller
// that is not the kept one has the owner read again, so a bluetoothd that
// restarted under a new unique name is followed. Any other caller, and
// every caller while the owner cannot be read, is refused with
// NotPermitted.
func (p *peripheral) fromBluez(sender dbus.Sender) *dbus.Error {
	p.mu.Lock()
	owner := p.bluezOwner
	p.mu.Unlock()
	if sender != "" && string(sender) == owner {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), peripheralTimeout)
	defer cancel()
	owner, err := p.bus.NameOwner(ctx, service)
	if err != nil {
		return dbus.NewError(errNotPermitted, []any{"the owner of " + service + " is unknown"})
	}
	p.mu.Lock()
	p.bluezOwner = owner
	p.mu.Unlock()
	if sender == "" || string(sender) != owner {
		return dbus.NewError(errNotPermitted, []any{"only " + service + " may call"})
	}
	return nil
}

// matterServiceData finds the service data of the Matter service (16-bit
// UUID 0xFFF6) among the AD structures of data.
func matterServiceData(data []byte) ([]byte, bool) {
	for len(data) > 0 {
		n := int(data[0])
		if n == 0 || n+1 > len(data) {
			return nil, false
		}
		ad := data[1 : n+1]
		if ad[0] == 0x16 && len(ad) >= 3 && ad[1] == 0xf6 && ad[2] == 0xff {
			return bytes.Clone(ad[3:]), true
		}
		data = data[n+1:]
	}
	return nil, false
}

// --- the GATT application ----------------------------------------------

// RegisterService implements contract.GATTPeripheral: the GATT application
// — the service 0xFFF6, C1 with the flag "write" taken through
// AcquireWrite, C2 with "indicate" through AcquireNotify and Confirm, C3
// with "read" (chip BluezEndpoint.cpp:448-520; C3 always, as matter.js
// serves it, BlenoBleServer.ts:92-120) — is exported under its
// ObjectManager root (:522-534) and registered with
// GattManager1.RegisterApplication (:267-294). An unregister owed by the
// previous registration is retried first. c3 is copied. ctx bounds the
// registration only: the service stays registered until its Serve
// returned, or — never served — until Close. A registration while one is
// registered is refused with [ErrServing].
func (p *peripheral) RegisterService(ctx context.Context, c3 []byte) (contract.GATTService, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errPeripheralClosed
	}
	if p.svc != nil {
		p.mu.Unlock()
		return nil, ErrServing
	}
	s := &gattService{p: p, state: svcRegistering, served: make(chan struct{}), ended: make(chan struct{})}
	p.svc, p.c3 = s, bytes.Clone(c3)
	owed := p.appOwed
	p.mu.Unlock()
	p.acquiredMu.Lock()
	p.mu.Lock()
	p.writeAcquired, p.notifyAcquired = false, false
	p.mu.Unlock()
	p.acquiredMu.Unlock()
	fail := func(err error) (contract.GATTService, error) {
		p.mu.Lock()
		s.state = svcEnded
		p.mu.Unlock()
		close(s.ended)
		p.endRegistration(s)
		return nil, err
	}
	if owed {
		if err := p.unregisterApplication(ctx); err != nil {
			return fail(err)
		}
	}
	//nolint:contextcheck // the exported handlers run on the bus's goroutines, outside RegisterService's context; fromBluez bounds its own call
	if err := p.exportApplication(); err != nil {
		p.unexportApplication()
		return fail(err)
	}
	regCtx, cancel := context.WithTimeout(ctx, peripheralTimeout)
	_, err := call(regCtx, p.bus, p.adapter, ifaceGattManager+".RegisterApplication", p.root, map[string]dbus.Variant{})
	cancel()
	if err != nil {
		p.releaseUnanswered(ctx, err, ifaceGattManager+".UnregisterApplication", p.root)
		p.unexportApplication()
		return fail(err)
	}
	p.mu.Lock()
	closed := p.closed
	if !closed {
		s.state = svcRegistered
	}
	p.mu.Unlock()
	if closed {
		// Close ran while BlueZ registered the application and left the
		// registration in flight to this call.
		_ = p.unregisterApplication(context.WithoutCancel(ctx))
		return fail(errPeripheralClosed)
	}
	return s, nil
}

// serviceState is where a [gattService] is in its life, guarded by the
// peripheral's mu.
type serviceState int

const (
	// svcRegistering: RegisterService runs.
	svcRegistering serviceState = iota
	// svcRegistered: registered, Serve not called yet.
	svcRegistered
	// svcServing: Serve runs and accepts centrals.
	svcServing
	// svcEnded: no central is accepted any more; the registration is
	// ending or ended.
	svcEnded
)

// gattService is one registration of the Matter GATT service.
type gattService struct {
	p *peripheral
	// served is closed when Serve installed accept; ended when the
	// service stopped accepting centrals (Serve's end, Close of an
	// unserved registration, a failed registration).
	served, ended chan struct{}
	// state, accept and serveCalled are guarded by p.mu.
	state       serviceState
	accept      func(contract.BLEConnection)
	serveCalled bool
	// acceptMu keeps the calls of accept from overlapping: godbus runs
	// each exported method call on a goroutine of its own, so two
	// centrals' first acquires may arrive at once.
	acceptMu sync.Mutex
	// accepting counts the accept calls under way; Serve waits for them
	// before it drops the centrals and returns, so accept is not called
	// after Serve returned.
	accepting sync.WaitGroup
}

// Serve implements contract.GATTService: accept runs for a central at its
// first AcquireWrite or AcquireNotify, before any event of it is
// delivered, one call at a time. When ctx ends — or Close ends the
// peripheral — every central is disconnected and the application
// unregistered; a failed unregister is Serve's error and stays owed to the
// next RegisterService. A second Serve is refused with [ErrServing]; a
// Serve after Close ended the unserved registration returns at once with
// the peripheral's closed error.
func (s *gattService) Serve(ctx context.Context, accept func(contract.BLEConnection)) error {
	p := s.p
	p.mu.Lock()
	if s.serveCalled {
		p.mu.Unlock()
		return ErrServing
	}
	s.serveCalled = true
	if s.state != svcRegistered {
		p.mu.Unlock()
		return errPeripheralClosed
	}
	s.state, s.accept = svcServing, accept
	p.mu.Unlock()
	close(s.served)
	select {
	case <-ctx.Done():
	case <-p.done:
	}
	// No central is accepted from here on, so none outlives the drop.
	p.mu.Lock()
	s.state = svcEnded
	p.mu.Unlock()
	close(s.ended)
	s.accepting.Wait()
	p.dropAll(true) //nolint:contextcheck // Serve's context has ended; each disconnect runs on a fresh bounded one
	err := p.unregisterApplication(context.WithoutCancel(ctx))
	p.endRegistration(s)
	return err
}

// endRegistration releases the peripheral for the next RegisterService
// once s ended; an unregister that failed stays owed (appOwed).
func (p *peripheral) endRegistration(s *gattService) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.svc == s {
		p.svc, p.c3 = nil, nil
	}
}

// unregisterApplication is GattManager1.UnregisterApplication on a context
// bounded by peripheralTimeout; the caller is the one that ends the
// registration (Serve, Close, or RegisterService itself). Answered — or
// DoesNotExist, or the bus going away with the application — the
// application is unexported. Any other failure keeps it exported and the
// unregister owed to the next RegisterService, as unregisterAdvertisement
// keeps the advertisement (review II, R2-P6-err-2012): no central reaches
// it meanwhile, since connFor accepts only while Serve runs.
func (p *peripheral) unregisterApplication(ctx context.Context) error {
	unregCtx, cancel := context.WithTimeout(ctx, peripheralTimeout)
	defer cancel()
	if _, err := call(unregCtx, p.bus, p.adapter, ifaceGattManager+".UnregisterApplication", p.root); err != nil && dbusName(err) != errDoesNotExist {
		select {
		case <-p.done: // the bus is going away with the application
		default:
			p.mu.Lock()
			p.appOwed = true
			p.mu.Unlock()
			return err
		}
	}
	p.mu.Lock()
	p.appOwed = false
	p.mu.Unlock()
	p.unexportApplication()
	return nil
}

// objects is the application's object tree as GetManagedObjects answers
// it: the service and its three characteristics, and the advertisement
// while it exists (chip exports both under one ObjectManager,
// BluezAdvertisement.cpp:86). WriteAcquired and NotifyAcquired are what
// make BlueZ use AcquireWrite and AcquireNotify.
func (p *peripheral) objects() map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	p.mu.Lock()
	c3 := bytes.Clone(p.c3)
	adv := p.adv
	registered := p.registered
	writeAcquired, notifyAcquired := p.writeAcquired, p.notifyAcquired
	p.mu.Unlock()
	svc := p.servicePath()
	objs := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		svc: {ifaceGattService: {
			"UUID":    dbus.MakeVariant(btp.ServiceUUIDShort),
			"Primary": dbus.MakeVariant(true),
		}},
		p.charPath(1): {ifaceGattChar: {
			"UUID":          dbus.MakeVariant(strings.ToLower(btp.C1CharacteristicUUID)),
			"Service":       dbus.MakeVariant(svc),
			"Flags":         dbus.MakeVariant([]string{"write"}),
			"WriteAcquired": dbus.MakeVariant(writeAcquired),
		}},
		p.charPath(2): {ifaceGattChar: {
			"UUID":           dbus.MakeVariant(strings.ToLower(btp.C2CharacteristicUUID)),
			"Service":        dbus.MakeVariant(svc),
			"Flags":          dbus.MakeVariant([]string{"indicate"}),
			"NotifyAcquired": dbus.MakeVariant(notifyAcquired),
		}},
		p.charPath(3): {ifaceGattChar: {
			"UUID":    dbus.MakeVariant(strings.ToLower(btp.C3CharacteristicUUID)),
			"Service": dbus.MakeVariant(svc),
			"Flags":   dbus.MakeVariant([]string{"read"}),
			"Value":   dbus.MakeVariant(c3),
		}},
	}
	if registered && adv != nil {
		objs[p.advPath()] = map[string]map[string]dbus.Variant{ifaceAdvertise: adv}
	}
	return objs
}

// propertiesOf is org.freedesktop.DBus.Properties of one exported object.
func (p *peripheral) propertiesOf(path dbus.ObjectPath) map[string]any {
	lookup := func(iface string) (map[string]dbus.Variant, bool) {
		if path == p.advPath() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if iface != ifaceAdvertise || p.adv == nil {
				return nil, false
			}
			return p.adv, true
		}
		props, ok := p.objects()[path][iface]
		return props, ok
	}
	return map[string]any{
		"Get": func(sender dbus.Sender, iface, name string) (dbus.Variant, *dbus.Error) {
			if derr := p.fromBluez(sender); derr != nil {
				return dbus.Variant{}, derr
			}
			props, ok := lookup(iface)
			if v, has := props[name]; ok && has {
				return v, nil
			}
			return dbus.Variant{}, dbus.NewError(dbusErrInvalid, []any{"no property " + iface + "." + name})
		},
		"GetAll": func(sender dbus.Sender, iface string) (map[string]dbus.Variant, *dbus.Error) {
			if derr := p.fromBluez(sender); derr != nil {
				return nil, derr
			}
			props, ok := lookup(iface)
			if !ok {
				return map[string]dbus.Variant{}, nil
			}
			return props, nil
		},
	}
}

func (p *peripheral) exportApplication() error {
	type export struct {
		path    dbus.ObjectPath
		iface   string
		methods map[string]any
	}
	exports := make([]export, 0, 8)
	exports = append(exports, []export{
		{p.root, ifaceObjectMgr, map[string]any{"GetManagedObjects": func(sender dbus.Sender) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
			if derr := p.fromBluez(sender); derr != nil {
				return nil, derr
			}
			return p.objects(), nil
		}}},
		{p.charPath(1), ifaceGattChar, map[string]any{"AcquireWrite": p.acquireWrite}},
		{p.charPath(2), ifaceGattChar, map[string]any{"AcquireNotify": p.acquireNotify, "Confirm": p.confirm}},
		{p.charPath(3), ifaceGattChar, map[string]any{"ReadValue": p.readC3}},
	}...)
	for _, path := range []dbus.ObjectPath{p.servicePath(), p.charPath(1), p.charPath(2), p.charPath(3)} {
		exports = append(exports, export{path, methodProps, p.propertiesOf(path)})
	}
	for _, e := range exports {
		if err := p.bus.Export(e.path, e.iface, e.methods); err != nil {
			return wrap("export "+e.iface, e.path, err)
		}
	}
	return nil
}

func (p *peripheral) unexportApplication() {
	_ = p.bus.Export(p.root, ifaceObjectMgr, nil)
	for _, path := range []dbus.ObjectPath{p.servicePath(), p.charPath(1), p.charPath(2), p.charPath(3)} {
		_ = p.bus.Export(path, ifaceGattChar, nil)
		_ = p.bus.Export(path, methodProps, nil)
	}
}

// readC3 is C3's ReadValue: the additional data from the requested
// offset; an offset beyond it is refused (matter.js
// BlenoBleServer.ts:344-350, RESULT_INVALID_OFFSET :106-109).
func (p *peripheral) readC3(sender dbus.Sender, options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	if derr := p.fromBluez(sender); derr != nil {
		return nil, derr
	}
	p.mu.Lock()
	c3 := bytes.Clone(p.c3)
	p.mu.Unlock()
	offset, _ := variantAs[uint16](options["offset"], hasKey(options, "offset"))
	if int(offset) > len(c3) {
		return nil, dbus.NewError(errInvalidOffset, []any{fmt.Sprintf("offset %d beyond %d bytes", offset, len(c3))})
	}
	return c3[offset:], nil
}

// deviceOf reads the options of AcquireWrite and AcquireNotify: the
// device and the ATT_MTU (chip BluezEndpoint.cpp:106-113, :163-170).
func deviceOf(options map[string]dbus.Variant) (dbus.ObjectPath, uint16, *dbus.Error) {
	device, ok := variantAs[dbus.ObjectPath](options["device"], hasKey(options, "device"))
	if !ok {
		return "", 0, dbus.NewError(errInvalidArgs, []any{"No device object path"})
	}
	mtu, ok := variantAs[uint16](options["mtu"], hasKey(options, "mtu"))
	if !ok {
		return "", 0, dbus.NewError(errInvalidArgs, []any{"No MTU value"})
	}
	return device, mtu, nil
}

// connFor is the connection of device, accepted at its first use of the
// Matter service while Serve runs. A central that acquires C1 or C2 after
// RegisterService returned but before Serve installed accept — the
// service is registered, so bluetoothd may route it here, and the
// contract has Serve call accept for every central that connects — is
// held, not refused: the call waits until Serve installed accept, the
// registration ended (refused then, as after Serve) or peripheralTimeout
// passed (refused). A deprecated BLEPeripheral never had that gap, since
// its Serve registered and served in one call.
func (p *peripheral) connFor(device dbus.ObjectPath) (*pconn, *dbus.Error) {
	refused := dbus.NewError(errFailed, []any{"No CHIPoBLE connection for the device"})
	p.mu.Lock()
	s := p.svc
	p.mu.Unlock()
	if s == nil {
		return nil, refused
	}
	wait := time.NewTimer(peripheralTimeout)
	select {
	case <-s.served:
	case <-s.ended:
	case <-p.done:
	case <-wait.C:
	}
	wait.Stop()
	p.mu.Lock()
	if p.svc != s || s.state != svcServing {
		p.mu.Unlock()
		return nil, refused
	}
	if c := p.conns[device]; c != nil {
		p.mu.Unlock()
		return c, nil
	}
	c := newPConn(p, device)
	p.conns[device] = c
	accept := s.accept
	s.accepting.Add(1)
	p.mu.Unlock()
	s.acceptMu.Lock()
	accept(c)
	s.acceptMu.Unlock()
	s.accepting.Done()
	c.startEvents()
	go c.checkConnected()
	return c, nil
}

// socketPair is chip's socket pair for an acquired characteristic:
// sequential packets, non-blocking (BluezEndpoint.cpp:123, :180). The
// first end stays here, the second goes to bluetoothd.
func socketPair() (*os.File, int, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, -1, err
	}
	return os.NewFile(uintptr(fds[0]), "btp"), fds[1], nil
}

// refreshAcquired publishes C1's WriteAcquired (n 1) or C2's
// NotifyAcquired (n 2): true while any connection holds a socket of the
// characteristic, announced as PropertiesChanged on the characteristic
// when it changes. connectedhomeip sets the property true once it handed
// out the socket (bluez_gatt_characteristic1_set_write_acquired,
// BluezEndpoint.cpp:129; set_notify_acquired, :186 at 6170af84), before
// it completes the call; a GATT server that decides by the property
// (bluezoo's GattCharacteristicClientLink.WriteValue) then writes into the
// socket it holds instead of acquiring a new one for each write.
// connectedhomeip never sets it back; here it follows the sockets, so the
// next central acquires its own. It is derived from the connections, not
// set by the caller: a socket that ends after another central acquired,
// or a link that drops between an acquire and its publication, cannot
// leave it wrong. Callers run it after every change of a socket.
func (p *peripheral) refreshAcquired(n int) {
	p.acquiredMu.Lock()
	defer p.acquiredMu.Unlock()
	p.mu.Lock()
	conns := make([]*pconn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	on := false
	for _, c := range conns {
		c.mu.Lock()
		held := (n == 1 && c.write != nil) || (n == 2 && c.notify != nil)
		c.mu.Unlock()
		if held {
			on = true
			break
		}
	}
	name := "WriteAcquired"
	p.mu.Lock()
	field := &p.writeAcquired
	if n == 2 {
		name, field = "NotifyAcquired", &p.notifyAcquired
	}
	changed := *field != on
	*field = on
	p.mu.Unlock()
	if changed {
		_ = p.bus.Emit(p.charPath(n), sigPropsChanged, ifaceGattChar, map[string]dbus.Variant{name: dbus.MakeVariant(on)}, []string{})
	}
}

// acquireWrite is C1's AcquireWrite: a socket pair whose writes are C1's
// (chip BluezCharacteristicAcquireWrite, BluezEndpoint.cpp:97-136).
func (p *peripheral) acquireWrite(sender dbus.Sender, options map[string]dbus.Variant) (dbus.UnixFD, uint16, *dbus.Error) {
	if derr := p.fromBluez(sender); derr != nil {
		return 0, 0, derr
	}
	device, mtu, derr := deviceOf(options)
	if derr != nil {
		return 0, 0, derr
	}
	c, derr := p.connFor(device)
	if derr != nil {
		return 0, 0, derr
	}
	f, remote, err := socketPair()
	if err != nil {
		return 0, 0, dbus.NewError(errFailed, []any{"FD creation failed"})
	}
	c.setMTU(mtu)
	if !c.setWrite(f) {
		_ = f.Close()
		_ = syscall.Close(remote)
		return 0, 0, dbus.NewError(errFailed, []any{"the connection is gone"})
	}
	p.refreshAcquired(1)
	return dbus.UnixFD(remote), mtu, nil //nolint:gosec // a file descriptor, small and non-negative
}

// acquireNotify is C2's AcquireNotify: a socket pair whose writes are C2's
// indications, refused while one is acquired (chip
// BluezCharacteristicAcquireNotify, BluezEndpoint.cpp:147-196); the
// subscription it is reaches the connection (:192).
func (p *peripheral) acquireNotify(sender dbus.Sender, options map[string]dbus.Variant) (dbus.UnixFD, uint16, *dbus.Error) {
	if derr := p.fromBluez(sender); derr != nil {
		return 0, 0, derr
	}
	device, mtu, derr := deviceOf(options)
	if derr != nil {
		return 0, 0, derr
	}
	c, derr := p.connFor(device)
	if derr != nil {
		return 0, 0, derr
	}
	if c.notifying() {
		return 0, 0, dbus.NewError(errNotPermitted, []any{"Already notifying"})
	}
	f, remote, err := socketPair()
	if err != nil {
		return 0, 0, dbus.NewError(errFailed, []any{"FD creation failed"})
	}
	c.setMTU(mtu)
	if !c.setNotify(f, mtu) {
		_ = f.Close()
		_ = syscall.Close(remote)
		return 0, 0, dbus.NewError(errFailed, []any{"the connection is gone"})
	}
	p.refreshAcquired(2)
	return dbus.UnixFD(remote), mtu, nil //nolint:gosec // a file descriptor, small and non-negative
}

// confirm is C2's Confirm, the indication confirmation of BlueZ up to
// 5.73, which names no device (chip BluezCharacteristicConfirm,
// BluezEndpoint.cpp:207-217, takes the connection of the device seen last,
// :398-401). It completes the indication of the live connection that waits
// for one: the bridge serves one central at a time, so a refused second
// central, gone already, cannot take the served one's confirmation.
func (p *peripheral) confirm(sender dbus.Sender) *dbus.Error {
	if derr := p.fromBluez(sender); derr != nil {
		return derr
	}
	p.mu.Lock()
	conns := make([]*pconn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	for _, c := range conns {
		if !c.isGone() && c.owes() {
			c.confirmed()
			return nil
		}
	}
	return nil
}

// --- one central's connection ------------------------------------------

// pconn is one central's connection to the Matter service — chip's
// BluezConnection for the peripheral (BluezConnection.cpp).
type pconn struct {
	p      *peripheral
	device dbus.ObjectPath
	// events delivers the handlers' events in order on one goroutine.
	events chan func()
	gone   chan struct{}
	once   sync.Once

	// mu guards every field below.
	mu      sync.Mutex
	onWrite func([]byte)
	onSub   func(int)
	onUnsub func()
	mtu     uint16
	write   *os.File
	notify  *os.File
	writes  int
	heldSub bool
	heldMTU int
	// sent and acked count the indications written to the current C2
	// socket and the confirmations received for them. ATT has one
	// indication outstanding at a time and confirms in order, so the
	// n-th confirmation is the n-th indication's — also when the Indicate
	// that sent it gave up (review II, R2-P6-fsm-2015). pending is the
	// waiting Indicate's channel, closed by confirmation pendingSeq.
	sent, acked uint64
	pending     chan struct{}
	pendingSeq  uint64
	queued      []func()
	started     bool
	indicate    sync.Mutex
	// order makes counting a C1 write and posting it one step, so a
	// subscription bluetoothd reports meanwhile is posted after it, never
	// between the count and the post. Taken before mu.
	order sync.Mutex
}

var _ contract.BLEConnection = (*pconn)(nil)

func newPConn(p *peripheral, device dbus.ObjectPath) *pconn {
	return &pconn{p: p, device: device, events: make(chan func(), 256), gone: make(chan struct{})}
}

// startEvents starts delivering events, the handlers registered (accept
// returned).
func (c *pconn) startEvents() {
	c.mu.Lock()
	c.started = true
	queued := c.queued
	c.queued = nil
	c.mu.Unlock()
	go func() {
		for {
			select {
			case f := <-c.events:
				f()
			case <-c.gone:
				return
			}
		}
	}()
	for _, f := range queued {
		c.post(f)
	}
}

// post queues one event; nothing after the connection is gone.
func (c *pconn) post(f func()) {
	c.mu.Lock()
	if !c.started {
		c.queued = append(c.queued, f)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	select {
	case c.events <- f:
	case <-c.gone:
	}
}

func (c *pconn) setMTU(mtu uint16) {
	c.mu.Lock()
	c.mtu = mtu
	c.mu.Unlock()
}

func (c *pconn) isGone() bool {
	select {
	case <-c.gone:
		return true
	default:
		return false
	}
}

// setWrite installs C1's socket and reads it: each packet is one write
// (chip WriteHandlerCallback, BluezConnection.cpp:172-192). A held
// subscription follows the first write: bluetoothd delivers C1 over the
// socket and the C2 subscription over D-Bus, so their order on arrival is
// not the central's, and a central writes its handshake request before
// it subscribes (matter.js BlenoBleServer.ts:288-291 refuses the
// subscription otherwise).
func (c *pconn) setWrite(f *os.File) bool {
	c.mu.Lock()
	if c.isGone() {
		c.mu.Unlock()
		return false
	}
	old := c.write
	c.write = f
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go func() {
		buf := make([]byte, 512) // the characteristic's maximum size (:179)
		defer func() {
			// The socket ended — the central closed it, the link went, or
			// the connection was dropped; a newer AcquireWrite may have
			// replaced it. os.File also reads a zero-length packet as
			// io.EOF while bluetoothd's end is open: closing this end lets
			// bluetoothd see the hang-up instead of writing into a socket
			// nobody reads, as setNotify's reader closes its own.
			c.mu.Lock()
			if c.write == f {
				c.write = nil
			}
			c.mu.Unlock()
			_ = f.Close()
			c.p.refreshAcquired(1)
		}()
		for {
			n, err := f.Read(buf)
			if err != nil || n <= 0 {
				return
			}
			v := bytes.Clone(buf[:n])
			c.order.Lock()
			c.mu.Lock()
			c.writes++
			held, mtu := c.heldSub, c.heldMTU
			c.heldSub = false
			c.mu.Unlock()
			c.post(func() { c.deliverWrite(v) })
			if held {
				c.post(func() { c.deliverSubscribe(mtu) })
			}
			c.order.Unlock()
		}
	}()
	return true
}

// setNotify installs C2's socket — the subscription — and reads the
// confirmations BlueZ 5.80 and later write to it, one byte 1 each (chip
// NotifyHandlerCallback, BluezConnection.cpp:210-238). Its end is the end
// of the subscription: bluetoothd frees a client's notify socket when the
// central writes 0 to C2's CCC descriptor, and when the link goes
// (src/gatt-database.c ccc_write_cb, BlueZ's documented AcquireNotify
// contract). connectedhomeip does not report that end — its
// HandleTXCharCCCDWrite posts an unsubscription only for a connection
// without NotifyAcquired (BLEManagerImpl.cpp:699-712), which no caller at
// the pin produces — but matter.js closes on the unsubscription
// (BlenoBleServer.ts:81-84 at b25ed960), so the port reports it.
func (c *pconn) setNotify(f *os.File, mtu uint16) bool {
	c.order.Lock()
	c.mu.Lock()
	if c.isGone() || c.notify != nil {
		c.mu.Unlock()
		c.order.Unlock()
		return false
	}
	c.notify = f
	held := c.writes == 0
	if held {
		c.heldSub, c.heldMTU = true, int(mtu)
	}
	c.mu.Unlock()
	if !held {
		c.post(func() { c.deliverSubscribe(int(mtu)) })
	}
	c.order.Unlock()
	go func() {
		var b [1]byte
		for {
			n, err := f.Read(b[:])
			if err != nil || n <= 0 {
				break
			}
			if b[0] == 1 {
				c.confirmed()
			}
		}
		// No confirmation comes for what the closed socket carried: what
		// is owed is written off and a waiting indication woken, in the
		// step that ends the subscription, so a later subscription's
		// indication is not.
		var waiting chan struct{}
		c.mu.Lock()
		if c.notify == f {
			c.notify = nil
			c.heldSub = false
			c.acked = c.sent
			waiting, c.pending = c.pending, nil
		}
		c.mu.Unlock()
		if waiting != nil {
			close(waiting)
		}
		_ = f.Close()
		c.p.refreshAcquired(2)
		if !c.isGone() {
			c.post(c.deliverUnsubscribe)
		}
	}()
	return true
}

func (c *pconn) notifying() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notify != nil
}

func (c *pconn) deliverWrite(v []byte) {
	c.mu.Lock()
	h := c.onWrite
	c.mu.Unlock()
	if h != nil {
		h(v)
	}
}

func (c *pconn) deliverSubscribe(mtu int) {
	c.mu.Lock()
	h := c.onSub
	c.mu.Unlock()
	if h != nil {
		h(mtu)
	}
}

func (c *pconn) deliverUnsubscribe() {
	c.mu.Lock()
	h := c.onUnsub
	c.mu.Unlock()
	if h != nil {
		h()
	}
}

// owes reports whether an indication sent has no confirmation yet — also
// one whose Indicate gave up waiting.
func (c *pconn) owes() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acked < c.sent
}

// confirmed counts one confirmation against the indications sent and
// completes the Indicate that waits for that one; a confirmation with
// nothing owed is dropped.
func (c *pconn) confirmed() {
	c.mu.Lock()
	if c.acked >= c.sent {
		c.mu.Unlock()
		return
	}
	c.acked++
	var ch chan struct{}
	if c.pending != nil && c.acked == c.pendingSeq {
		ch, c.pending = c.pending, nil
	}
	c.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// abandon is an Indicate that returns without its confirmation: its
// channel is no longer the waiting one, while the count keeps the
// indication owed, so its confirmation, when it comes, is not taken for
// the next one's.
func (c *pconn) abandon(done chan struct{}) {
	c.mu.Lock()
	if c.pending == done {
		c.pending = nil
	}
	c.mu.Unlock()
}

// Remote implements contract.BLEConnection: the device's address, from
// its object path (dev_XX_XX_XX_XX_XX_XX).
func (c *pconn) Remote() string {
	last := string(c.device)[strings.LastIndex(string(c.device), "/")+1:]
	return strings.ReplaceAll(strings.TrimPrefix(last, "dev_"), "_", ":")
}

// OnWrite implements contract.BLEConnection.
func (c *pconn) OnWrite(f func([]byte)) { c.mu.Lock(); c.onWrite = f; c.mu.Unlock() }

// OnSubscribe implements contract.BLEConnection.
func (c *pconn) OnSubscribe(f func(int)) { c.mu.Lock(); c.onSub = f; c.mu.Unlock() }

// OnUnsubscribe implements contract.BLEConnection.
func (c *pconn) OnUnsubscribe(f func()) { c.mu.Lock(); c.onUnsub = f; c.mu.Unlock() }

// Indicate implements contract.BLEConnection: the value written to C2's
// socket (chip SendIndicationImpl, BluezConnection.cpp:268-289), complete
// with its confirmation — a byte 1 on the socket, or C2's Confirm.
func (c *pconn) Indicate(ctx context.Context, value []byte) error {
	c.indicate.Lock()
	defer c.indicate.Unlock()
	c.mu.Lock()
	f := c.notify
	if f == nil || c.isGone() {
		c.mu.Unlock()
		return fmt.Errorf("bluez: indicate on %s: %w", c.device, btp.ErrDisconnected)
	}
	done := make(chan struct{})
	c.sent++
	seq := c.sent
	c.pending, c.pendingSeq = done, seq
	c.mu.Unlock()
	if _, err := f.Write(value); err != nil {
		// Not sent: nothing is owed for it.
		c.mu.Lock()
		if c.pending == done {
			c.pending = nil
		}
		if c.sent == seq && c.acked < seq {
			c.sent--
		}
		c.mu.Unlock()
		return fmt.Errorf("bluez: indicate on %s: %w", c.device, err)
	}
	select {
	case <-done:
	case <-c.gone:
		c.abandon(done)
		return fmt.Errorf("bluez: indicate on %s: %w", c.device, btp.ErrDisconnected)
	case <-ctx.Done():
		c.abandon(done)
		return ctx.Err()
	}
	c.mu.Lock()
	closed := c.notify != f
	c.mu.Unlock()
	if closed {
		return fmt.Errorf("bluez: indicate on %s: the subscription ended: %w", c.device, io.ErrClosedPipe)
	}
	return nil
}

// Disconnect implements contract.BLEConnection: Device1.Disconnect (chip
// CloseConnectionImpl, BluezConnection.cpp:148-160), the connection gone
// whatever BlueZ answers.
func (c *pconn) Disconnect() error {
	if c.isGone() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), peripheralTimeout)
	defer cancel()
	_, err := call(ctx, c.p.bus, c.device, ifaceDevice+".Disconnect")
	c.markGone()
	return err
}

// Disconnected implements contract.BLEConnection.
func (c *pconn) Disconnected() <-chan struct{} { return c.gone }

// checkConnected ends a connection whose device was gone before it was
// admitted: godbus runs a method call such as AcquireWrite on a goroutine
// of its own but handles signals in order on its dispatch loop, so the
// device's Connected=false can be handled before connFor inserted the
// connection and find nothing to end. Device1.Connected is read once the
// connection is in, as the central does after its registration
// (central_linux.go), on a bounded context and off the method call, whose
// reply bluetoothd waits for. A failed read leaves the connection to the
// signals (review II, R2-P1-drv-05).
func (c *pconn) checkConnected() {
	ctx, cancel := context.WithTimeout(context.Background(), peripheralTimeout)
	defer cancel()
	v, err := getProperty(ctx, c.p.bus, c.device, ifaceDevice, "Connected")
	if err != nil {
		return
	}
	if connected, ok := variantAs[bool](v, true); ok && !connected {
		c.markGone()
	}
}

// markGone ends the connection: the sockets closed, Disconnected closed.
func (c *pconn) markGone() {
	c.once.Do(func() {
		close(c.gone)
		c.mu.Lock()
		w, n := c.write, c.notify
		c.write, c.notify = nil, nil
		c.mu.Unlock()
		if w != nil {
			_ = w.Close()
		}
		if n != nil {
			_ = n.Close()
		}
		c.p.forget(c)
		c.p.refreshAcquired(1)
		c.p.refreshAcquired(2)
	})
}

// errPeripheralClosed is a call on a closed peripheral.
var errPeripheralClosed = errors.New("bluez: the peripheral is closed")
