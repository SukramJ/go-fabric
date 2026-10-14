// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	observerdef "github.com/SukramJ/go-fabric/cluster/spec/contentappobserver"
	keypaddef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
)

type keypad struct {
	keys []keypaddef.CecKeyCodeEnum
}

func (k *keypad) SendKey(_ context.Context, req keypaddef.SendKeyRequest) (keypaddef.SendKeyResponse, error) {
	k.keys = append(k.keys, req.KeyCode)
	if req.KeyCode > keypaddef.CecKeyCodeNumbers9 {
		return keypaddef.SendKeyResponse{Status: keypaddef.StatusUnsupportedKey}, nil
	}
	return keypaddef.SendKeyResponse{Status: keypaddef.StatusSuccess}, nil
}

// TestKeypadSendKey: SendKey reaches the port with the key code and its
// SendKeyResponse goes out as the port built it.
func TestKeypadSendKey(t *testing.T) {
	t.Parallel()
	port := &keypad{}
	srv, err := media.NewKeypad(media.KeypadConfig{Port: port, Features: keypaddef.FeatureNumberKeys | keypaddef.FeatureLocationKeys})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := invoke(t, srv, keypaddef.Definition, keypaddef.CmdSendKey, keypaddef.SendKeyRequest{KeyCode: keypaddef.CecKeyCodeNumbers1})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, keypaddef.Definition, keypaddef.CmdSendKeyResponse, resp.(keypaddef.SendKeyResponse), keypaddef.SendKeyResponse{Status: keypaddef.StatusSuccess})
	resp, err = invoke(t, srv, keypaddef.Definition, keypaddef.CmdSendKey, keypaddef.SendKeyRequest{KeyCode: keypaddef.CecKeyCodePlay})
	if err != nil || resp.(keypaddef.SendKeyResponse).Status != keypaddef.StatusUnsupportedKey {
		t.Fatalf("an unsupported key: %v %v", resp, err)
	}
	if !slices.Equal(port.keys, []keypaddef.CecKeyCodeEnum{keypaddef.CecKeyCodeNumbers1, keypaddef.CecKeyCodePlay}) {
		t.Errorf("keys %v", port.keys)
	}
	if v := read(t, srv, 0xFFFC); v != uint32(keypaddef.FeatureNumberKeys|keypaddef.FeatureLocationKeys) {
		t.Errorf("FeatureMap %v", v)
	}
	if _, err := media.NewKeypad(media.KeypadConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

type sleeper struct {
	slept int
	err   error
}

func (s *sleeper) Sleep(context.Context) error {
	s.slept++
	return s.err
}

// TestLowPowerSleep: Sleep calls the port — matter.js LowPowerServer.ts:
// 16-18 emits enterLowPowerMode — and answers its error as the status.
func TestLowPowerSleep(t *testing.T) {
	t.Parallel()
	port := &sleeper{}
	srv, err := media.NewLowPower(media.LowPowerConfig{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := invoke(t, srv, lowpowerdef.Definition, lowpowerdef.CmdSleep, lowpowerdef.SleepRequest{}); err != nil || resp != nil {
		t.Fatalf("Sleep: %v %v", resp, err)
	}
	if port.slept != 1 {
		t.Errorf("Sleep reached the port %d times", port.slept)
	}
	// A command without a payload arrives without fields.
	if _, err := srv.MatterInvoke(context.Background(), lowpowerdef.CmdSleep, nil); err != nil {
		t.Fatal(err)
	}
	port.err = errPort
	_, err = invoke(t, srv, lowpowerdef.Definition, lowpowerdef.CmdSleep, lowpowerdef.SleepRequest{})
	wantStatus(t, err, im.StatusFailure)
	// Fields of another type are INVALID_COMMAND.
	_, err = srv.MatterInvoke(context.Background(), lowpowerdef.CmdSleep, "garbage")
	wantStatus(t, err, im.StatusInvalidCommand)
	if _, err := media.NewLowPower(media.LowPowerConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

type observer struct{}

func (observer) ContentAppMessage(_ context.Context, req observerdef.ContentAppMessageRequest) (observerdef.ContentAppMessageResponse, error) {
	return observerdef.ContentAppMessageResponse{Status: observerdef.StatusSuccess, Data: &req.Data, EncodingHint: req.EncodingHint}, nil
}

// TestContentAppObserverMessage: the message reaches the port and its
// response goes out, as the APPOBSERVER-13.2 step expects it echoed.
func TestContentAppObserverMessage(t *testing.T) {
	t.Parallel()
	srv, err := media.NewContentAppObserver(media.ContentAppObserverConfig{Port: observer{}})
	if err != nil {
		t.Fatal(err)
	}
	req := observerdef.ContentAppMessageRequest{Data: "exampleData", EncodingHint: spectest.Ptr("Zoo")}
	resp, err := invoke(t, srv, observerdef.Definition, observerdef.CmdContentAppMessage, req)
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, observerdef.Definition, observerdef.CmdContentAppMessageResponse, resp.(observerdef.ContentAppMessageResponse),
		observerdef.ContentAppMessageResponse{Status: observerdef.StatusSuccess, Data: spectest.Ptr("exampleData"), EncodingHint: spectest.Ptr("Zoo")})
	if _, err := media.NewContentAppObserver(media.ContentAppObserverConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

type login struct {
	pin    string
	logins []logindef.LoginRequest
	outs   int
}

func (l *login) GetSetupPIN(_ context.Context, _ logindef.GetSetupPinRequest) (logindef.GetSetupPinResponse, error) {
	return logindef.GetSetupPinResponse{SetupPin: l.pin}, nil
}

func (l *login) Login(_ context.Context, req logindef.LoginRequest) error {
	l.logins = append(l.logins, req)
	if req.SetupPin != l.pin {
		return media.StatusError{Status: im.StatusUnsupportedAccess}
	}
	return nil
}

func (l *login) Logout(context.Context, logindef.LogoutRequest) error {
	l.outs++
	return nil
}

// TestAccountLogin: the three commands reach the port; they are timed and
// Administer / Operate, as account-login.element.ts:28, :40, :47 give
// them ("F A T", "F O T").
func TestAccountLogin(t *testing.T) {
	t.Parallel()
	port := &login{pin: "20202021"}
	srv, err := media.NewAccountLogin(media.AccountLoginConfig{Port: port, Events: []uint32{logindef.EventLoggedOut}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := invoke(t, srv, logindef.Definition, logindef.CmdGetSetupPin, logindef.GetSetupPinRequest{TempAccountIdentifier: "1234efgh5678abcd"})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, logindef.Definition, logindef.CmdGetSetupPinResponse, resp.(logindef.GetSetupPinResponse), logindef.GetSetupPinResponse{SetupPin: "20202021"})
	node := uint64(0x12344321)
	if _, err := invoke(t, srv, logindef.Definition, logindef.CmdLogin, logindef.LoginRequest{TempAccountIdentifier: "1234efgh5678abcd", SetupPin: "20202021", Node: &node}); err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t, srv, logindef.Definition, logindef.CmdLogin, logindef.LoginRequest{TempAccountIdentifier: "1234efgh5678abcd", SetupPin: "00000000"})
	wantStatus(t, err, im.StatusUnsupportedAccess)
	if _, err := invoke(t, srv, logindef.Definition, logindef.CmdLogout, logindef.LogoutRequest{Node: &node}); err != nil {
		t.Fatal(err)
	}
	if len(port.logins) != 2 || *port.logins[0].Node != node || port.outs != 1 {
		t.Errorf("logins %+v, logouts %d", port.logins, port.outs)
	}
	for _, cmd := range []uint32{logindef.CmdGetSetupPin, logindef.CmdLogin, logindef.CmdLogout} {
		if !srv.IsTimedCommand(cmd) {
			t.Errorf("command 0x%02X is not timed", cmd)
		}
	}
	if srv.MinInvokePrivilege(logindef.CmdGetSetupPin) != 5 || srv.MinInvokePrivilege(logindef.CmdLogout) != 3 {
		t.Errorf("privileges %d %d", srv.MinInvokePrivilege(logindef.CmdGetSetupPin), srv.MinInvokePrivilege(logindef.CmdLogout))
	}
	ev := &emitted{}
	srv.SetMatterEventEmitter(ev)
	if err := srv.EmitLoggedOut(logindef.LoggedOutEvent{Node: &node}); err != nil {
		t.Fatal(err)
	}
	if len(ev.events) != 1 || ev.events[0].event != logindef.EventLoggedOut {
		t.Errorf("events %+v", ev.events)
	}
	if _, err := media.NewAccountLogin(media.AccountLoginConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// TestStatusError: the error type's text, cause and status.
func TestStatusError(t *testing.T) {
	t.Parallel()
	e := media.StatusError{Status: im.StatusBusy, Err: errPort}
	if !errors.Is(e, errPort) || e.MatterStatusCode() != im.StatusBusy || e.Error() == "" {
		t.Errorf("%v", e)
	}
	if (media.StatusError{Status: im.StatusBusy}).Error() == "" {
		t.Error("empty text")
	}
}
