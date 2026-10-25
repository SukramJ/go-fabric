// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/SukramJ/go-fabric/controller/dcl"
)

// VendorsFile is the snapshot's vendor list: the vendor records of the
// ledger, so a vendor id resolves to a name without the network.
const VendorsFile = "vendors.json"

// Vendor is one vendor — matter.js VendorInfo
// (packages/protocol/src/dcl/DclVendorInfoService.ts:41-48 at 862ad75f),
// the ledger's vendor record normalised (:390-400).
type Vendor struct {
	ID                   uint16
	Name                 string
	CompanyLegalName     string
	CompanyPreferredName string
	LandingPageURL       string
	Creator              string
}

// vendorSeed is the file's form — matter.js SeedSource of VendorEntry
// (packages/protocol/src/dcl/SeedTypes.ts:7-11, :24-32).
type vendorSeed struct {
	BuiltAt       string        `json:"builtAt"`
	ExpectedCount int           `json:"expectedCount"`
	Entries       []vendorEntry `json:"entries"`
}

type vendorEntry struct {
	VendorID             *int   `json:"vendorId"`
	VendorName           string `json:"vendorName"`
	CompanyLegalName     string `json:"companyLegalName,omitempty"`
	CompanyPreferredName string `json:"companyPreferredName,omitempty"`
	VendorLandingPageURL string `json:"vendorLandingPageURL,omitempty"`
	Creator              string `json:"creator,omitempty"`
	Kind                 string `json:"kind"`
}

// staticVendors are the vendors known without the ledger — matter.js
// STATIC_VENDORS (DclVendorInfoService.ts:136-158 at 862ad75f): the test
// vendor ids of the specification (0xFFF1–0xFFF4) and Nabu Casa. A ledger
// record of the same id takes precedence.
func staticVendors() []Vendor {
	out := make([]Vendor, 0, 5)
	for _, id := range []uint16{0xFFF1, 0xFFF2, 0xFFF3, 0xFFF4} {
		out = append(out, Vendor{
			ID: id, Name: "Test Vendor", CompanyLegalName: "Test Vendor Inc.", CompanyPreferredName: "Test Vendor",
			LandingPageURL: "https://test.example.com", Creator: "System",
		})
	}
	return append(out, Vendor{
		ID: 4939, Name: "Nabu Casa", CompanyLegalName: "Nabu Casa, Inc.", CompanyPreferredName: "Nabu Casa",
		LandingPageURL: "https://www.nabucasa.com", Creator: "System",
	})
}

// loadVendors reads the vendor list of fsys over the static vendors, as
// #consumeVendorSeed does (DclVendorInfoService.ts:405-445 at 862ad75f):
// an entry without a numeric id, without a name or of another kind than
// production or test stops the list there ([ErrMalformed]; the entries
// before it are kept). No file is the static vendors alone.
func (s *Snapshot) loadVendors(fsys fs.FS) error {
	s.vendors = map[uint16]Vendor{}
	for _, v := range staticVendors() {
		s.vendors[v.ID] = v
	}
	b, err := fs.ReadFile(fsys, VendorsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var seed vendorSeed
	if err := json.Unmarshal(b, &seed); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformed, VendorsFile, err)
	}
	for _, e := range seed.Entries {
		if e.VendorID == nil || *e.VendorID < 0 || *e.VendorID > 0xFFFF || e.VendorName == "" || (e.Kind != KindProduction && e.Kind != KindTest) {
			return fmt.Errorf("%w: %s: invalid vendor entry shape %+v", ErrMalformed, VendorsFile, e)
		}
		id := uint16(*e.VendorID)
		s.vendors[id] = Vendor{
			ID: id, Name: e.VendorName, CompanyLegalName: e.CompanyLegalName, CompanyPreferredName: e.CompanyPreferredName,
			LandingPageURL: e.VendorLandingPageURL, Creator: e.Creator,
		}
	}
	return nil
}

// Vendors is every vendor the snapshot knows — the ledger's records it
// was built with over matter.js's static vendors — in id order: matter.js
// DclVendorInfoService.vendors (DclVendorInfoService.ts:228-231 at
// 862ad75f), offline.
func (s *Snapshot) Vendors() []Vendor {
	out := make([]Vendor, 0, len(s.vendors))
	for _, v := range s.vendors {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Vendor) int { return int(a.ID) - int(b.ID) })
	return out
}

// Vendor is the vendor of id; false when the snapshot knows none —
// DclVendorInfoService.infoFor (:221-224).
func (s *Snapshot) Vendor(id uint16) (Vendor, bool) {
	v, ok := s.vendors[id]
	return v, ok
}

// FetchVendors writes the ledger's vendor records into dir as
// [VendorsFile] (`make update-dcl-snapshot`, or its -vendors-only form):
// DclClient.fetchAllVendors through [dcl.Client.Vendors], every record
// of schema version 0, in id order, as production entries. It is a review
// step as [Fetch] is.
func FetchVendors(ctx context.Context, client *dcl.Client, dir, builtAt string) (int, error) {
	vendors, err := client.Vendors(ctx)
	if err != nil {
		return 0, fmt.Errorf("snapshot: vendors: %w", err)
	}
	if len(vendors) == 0 {
		return 0, errors.New("snapshot: the ledger returned no vendor")
	}
	slices.SortFunc(vendors, func(a, b dcl.Vendor) int { return int(a.VendorID) - int(b.VendorID) })
	seed := vendorSeed{BuiltAt: builtAt, ExpectedCount: len(vendors), Entries: make([]vendorEntry, 0, len(vendors))}
	for _, v := range vendors {
		if v.VendorName == "" {
			continue
		}
		id := int(v.VendorID)
		seed.Entries = append(seed.Entries, vendorEntry{
			VendorID: &id, VendorName: v.VendorName, CompanyLegalName: v.CompanyLegalName,
			CompanyPreferredName: v.CompanyPreferredName, VendorLandingPageURL: v.VendorLandingPageURL,
			Creator: v.Creator, Kind: KindProduction,
		})
	}
	seed.ExpectedCount = len(seed.Entries)
	b, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(dir, VendorsFile), append(b, '\n'), 0o644); err != nil { //nolint:gosec // G306: public ledger records in the source tree
		return 0, err
	}
	return len(seed.Entries), nil
}
