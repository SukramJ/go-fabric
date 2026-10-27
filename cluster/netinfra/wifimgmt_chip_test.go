// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/netinfra"
	wdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	"github.com/SukramJ/go-fabric/im"
)

// The chip cases of src/app/clusters/wifi-network-management-server/tests/
// TestWiFiNetworkManagementCluster.cpp at 6170af84, ported by meaning: the
// cluster is the server built by NewWiFiManagement, chip's ClusterTester
// reads become MatterRead, its dirty list the change reports, its subject
// descriptor the request context.

var (
	testSSID       = []byte("TestSSID")
	testPassphrase = []byte("password123")
)

func newWiFi(t *testing.T) *netinfra.WiFiManagementServer {
	t.Helper()
	s, err := netinfra.NewWiFiManagement(netinfra.WiFiManagementConfig{})
	mustT(t, err)
	return s
}

// readNullable reads attrID: nil for null.
func readNullable(t *testing.T, s *netinfra.WiFiManagementServer, attrID uint32) any {
	t.Helper()
	v, ok := s.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", attrID)
	}
	return v
}

// TestWiFiNetworkManagementCluster_AttributeListTest is :58-65.
func TestWiFiNetworkManagementCluster_AttributeListTest(t *testing.T) {
	t.Parallel()
	got := clusterAttributes(newWiFi(t))
	if want := []uint32{wdef.AttrSsid, wdef.AttrPassphraseSurrogate}; !slices.Equal(got, want) {
		t.Errorf("attributes %v, want %v", got, want)
	}
}

// TestWiFiNetworkManagementCluster_ReadGlobalAttributesTest is :67-76.
func TestWiFiNetworkManagementCluster_ReadGlobalAttributesTest(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	if rev, _ := s.MatterRead(0xFFFD); rev != wdef.Revision {
		t.Errorf("ClusterRevision %v, want %d", rev, wdef.Revision)
	}
	if fm, _ := s.MatterRead(0xFFFC); fm != uint32(0) {
		t.Errorf("FeatureMap %v, want 0", fm)
	}
	cmds := s.MatterAcceptedCommands()
	if !slices.Equal(cmds, []uint32{wdef.CmdNetworkPassphraseRequest}) {
		t.Errorf("AcceptedCommandList %v", cmds)
	}
	if gen := s.MatterGeneratedCommands(); !slices.Equal(gen, []uint32{wdef.CmdNetworkPassphraseResponse}) {
		t.Errorf("GeneratedCommandList %v", gen)
	}
}

// TestWiFiNetworkManagementCluster_InitialStateHasNoCredentials is :78-91.
func TestWiFiNetworkManagementCluster_InitialStateHasNoCredentials(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	if s.HasNetworkCredentials() || len(s.SSID()) != 0 || len(s.Passphrase()) != 0 {
		t.Error("a new server holds credentials")
	}
	if v := readNullable(t, s, wdef.AttrSsid); v != nil {
		t.Errorf("Ssid %v, want null", v)
	}
	if v := readNullable(t, s, wdef.AttrPassphraseSurrogate); v != nil {
		t.Errorf("PassphraseSurrogate %v, want null", v)
	}
}

// TestWiFiNetworkManagementCluster_SetNetworkCredentials is :93-120.
func TestWiFiNetworkManagementCluster_SetNetworkCredentials(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	dirty := watch(s)
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	if !s.HasNetworkCredentials() || !bytes.Equal(s.SSID(), testSSID) || !bytes.Equal(s.Passphrase(), testPassphrase) {
		t.Error("credentials not stored")
	}
	if v, _ := readNullable(t, s, wdef.AttrSsid).([]byte); !bytes.Equal(v, testSSID) {
		t.Errorf("Ssid %q, want %q", v, testSSID)
	}
	if v := readNullable(t, s, wdef.AttrPassphraseSurrogate); v == nil {
		t.Error("PassphraseSurrogate null with credentials")
	}
	if !dirty.has(wdef.AttrSsid) || !dirty.has(wdef.AttrPassphraseSurrogate) {
		t.Error("Ssid and PassphraseSurrogate not reported")
	}
	// The same credentials again report nothing.
	dirty.clear()
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	if !dirty.empty() {
		t.Error("the same credentials reported a change")
	}
}

// TestWiFiNetworkManagementCluster_ClearNetworkCredentials is :122-157.
func TestWiFiNetworkManagementCluster_ClearNetworkCredentials(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	dirty := watch(s)
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	if readNullable(t, s, wdef.AttrSsid) == nil {
		t.Fatal("Ssid null after SetNetworkCredentials")
	}
	dirty.clear()
	mustT(t, s.ClearNetworkCredentials())
	if s.HasNetworkCredentials() || len(s.SSID()) != 0 || len(s.Passphrase()) != 0 {
		t.Error("credentials survive ClearNetworkCredentials")
	}
	if v := readNullable(t, s, wdef.AttrSsid); v != nil {
		t.Errorf("Ssid %v, want null", v)
	}
	if v := readNullable(t, s, wdef.AttrPassphraseSurrogate); v != nil {
		t.Errorf("PassphraseSurrogate %v, want null", v)
	}
	if !dirty.has(wdef.AttrSsid) || !dirty.has(wdef.AttrPassphraseSurrogate) {
		t.Error("Ssid and PassphraseSurrogate not reported")
	}
	// Clearing again reports nothing.
	dirty.clear()
	mustT(t, s.ClearNetworkCredentials())
	if !dirty.empty() {
		t.Error("a second clear reported a change")
	}
}

