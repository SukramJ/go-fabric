// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

// The harness of the port of matter.js packages/node/test/behaviors/
// door-lock/DoorLockServerTest.ts at 862ad75f (the
// doorlockserver_*_parity_matterjs_test.go files).
//
// matter.js runs the file on a MockServerNode (createLock, :92-96) or on
// a commissioned MockSite pair (setUpScheduledLock, :181-262); the port
// runs both on a commissioned fabrictest pair, the lock a bridged endpoint
// of the device (fabrictest.Endpoint). Where matter.js reads the lock's
// state locally (lock.state.doorLock.users, lockState) the port reads the
// server ([lock.DoorLockServer.Users], MatterRead); where it listens to the
// lock's events (lock.events.doorLock.*.on) the port reads them from the
// device's event log over the wire, newest since the last mark. Time is the
// site's fake clock: MockTime.advance is Fake.Advance, MockTime.stepWallClock
// Fake.Set, settled Site.Settle, withMockTime the site's Run that every wire
// helper uses.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// deviceTypeDoorLock is the Door Lock device type (0x000A).
const deviceTypeDoorLock = 0x000A

// pairFabric is the fabric index of the pair's controller, the fabric the
// matter.js fixtures name FabricIndex(1).
const pairFabric = 1

// pin is DoorLockServerTest.ts:98-100.
func pin(digits string) []byte { return []byte(digits) }

func pinPtr(digits string) *[]byte {
	p := pin(digits)
	return &p
}

// lockSite holds the package's sites to one at a time: a fake site clock
// moves only while the whole test process is quiet (fabrictest's Limits).
var lockSite sync.Mutex

// testHardware stands in for lock hardware: DoorLockServerTest.ts:117-152.
type testHardware struct {
	srv *lock.DoorLockServer

	mu      sync.Mutex
	calls   []hardwareCall
	failure error
	gate    chan struct{}
	called  chan struct{}
}

// hardwareCall is one call with the lock state the hardware saw.
type hardwareCall struct {
	op        lock.LockOperation
	lockState any
}

func (h *testHardware) HandleLockOperation(_ context.Context, op lock.LockOperation) error {
	state, _ := h.srv.MatterRead(lockdef.AttrLockState)
	h.mu.Lock()
	h.calls = append(h.calls, hardwareCall{op: op, lockState: state})
	called, gate := h.called, h.gate
	h.called = nil
	h.mu.Unlock()
	if called != nil {
		close(called)
	}
	if gate != nil {
		<-gate
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failure
}

// hold holds lock operations at the hardware until the returned function
// is called (holdHardware, :133-136).
func (h *testHardware) hold() func() {
	gate := make(chan struct{})
	h.mu.Lock()
	h.gate = gate
	h.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

// nextCall is closed once the hardware receives its next lock operation
// (nextHardwareCall, :138-141).
func (h *testHardware) nextCall() <-chan struct{} {
	ch := make(chan struct{})
	h.mu.Lock()
	h.called = ch
	h.mu.Unlock()
	return ch
}

func (h *testHardware) fail(err error) {
	h.mu.Lock()
	h.failure = err
	h.mu.Unlock()
}

func (h *testHardware) snapshot() []hardwareCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hardwareCall(nil), h.calls...)
}

// lockFixture is a lock on a commissioned pair.
type lockFixture struct {
	t    *testing.T
	ctx  context.Context
	site *fabrictest.Site
	pair *fabrictest.Pair
	srv  *lock.DoorLockServer
	hw   *testHardware
	ep   uint16
	mark uint64
}

// newLockFixture builds the server cfg describes on a new site and
// commissions it, the lock a bridged endpoint of the device.
func newLockFixture(t *testing.T, cfg lock.DoorLockConfig) *lockFixture {
	t.Helper()
	lockSite.Lock()
	t.Cleanup(lockSite.Unlock)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	hw := &testHardware{}
	cfg.Clock = site.Clock()
	if cfg.Hardware == nil {
		cfg.Hardware = hw
	}
	srv, err := lock.New(cfg)
	if err != nil {
		t.Fatalf("lock.New: %v", err)
	}
	hw.srv = srv
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("lock", deviceTypeDoorLock, srv)}},
	})
	ep, ok := pair.Device.EndpointOf("lock")
	if !ok {
		t.Fatal("the lock has no endpoint")
	}
	return &lockFixture{t: t, ctx: ctx, site: site, pair: pair, srv: srv, hw: hw, ep: ep}
}

