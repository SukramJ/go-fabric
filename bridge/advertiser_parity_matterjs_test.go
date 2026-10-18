// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A port of matter.js packages/protocol/test/protocol/DeviceAdvertiserTest.ts
// at 862ad75f: the TCP T key and the ICD fields of the operational
// advertisement, and its refresh.
//
// The Go surface differs: DeviceAdvertiser's operational advertisement
// is Bridge.AnnounceFabric, supportedTransports is Config.TCP
// ({tcpClient: Outgoing, tcpServer: Incoming}), a refresh is a repeated
// AnnounceFabric, and the MockAdvertiser is a recording mdns.Advertiser.

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// deviceAdvertiserRecorder records every Publish, as MockAdvertiser
// records advertise() calls.
type deviceAdvertiserRecorder struct {
	*fakeAdvertiser
	mu    sync.Mutex
	calls []mdns.Service
}

func (r *deviceAdvertiserRecorder) Publish(ctx context.Context, svc mdns.Service) error {
	r.mu.Lock()
	r.calls = append(r.calls, svc)
	r.mu.Unlock()
	return r.fakeAdvertiser.Publish(ctx, svc)
}

// operational is the recorded operational advertisements.
func (r *deviceAdvertiserRecorder) operational() []mdns.Service {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []mdns.Service
	for _, svc := range r.calls {
		if svc.ServiceType == mdns.ServiceTypeOperational {
			out = append(out, svc)
		}
	}
	return out
}

func deviceAdvertiserBridge(t *testing.T, tcp TCPConfig) (*Bridge, *deviceAdvertiserRecorder) {
	t.Helper()
	rec := &deviceAdvertiserRecorder{fakeAdvertiser: newFakeAdvertiser()}
	b, err := New(wbEmptySnapshotter, rec, Config{
		Listen: ":0", VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "Test Device", TCP: tcp,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b, rec
}

// deviceAdvertiserFabric announces fabric index 1's node 1, as the
// tests' fabrics.events.added emit does.
func deviceAdvertiserFabric(t *testing.T, b *Bridge, rec *deviceAdvertiserRecorder) mdns.Service {
	t.Helper()
	b.AnnounceFabric(context.Background(), [8]byte{0, 0, 0, 0, 0, 0, 0, 1}, 1)
	ops := rec.operational()
	if len(ops) == 0 {
		t.Fatal("no operational advertisement")
	}
	return ops[0]
}

func deviceAdvertiserTXT(svc mdns.Service, key string) (string, bool) {
	for _, r := range svc.TXT {
		if r.Key == key {
			return r.Value, true
		}
	}
	return "", false
}

func TestDeviceAdvertiserTcpTKey(t *testing.T) {
	t.Run("includes tcp bitmap in operational advertisement when tcp is set", func(t *testing.T) {
		b, rec := deviceAdvertiserBridge(t, TCPConfig{Incoming: true, Outgoing: true})
		// {tcpClient: true, tcpServer: true}: bits 1 and 2.
		if v, ok := deviceAdvertiserTXT(deviceAdvertiserFabric(t, b, rec), "T"); !ok || v != "6" {
			t.Fatalf("T = %q (present %v), want 6", v, ok)
		}
	})
	t.Run("does not include tcp in operational advertisement when tcp is not set", func(t *testing.T) {
		b, rec := deviceAdvertiserBridge(t, TCPConfig{})
		if v, ok := deviceAdvertiserTXT(deviceAdvertiserFabric(t, b, rec), "T"); ok {
			t.Fatalf("T = %q present, want absent", v)
		}
	})
	t.Run("supportedTransports setter updates context for subsequent advertisements", func(t *testing.T) {
		// Go fixes the transports at New; the advertisement built later
		// carries them, here {tcpClient: false, tcpServer: true}.
		b, rec := deviceAdvertiserBridge(t, TCPConfig{Incoming: true})
		if v, ok := deviceAdvertiserTXT(deviceAdvertiserFabric(t, b, rec), "T"); !ok || v != "4" {
			t.Fatalf("T = %q (present %v), want 4", v, ok)
		}
	})
}

func TestDeviceAdvertiserIcdAdvertisementProvider(t *testing.T) {
	t.Run("does not include ICD fields when no provider is registered", func(t *testing.T) {
		b, rec := deviceAdvertiserBridge(t, TCPConfig{})
		if v, ok := deviceAdvertiserTXT(deviceAdvertiserFabric(t, b, rec), "ICD"); ok {
			t.Fatalf("ICD = %q present, want absent", v)
		}
	})
	t.Run("refreshOperationalAdvertisement re-advertises the fabric when in operational mode", func(t *testing.T) {
		b, rec := deviceAdvertiserBridge(t, TCPConfig{})
		first := deviceAdvertiserFabric(t, b, rec)
		before := len(rec.operational())
		b.AnnounceFabric(context.Background(), [8]byte{0, 0, 0, 0, 0, 0, 0, 1}, 1)
		ops := rec.operational()
		if len(ops) <= before || ops[len(ops)-1].InstanceName != first.InstanceName {
			t.Fatalf("operational advertisements %d after the refresh, want more than %d for %s", len(ops), before, first.InstanceName)
		}
	})
}