// TestWiFiNetworkManagementCluster_SetNetworkCredentialsValidation is
// :159-189.
func TestWiFiNetworkManagementCluster_SetNetworkCredentialsValidation(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	ssid := []byte("Test")
	hexPSK := bytes.Repeat([]byte("0123456789abcdef"), 4)
	for _, c := range []struct {
		name             string
		ssid, passphrase []byte
		ok               bool
	}{
		{"SSID too short (empty)", nil, []byte("password"), false},
		{"SSID too long (> 32 bytes)", make([]byte, 33), []byte("password"), false},
		{"passphrase too short", ssid, []byte("short"), false},
		{"valid credentials", ssid, []byte("validpass"), true},
		{"valid 64-byte hex PSK", ssid, hexPSK, true},
		{"64-byte non-hex PSK", ssid, bytes.Repeat([]byte("G"), 64), false},
	} {
		err := s.SetNetworkCredentials(c.ssid, c.passphrase)
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, netinfra.ErrInvalidCredentials) {
			t.Errorf("%s: %v, want ErrInvalidCredentials", c.name, err)
		}
		if err != nil && bytes.Contains([]byte(err.Error()), c.passphrase) && len(c.passphrase) > 0 {
			t.Errorf("%s: the error carries the passphrase", c.name)
		}
	}
}

// TestWiFiNetworkManagementCluster_PassphraseSurrogateChangesOnPassphraseChange
// is :191-211, with the real-time rule of WiFiNetworkManagementCluster.cpp:
// 94-99: one past the last surrogate, or the real time in milliseconds when
// that is later.
func TestWiFiNetworkManagementCluster_PassphraseSurrogateChangesOnPassphraseChange(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	mustT(t, s.SetNetworkCredentials(testSSID, []byte("password1")))
	s1 := readNullable(t, s, wdef.AttrPassphraseSurrogate)
	mustT(t, s.SetNetworkCredentials(testSSID, []byte("password2")))
	s2 := readNullable(t, s, wdef.AttrPassphraseSurrogate)
	if s1 == nil || s2 == nil || s1 == s2 {
		t.Errorf("surrogates %v and %v, want two different values", s1, s2)
	}

	clk := clocktest.New(time.UnixMilli(5000))
	c, err := netinfra.NewWiFiManagement(netinfra.WiFiManagementConfig{Clock: clk})
	mustT(t, err)
	mustT(t, c.SetNetworkCredentials(testSSID, []byte("password1")))
	if v := readNullable(t, c, wdef.AttrPassphraseSurrogate); v != uint64(5000) {
		t.Errorf("surrogate %v, want the real time 5000", v)
	}
	mustT(t, c.SetNetworkCredentials(testSSID, []byte("password2")))
	if v := readNullable(t, c, wdef.AttrPassphraseSurrogate); v != uint64(5001) {
		t.Errorf("surrogate %v at the same time, want 5001", v)
	}
	// An SSID change alone leaves the surrogate.
	mustT(t, c.SetNetworkCredentials([]byte("Other"), []byte("password2")))
	if v := readNullable(t, c, wdef.AttrPassphraseSurrogate); v != uint64(5001) {
		t.Errorf("surrogate %v after an SSID change, want 5001", v)
	}
}

// TestWiFiNetworkManagementCluster_NetworkPassphraseRequestRequiresCaseSession
// is :213-228; a group message is not CASE either.
func TestWiFiNetworkManagementCluster_NetworkPassphraseRequestRequiresCaseSession(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	if _, err := s.MatterInvoke(paseCtx(), wdef.CmdNetworkPassphraseRequest, wdef.NetworkPassphraseRequestRequest{}); statusOf(err) != im.StatusUnsupportedAccess {
		t.Errorf("PASE: %v, want UNSUPPORTED_ACCESS", err)
	}
	if _, err := s.MatterInvoke(groupCtx(), wdef.CmdNetworkPassphraseRequest, wdef.NetworkPassphraseRequestRequest{}); statusOf(err) != im.StatusUnsupportedAccess {
		t.Errorf("group: %v, want UNSUPPORTED_ACCESS", err)
	}
}

// TestWiFiNetworkManagementCluster_NetworkPassphraseRequestRequiresCredentials
// is :230-241.
func TestWiFiNetworkManagementCluster_NetworkPassphraseRequestRequiresCredentials(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	if _, err := s.MatterInvoke(caseCtx(), wdef.CmdNetworkPassphraseRequest, wdef.NetworkPassphraseRequestRequest{}); statusOf(err) != im.StatusInvalidInState {
		t.Errorf("%v, want INVALID_IN_STATE", err)
	}
}

// TestWiFiNetworkManagementCluster_NetworkPassphraseRequestSuccess is
// :243-261.
func TestWiFiNetworkManagementCluster_NetworkPassphraseRequestSuccess(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	mustT(t, s.SetNetworkCredentials(testSSID, testPassphrase))
	resp, err := s.MatterInvoke(caseCtx(), wdef.CmdNetworkPassphraseRequest, wdef.NetworkPassphraseRequestRequest{})
	mustT(t, err)
	r, ok := resp.(wdef.NetworkPassphraseResponse)
	if !ok || !bytes.Equal(r.Passphrase, testPassphrase) {
		t.Errorf("response %#v, want the passphrase", resp)
	}
}

// TestWiFiManagementCredentialsAreNotHeldAttributes: the typed Set cannot
// move Ssid or PassphraseSurrogate past the credentials.
func TestWiFiManagementCredentialsAreNotHeldAttributes(t *testing.T) {
	t.Parallel()
	s := newWiFi(t)
	if err := s.SetAttributes(map[uint32]any{wdef.AttrSsid: []byte("x")}); err == nil {
		t.Error("SetAttributes moved Ssid")
	}
	mustT(t, s.SetAttributes(map[uint32]any{wdef.AttrSsid: nil}))
}