// lockState is createLock's state (DoorLockServerTest.ts:64-90) on
// DoorLockServer.with("User", "PinCredential") (:62). The seeded user is
// created by FabricIndex.NO_FABRIC there, the fabric of MockServerNode's
// online actor (mock-server-node.ts:173-180); the port's requests come
// from the pair's fabric, so the user is created by that.
func lockState() lock.DoorLockConfig {
	locked := lockdef.LockStateLocked
	return lock.DoorLockConfig{
		Features:                            lock.FeatureUSR | lock.FeaturePIN,
		LockState:                           &locked,
		LockType:                            lockdef.LockTypeDeadBolt,
		OperatingMode:                       lockdef.OperatingModeNormal,
		WrongCodeEntryLimit:                 3,
		UserCodeTemporaryDisableTime:        10,
		NumberOfTotalUsersSupported:         10,
		NumberOfPinUsersSupported:           10,
		NumberOfCredentialsSupportedPerUser: 5,
		MinPinCodeLength:                    4,
		MaxPinCodeLength:                    8,
		Initial: store.DoorLockAuth{Users: []store.DoorLockUser{{
			UserIndex:               1,
			UserStatus:              uint8(lockdef.UserStatusOccupiedEnabled),
			UserType:                uint8(lockdef.UserTypeUnrestrictedUser),
			CredentialRule:          uint8(lockdef.CredentialRuleSingle),
			Credentials:             []store.DoorLockCredentialRef{},
			CreatorFabricIndex:      pairFabric,
			LastModifiedFabricIndex: pairFabric,
		}}},
	}
}

// createLock is DoorLockServerTest.ts:92-96.
func createLock(t *testing.T, edit func(*lock.DoorLockConfig)) *lockFixture {
	t.Helper()
	cfg := lockState()
	if edit != nil {
		edit(&cfg)
	}
	return newLockFixture(t, cfg)
}

// scheduledUser is DoorLockServerTest.ts:166-179.
func scheduledUser(userType lockdef.UserTypeEnum, expiresAt *int64) store.DoorLockUser {
	return store.DoorLockUser{
		UserIndex:               1,
		UserStatus:              uint8(lockdef.UserStatusOccupiedEnabled),
		UserType:                uint8(userType),
		CredentialRule:          uint8(lockdef.CredentialRuleSingle),
		Credentials:             []store.DoorLockCredentialRef{{Type: uint8(lockdef.CredentialTypePin), Index: 1}},
		CreatorFabricIndex:      pairFabric,
		LastModifiedFabricIndex: pairFabric,
		ExpiringUserExpiresAt:   expiresAt,
	}
}

// scheduledLockState is setUpScheduledLock's state
// (DoorLockServerTest.ts:185-212) on TestScheduledDoorLockServer (:107-115).
func scheduledLockState(users ...store.DoorLockUser) lock.DoorLockConfig {
	locked := lockdef.LockStateLocked
	return lock.DoorLockConfig{
		Features: lock.FeaturePIN | lock.FeatureUSR | lock.FeatureCOTA | lock.FeatureUBOLT |
			lock.FeatureWDSCH | lock.FeatureYDSCH | lock.FeatureHDSCH,
		LockState:                                &locked,
		LockType:                                 lockdef.LockTypeOther,
		OperatingMode:                            lockdef.OperatingModeNormal,
		WrongCodeEntryLimit:                      3,
		UserCodeTemporaryDisableTime:             10,
		NumberOfTotalUsersSupported:              10,
		NumberOfPinUsersSupported:                10,
		NumberOfWeekDaySchedulesSupportedPerUser: 10,
		NumberOfYearDaySchedulesSupportedPerUser: 10,
		NumberOfHolidaySchedulesSupported:        10,
		NumberOfCredentialsSupportedPerUser:      5,
		RequirePinForRemoteOperation:             false,
		MinPinCodeLength:                         4,
		MaxPinCodeLength:                         8,
		Initial: store.DoorLockAuth{
			Users: users,
			Credentials: []store.DoorLockCredential{{
				Type: uint8(lockdef.CredentialTypePin), Index: 1, Data: pin("1234"),
				CreatorFabricIndex: pairFabric, LastModifiedFabricIndex: pairFabric,
			}},
		},
	}
}

