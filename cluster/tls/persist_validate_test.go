// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/tls"
)

// TestLoadRefusesARecordTheServerCannotWrite (R2-P6-store-1119 a): a
// stored table with an id two entries hold, an id at the bound or an
// allocator past it is ErrStore, not a server whose allocator hands out
// an id above the bound or never returns.
func TestLoadRefusesARecordTheServerCannotWrite(t *testing.T) {
	keys := &mockKeys{invalid: map[string]bool{}}
	for name, raw := range map[string]string{
		"duplicate CAID":    `{"roots":[{"fabric":1,"caid":3,"certificate":"AA=="},{"fabric":2,"caid":3,"certificate":"AQ=="}]}`,
		"CAID at the bound": `{"roots":[{"fabric":1,"caid":65534,"certificate":"AA=="}]}`,
		"duplicate CCDID":   `{"clients":[{"fabric":1,"ccdid":7},{"fabric":1,"ccdid":7}]}`,
		"allocator past":    `{"nextRoot":65535}`,
		"client allocator":  `{"nextClient":65535}`,
	} {
		st := &mapStore{m: map[string]string{"tls.certificates:k": raw}}
		_, err := tls.NewCertificateManagement(context.Background(), tls.CertificateManagementConfig{
			MaxRootCertificates: 5, MaxClientCertificates: 5, Port: keys, Store: st, Key: "k",
		})
		if !errors.Is(err, tls.ErrStore) {
			t.Errorf("%s: %v, want ErrStore", name, err)
		}
	}
	for name, raw := range map[string]string{
		"duplicate endpoint id": `{"endpoints":[{"fabric":1,"endpointId":2},{"fabric":2,"endpointId":2}]}`,
		"endpoint id 0xFFFF":    `{"endpoints":[{"fabric":1,"endpointId":65535}]}`,
	} {
		st := &mapStore{m: map[string]string{"tls.endpoints:k": raw}}
		_, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5, Store: st, Key: "k"})
		if !errors.Is(err, tls.ErrStore) {
			t.Errorf("%s: %v, want ErrStore", name, err)
		}
	}
}
