// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
)

// TestTypedNilDeclarationIsRefused (R2-P6-err-2507): a typed nil
// *<cluster>def.Declare is a declaration of nothing, refused with
// ErrConfiguration — never a panic of New.
func TestTypedNilDeclarationIsRefused(t *testing.T) {
	t.Parallel()
	for name, decl := range map[string]Declaration{
		"BridgedDeviceBasicInformation": (*bdbidef.Declare)(nil),
		"Identify":                      (*identifydef.Declare)(nil),
		"OnOff":                         (*onoffdef.Declare)(nil),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: New panicked on a typed nil declaration: %v", name, r)
				}
			}()
			if _, err := NewOnOffLight(testOptions("nil-"+name), decl); !errors.Is(err, ErrConfiguration) {
				t.Errorf("%s: New with a typed nil declaration: %v, want ErrConfiguration", name, err)
			}
		}()
	}
}

// TestTypedHostServerOfAnotherClusterIsRefused (R2-P6-err-2505): a typed
// Declare's HostServer serves the declared cluster — one of another
// cluster would be mounted under the declared ID and leave the declared,
// mandatory cluster unserved.
func TestTypedHostServerOfAnotherClusterIsRefused(t *testing.T) {
	t.Parallel()
	_, err := NewOnOffLight(testOptions("other"), onoffdef.Declare{HostServer: core.NewBinding()})
	if !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), "0x001E") {
		t.Fatalf("OnOff declared with a Binding server: %v, want ErrConfiguration naming 0x001E", err)
	}
}

// TestTypedHostServerWithAnIllegalIDIsRefused (R2-P6-err-2505): the ID
// check a HostServer of a cluster without a generated package gets runs on
// a typed Declare's HostServer too.
func TestTypedHostServerWithAnIllegalIDIsRefused(t *testing.T) {
	t.Parallel()
	srv := &attrListingServer{hostServer: hostServer{id: onoffdef.ClusterID}, attrs: []uint32{0x0000, 0xFFFF_FFFF}}
	if _, err := NewOnOffLight(testOptions("illegal"), onoffdef.Declare{HostServer: srv}); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("an OnOff HostServer listing attribute 0xFFFFFFFF: %v, want ErrConfiguration", err)
	}
}

// TestSecondUndeclaredHostServerIsRefused (R2-P6-err-2508): a cluster
// without a generated package is declared once — a second HostServer for
// it is refused, not mounted over the first.
func TestSecondUndeclaredHostServerIsRefused(t *testing.T) {
	t.Parallel()
	a, b := &hostServer{id: 0xFFF1_FC01}, &hostServer{id: 0xFFF1_FC01}
	_, err := NewOnOffLight(testOptions("twice"), HostServer(a), HostServer(b))
	if !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("two HostServer calls for 0xFFF1FC01: %v, want ErrConfiguration, declared twice", err)
	}
}

// attrListingServer is a hostServer that lists its attributes.
type attrListingServer struct {
	hostServer
	attrs []uint32
}

func (l *attrListingServer) MatterAttributes() []uint32 { return l.attrs }