// setUpScheduledLock is DoorLockServerTest.ts:181-262.
func setUpScheduledLock(t *testing.T, edit func(*lock.DoorLockConfig), users ...store.DoorLockUser) *lockFixture {
	t.Helper()
	cfg := scheduledLockState(users...)
	if edit != nil {
		edit(&cfg)
	}
	return newLockFixture(t, cfg)
}

// invoke invokes a DoorLock command over the wire, timed when the command
// requires it.
func (f *lockFixture) invoke(cmd uint32, req spec.Encodable) (interaction.Result, error) {
	f.t.Helper()
	path := im.ConcreteCommandPath{Endpoint: f.ep, Cluster: lockdef.ClusterID, Command: cmd}
	if c := lockdef.Definition.Command(cmd, spec.Request); c != nil && c.Access.Timed {
		return fabrictest.InvokeTimed(f.ctx, f.pair, path, req)
	}
	return fabrictest.Invoke(f.ctx, f.pair, path, req)
}

// must invokes and fails the test on an error.
func (f *lockFixture) must(cmd uint32, req spec.Encodable) interaction.Result {
	f.t.Helper()
	res, err := f.invoke(cmd, req)
	if err != nil {
		f.t.Fatalf("command 0x%02X: %v", cmd, err)
	}
	return res
}

// refused invokes and fails the test unless the device refuses.
func (f *lockFixture) refused(cmd uint32, req spec.Encodable) *interaction.StatusError {
	f.t.Helper()
	_, err := f.invoke(cmd, req)
	var se *interaction.StatusError
	if !errors.As(err, &se) {
		f.t.Fatalf("command 0x%02X: %v, want a refusal", cmd, err)
	}
	return se
}

// response decodes a command's response fields.
func response[T any, P interface {
	*T
	DecodeTLV(spec.Node) error
}](t *testing.T, res interaction.Result) T {
	t.Helper()
	if !res.HasFields {
		t.Fatalf("response without fields (status 0x%02X)", res.Status)
	}
	v, err := spec.DecodeStruct[T, P](res.Fields)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

// setCredential is a SetCredential command and its response.
func (f *lockFixture) setCredential(req lockdef.SetCredentialRequest) lockdef.SetCredentialResponse {
	f.t.Helper()
	return response[lockdef.SetCredentialResponse](f.t, f.must(lockdef.CmdSetCredential, req))
}

// getUser is a GetUser command and its response.
func (f *lockFixture) getUser(userIndex uint16) lockdef.GetUserResponse {
	f.t.Helper()
	return response[lockdef.GetUserResponse](f.t, f.must(lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: userIndex}))
}

// unlockScheduled is DoorLockServerTest.ts:275-277.
func (f *lockFixture) unlockScheduled() error {
	f.t.Helper()
	_, err := f.invoke(lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{PinCode: pinPtr("1234")})
	return err
}

// unlock is UnlockDoor with the given PIN, none for "".
func (f *lockFixture) unlock(code string) error {
	f.t.Helper()
	req := lockdef.UnlockDoorRequest{}
	if code != "" {
		req.PinCode = pinPtr(code)
	}
	_, err := f.invoke(lockdef.CmdUnlockDoor, req)
	return err
}

// userStatus is userStatusOf (DoorLockServerTest.ts:266-268).
func (f *lockFixture) userStatus() lockdef.UserStatusEnum {
	u, ok := f.srv.User(1)
	if !ok {
		return lockdef.UserStatusAvailable
	}
	return lockdef.UserStatusEnum(u.UserStatus)
}

