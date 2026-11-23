// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// This file keeps the names that moved to thingfabric.dev/go-fabric/dcl
// (the ledger client, its OTA answers, the record types, the trust-store
// store and the vendor information service) working at this path for the
// deprecation window (README.md §The deprecation window): every type is an
// alias, every constant and variable the same value, every function a
// delegation, so the behaviour is the moved package's, unchanged.

import (
	"context"
	"io"
	"log/slog"

	"thingfabric.dev/go-fabric/bdx/otaimage"
	"thingfabric.dev/go-fabric/dcl"
)

// Client is [dcl.Client].
//
// Deprecated: use [dcl.Client]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Client = dcl.Client

// Error is [dcl.Error].
//
// Deprecated: use [dcl.Error]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Error = dcl.Error

// ResponseError is [dcl.ResponseError].
//
// Deprecated: use [dcl.ResponseError]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type ResponseError = dcl.ResponseError

// OTAError is [dcl.OTAError].
//
// Deprecated: use [dcl.OTAError]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type OTAError = dcl.OTAError

// OTAUpdate is [dcl.OTAUpdate].
//
// Deprecated: use [dcl.OTAUpdate]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type OTAUpdate = dcl.OTAUpdate

// OTAVersions is [dcl.OTAVersions].
//
// Deprecated: use [dcl.OTAVersions]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type OTAVersions = dcl.OTAVersions

// SubjectReference is [dcl.SubjectReference].
//
// Deprecated: use [dcl.SubjectReference]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type SubjectReference = dcl.SubjectReference

// Certificate is [dcl.Certificate].
//
// Deprecated: use [dcl.Certificate]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Certificate = dcl.Certificate

// DeviceModel is [dcl.DeviceModel].
//
// Deprecated: use [dcl.DeviceModel]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type DeviceModel = dcl.DeviceModel

// SoftwareVersion is [dcl.SoftwareVersion].
//
// Deprecated: use [dcl.SoftwareVersion]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type SoftwareVersion = dcl.SoftwareVersion

// Vendor is [dcl.Vendor].
//
// Deprecated: use [dcl.Vendor]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Vendor = dcl.Vendor

// ComplianceInfo is [dcl.ComplianceInfo].
//
// Deprecated: use [dcl.ComplianceInfo]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type ComplianceInfo = dcl.ComplianceInfo

// ComplianceHistoryItem is [dcl.ComplianceHistoryItem].
//
// Deprecated: use [dcl.ComplianceHistoryItem]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type ComplianceHistoryItem = dcl.ComplianceHistoryItem

// RevocationPoint is [dcl.RevocationPoint].
//
// Deprecated: use [dcl.RevocationPoint]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type RevocationPoint = dcl.RevocationPoint

// Store is [dcl.Store].
//
// Deprecated: use [dcl.Store]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Store = dcl.Store

// MemoryStore is [dcl.MemoryStore].
//
// Deprecated: use [dcl.MemoryStore]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type MemoryStore = dcl.MemoryStore

// FileStore is [dcl.FileStore].
//
// Deprecated: use [dcl.FileStore]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type FileStore = dcl.FileStore

// Settings is [dcl.Settings].
//
// Deprecated: use [dcl.Settings]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type Settings = dcl.Settings

// SettingStore is [dcl.SettingStore].
//
// Deprecated: use [dcl.SettingStore]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type SettingStore = dcl.SettingStore

// VendorInfo is [dcl.VendorInfo].
//
// Deprecated: use [dcl.VendorInfo]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type VendorInfo = dcl.VendorInfo

// ProductInfo is [dcl.ProductInfo].
//
// Deprecated: use [dcl.ProductInfo]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type ProductInfo = dcl.ProductInfo

// VendorSeed is [dcl.VendorSeed].
//
// Deprecated: use [dcl.VendorSeed]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type VendorSeed = dcl.VendorSeed

// VendorInfoOptions is [dcl.VendorInfoOptions].
//
// Deprecated: use [dcl.VendorInfoOptions]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type VendorInfoOptions = dcl.VendorInfoOptions

// VendorUpdate is [dcl.VendorUpdate].
//
// Deprecated: use [dcl.VendorUpdate]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type VendorUpdate = dcl.VendorUpdate

// VendorInfoService is [dcl.VendorInfoService].
//
// Deprecated: use [dcl.VendorInfoService]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
type VendorInfoService = dcl.VendorInfoService

// ProductionURL is [dcl.ProductionURL].
//
// Deprecated: use [dcl.ProductionURL]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const ProductionURL = dcl.ProductionURL

// TestNetURL is [dcl.TestNetURL].
//
// Deprecated: use [dcl.TestNetURL]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const TestNetURL = dcl.TestNetURL

// DefaultTimeout is [dcl.DefaultTimeout].
//
// Deprecated: use [dcl.DefaultTimeout]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const DefaultTimeout = dcl.DefaultTimeout

// CodeNotFound is [dcl.CodeNotFound].
//
// Deprecated: use [dcl.CodeNotFound]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const CodeNotFound = dcl.CodeNotFound

// DefaultDownloadTimeout is [dcl.DefaultDownloadTimeout].
//
// Deprecated: use [dcl.DefaultDownloadTimeout]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const DefaultDownloadTimeout = dcl.DefaultDownloadTimeout

// DefaultMaxImageSize is [dcl.DefaultMaxImageSize].
//
// Deprecated: use [dcl.DefaultMaxImageSize]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const DefaultMaxImageSize = dcl.DefaultMaxImageSize

// SourceProduction is [dcl.SourceProduction].
//
// Deprecated: use [dcl.SourceProduction]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const SourceProduction = dcl.SourceProduction

// SourceTest is [dcl.SourceTest].
//
// Deprecated: use [dcl.SourceTest]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const SourceTest = dcl.SourceTest

// RevocationTypeCRL is [dcl.RevocationTypeCRL].
//
// Deprecated: use [dcl.RevocationTypeCRL]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const RevocationTypeCRL = dcl.RevocationTypeCRL

// DefaultVendorUpdateInterval is [dcl.DefaultVendorUpdateInterval].
//
// Deprecated: use [dcl.DefaultVendorUpdateInterval]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
const DefaultVendorUpdateInterval = dcl.DefaultVendorUpdateInterval

// ErrOTAImageTooLarge is [dcl.ErrOTAImageTooLarge], the same error value.
//
// Deprecated: use [dcl.ErrOTAImageTooLarge]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
var ErrOTAImageTooLarge = dcl.ErrOTAImageTooLarge

// IsNotFound is [dcl.IsNotFound].
//
// Deprecated: use [dcl.IsNotFound]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
func IsNotFound(err error) bool { return dcl.IsNotFound(err) }

// NewClient is [dcl.NewClient].
//
// Deprecated: use [dcl.NewClient]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
func NewClient(baseURL string) *Client { return dcl.NewClient(baseURL) }

// VerifyImage is [dcl.VerifyImage].
//
// Deprecated: use [dcl.VerifyImage]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
func VerifyImage(r io.Reader, u SoftwareVersion, log *slog.Logger) (otaimage.Header, error) {
	return dcl.VerifyImage(r, u, log)
}

// NewVendorInfoService is [dcl.NewVendorInfoService].
//
// Deprecated: use [dcl.NewVendorInfoService]; the ledger client moved to the role-neutral
// package thingfabric.dev/go-fabric/dcl (review II, decision 553), and
// this name forwards to it unchanged. Removable in v0.28.0.
func NewVendorInfoService(ctx context.Context, opts VendorInfoOptions) (*VendorInfoService, error) {
	return dcl.NewVendorInfoService(ctx, opts)
}
