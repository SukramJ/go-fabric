// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Store persists a TLS server's tables: one JSON setting per server and
// device, under the device's StableKey — the shape of the DoorLock
// server's credential key (store.SettingDoorLockCredentialKey). Every
// store.Port is one (store.Settings); nil keeps the tables in memory.
// chip keeps the same tables in its persistent storage
// (tls-certificate-management-server/CertificateTableImpl.cpp,
// examples/all-clusters-app tls-client-management-instance.cpp).
type Store interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
}

// The setting keys, each followed by the device's StableKey.
const (
	settingCertificates = "tls.certificates:"
	settingEndpoints    = "tls.endpoints:"
)

// ErrStore reports a table the store could not load or save.
var ErrStore = errors.New("tls: store")

// certificateRecord is what CertificateManagement persists.
type certificateRecord struct {
	Roots      []rootEntry   `json:"roots"`
	Clients    []clientEntry `json:"clients"`
	NextRoot   uint16        `json:"nextRoot"`
	NextClient uint16        `json:"nextClient"`
}

// rootEntry is a provisioned root certificate.
type rootEntry struct {
	Fabric      uint8  `json:"fabric"`
	CAID        uint16 `json:"caid"`
	Certificate []byte `json:"certificate"`
}

// clientEntry is a client certificate: its key exists from ClientCSR on,
// Certificate is nil until ProvisionClientCertificate.
type clientEntry struct {
	Fabric        uint8    `json:"fabric"`
	CCDID         uint16   `json:"ccdid"`
	Certificate   []byte   `json:"certificate,omitempty"`
	Intermediates [][]byte `json:"intermediates,omitempty"`
}

// endpointRecord is what ClientManagement persists.
type endpointRecord struct {
	Endpoints []endpointEntry `json:"endpoints"`
	Next      uint16          `json:"next"`
}

// endpointEntry is a provisioned TLS endpoint.
type endpointEntry struct {
	Fabric         uint8   `json:"fabric"`
	EndpointID     uint16  `json:"endpointId"`
	Hostname       []byte  `json:"hostname"`
	Port           uint16  `json:"port"`
	CAID           uint16  `json:"caid"`
	CCDID          *uint16 `json:"ccdid,omitempty"`
	ReferenceCount uint8   `json:"referenceCount"`
}

// load reads the record under key into v; v stays as it is when none was
// saved.
func load(ctx context.Context, s Store, key string, v any) error {
	if s == nil {
		return nil
	}
	raw, ok, err := s.GetSetting(ctx, key)
	if err != nil {
		return fmt.Errorf("%w: load %s: %w", ErrStore, key, err)
	}
	if !ok {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return fmt.Errorf("%w: decode %s: %w", ErrStore, key, err)
	}
	return nil
}

// save writes v under key.
func save(ctx context.Context, s Store, key string, v any) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%w: encode %s: %w", ErrStore, key, err)
	}
	if err := s.SetSetting(ctx, key, string(raw)); err != nil {
		return fmt.Errorf("%w: save %s: %w", ErrStore, key, err)
	}
	return nil
}