// lockStateNow reads LockState.
func (f *lockFixture) lockStateNow() any {
	v, _ := f.srv.MatterRead(lockdef.AttrLockState)
	return v
}

// lockEvents are the lock's events since the last mark, by kind.
type lockEvents struct {
	errors      []lockdef.LockOperationErrorEvent
	operations  []lockdef.LockOperationEvent
	userChanges []lockdef.LockUserChangeEvent
	alarms      []lockdef.AlarmCodeEnum
}

// events reads the lock's events newer than the mark from the device's
// event log; markEvents moves the mark (matter.js's `.length = 0`).
func (f *lockFixture) events() lockEvents {
	f.t.Helper()
	return f.eventsSince(f.mark)
}

// allEvents are the lock's events since the case started.
func (f *lockFixture) allEvents() lockEvents {
	f.t.Helper()
	return f.eventsSince(0)
}

func (f *lockFixture) eventsSince(mark uint64) lockEvents {
	f.t.Helper()
	got, err := fabrictest.Events(f.ctx, f.pair, f.eventPaths()...)
	if err != nil {
		f.t.Fatalf("read events: %v", err)
	}
	var out lockEvents
	for _, e := range got {
		if e.Number <= mark || !e.HasValue {
			continue
		}
		switch e.Path.Event {
		case lockdef.EventLockOperationError:
			out.errors = append(out.errors, decodeEvent[lockdef.LockOperationErrorEvent](f.t, e))
		case lockdef.EventLockOperation:
			out.operations = append(out.operations, decodeEvent[lockdef.LockOperationEvent](f.t, e))
		case lockdef.EventLockUserChange:
			out.userChanges = append(out.userChanges, decodeEvent[lockdef.LockUserChangeEvent](f.t, e))
		case lockdef.EventDoorLockAlarm:
			out.alarms = append(out.alarms, decodeEvent[lockdef.DoorLockAlarmEvent](f.t, e).AlarmCode)
		}
	}
	return out
}

// eventPaths are the lock's four events the cases observe.
func (f *lockFixture) eventPaths() []im.ConcreteEventPath {
	var paths []im.ConcreteEventPath
	for _, id := range []uint32{lockdef.EventDoorLockAlarm, lockdef.EventLockOperation, lockdef.EventLockOperationError, lockdef.EventLockUserChange} {
		paths = append(paths, im.ConcreteEventPath{
			Endpoint: f.ep, Cluster: lockdef.ClusterID, Event: id,
			HasEndpoint: true, HasCluster: true, HasEvent: true,
		})
	}
	return paths
}

// markEvents moves the mark past every event the device logged so far.
func (f *lockFixture) markEvents() {
	f.t.Helper()
	got, err := fabrictest.Events(f.ctx, f.pair, f.eventPaths()...)
	if err != nil {
		f.t.Fatalf("read events: %v", err)
	}
	for _, e := range got {
		f.mark = max(f.mark, e.Number)
	}
}

func decodeEvent[T any, P interface {
	*T
	DecodeTLV(spec.Node) error
}](t *testing.T, e interaction.EventData) T {
	t.Helper()
	v, err := spec.DecodeStruct[T, P](e.Value)
	if err != nil {
		t.Fatalf("decode event 0x%02X: %v", e.Path.Event, err)
	}
	return v
}

// operationErrors is operationErrorsOf (DoorLockServerTest.ts:280-282).
func (ev lockEvents) operationErrors() []lockdef.OperationErrorEnum {
	out := []lockdef.OperationErrorEnum{}
	for _, e := range ev.errors {
		out = append(out, e.OperationError)
	}
	return out
}

// expectScheduledDenial is DoorLockServerTest.ts:284-290: the unlock is
// refused and the one error event names the reason and user 1.
func (f *lockFixture) expectScheduledDenial(want lockdef.OperationErrorEnum) {
	f.t.Helper()
	f.markEvents()
	if err := f.unlockScheduled(); err == nil {
		f.t.Fatal("unlock was granted, want a denial")
	}
	ev := f.events()
	if len(ev.errors) != 1 || ev.errors[0].OperationError != want || ev.errors[0].UserIndex.Null || ev.errors[0].UserIndex.Value != 1 {
		f.t.Fatalf("error events %+v, want one %d for user 1", ev.errors, want)
	}
}

// modifyUser is DoorLockServerTest.ts:292-304.
func (f *lockFixture) modifyUser(userName *string, userStatus *lockdef.UserStatusEnum) {
	f.t.Helper()
	req := lockdef.SetUserRequest{
		OperationType:  lockdef.DataOperationTypeModify,
		UserIndex:      1,
		UserName:       spec.NullOf[string](),
		UserUniqueId:   spec.NullOf[uint32](),
		UserStatus:     spec.NullOf[lockdef.UserStatusEnum](),
		UserType:       spec.NullOf[lockdef.UserTypeEnum](),
		CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
	}
	if userName != nil {
		req.UserName = spec.ValueOf(*userName)
	}
	if userStatus != nil {
		req.UserStatus = spec.ValueOf(*userStatus)
	}
	f.must(lockdef.CmdSetUser, req)
}

// advance is MockTime.advance.
func (f *lockFixture) advance(d time.Duration) { f.site.Fake().Advance(d) }

// settle is settled(device).
func (f *lockFixture) settle() {
	f.t.Helper()
	if err := f.site.Settle(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

// stepWallClock is MockTime.stepWallClock: the fake's one reading moves,
// and its armed timers keep their deadlines.
func (f *lockFixture) stepWallClock(d time.Duration) {
	fake := f.site.Fake()
	fake.Set(fake.Now().Add(d))
}

// now is Time.nowMs as the lock reads it.
func (f *lockFixture) now() time.Time { return f.site.Clock().Now() }

// currentLocal is DoorLockServerTest.ts:156-164 before a site exists:
// every site's clock starts at fabrictest.DefaultStart, as every matter.js
// case starts at MockTime's.
func currentLocal() (day time.Weekday, hour int, epochS int64) {
	return localAt(fabrictest.DefaultStart)
}

// currentLocal is DoorLockServerTest.ts:156-164 on the fixture's clock.
func (f *lockFixture) currentLocal() (day time.Weekday, hour int, epochS int64) {
	return localAt(f.now())
}

func localAt(t time.Time) (day time.Weekday, hour int, epochS int64) {
	now := t.In(time.Local)
	return now.Weekday(), now.Hour(), lock.LocalInstantOf(now).EpochS
}

// credential names a credential.
func credential(t lockdef.CredentialTypeEnum, index uint16) lockdef.CredentialStruct {
	return lockdef.CredentialStruct{CredentialType: t, CredentialIndex: index}
}

// addPin is SetCredential Add of a PIN with the given user fields.
func addPin(index uint16, code string, userIndex spec.Nullable[uint16]) lockdef.SetCredentialRequest {
	return lockdef.SetCredentialRequest{
		OperationType:  lockdef.DataOperationTypeAdd,
		Credential:     credential(lockdef.CredentialTypePin, index),
		CredentialData: pin(code),
		UserIndex:      userIndex,
		UserStatus:     spec.NullOf[lockdef.UserStatusEnum](),
		UserType:       spec.NullOf[lockdef.UserTypeEnum](),
	}
}

// user1 and noUser are the UserIndex fields the cases name.
var (
	user1  = spec.ValueOf[uint16](1)
	noUser = spec.NullOf[uint16]()
)

// nextIndex is a response's NextCredentialIndex, nil for null or absent.
func nextIndex(n *spec.Nullable[uint16]) *uint16 {
	if n == nil || n.Null {
		return nil
	}
	v := n.Value
	return &v
}

// withPairFabric is a local request context on the pair's fabric.
func withPairFabric() context.Context {
	return im.WithFabricFilter(context.Background(), false, pairFabric)
}

// nullFabric and nullNode are the null FabricIndex and SourceNode of an
// event of no fabric.
func nullFabric() spec.Nullable[uint8] { return spec.NullOf[uint8]() }
func nullNode() spec.Nullable[uint64]  { return spec.NullOf[uint64]() }
