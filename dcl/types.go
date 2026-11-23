// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"math"
	"regexp"
	"strconv"
)

// The ledger's records, in the wire form of DclRestApiTypes.ts at 0ceb202d:
// every field is sent, an unset optional one as "" or 0. Where matter.js's
// DclClient maps "" or 0 to undefined, the Go field keeps the zero value,
// which means the same.

// SubjectReference names a root certificate — DclPkiRootCertificateSubjectReference
// (DclRestApiTypes.ts:46-49).
type SubjectReference struct {
	Subject      string `json:"subject"`
	SubjectKeyID string `json:"subjectKeyId"`
}

// Certificate is a PAA or PAI record — ProductAttestationDclSchema
// (packages/types/src/dcl/attestation-certificate.ts:48-154), the fields
// this module reads.
type Certificate struct {
	// PEMCert is the certificate, PEM encoded.
	PEMCert string `json:"pemCert"`
	// SerialNumber is the serial as a decimal string.
	SerialNumber     string `json:"serialNumber"`
	Issuer           string `json:"issuer"`
	AuthorityKeyID   string `json:"authorityKeyId"`
	RootSubject      string `json:"rootSubject"`
	RootSubjectKeyID string `json:"rootSubjectKeyId"`
	IsRoot           bool   `json:"isRoot"`
	Owner            string `json:"owner"`
	// Subject is the base64 subject DN, SubjectAsText its text form.
	Subject       string `json:"subject"`
	SubjectAsText string `json:"subjectAsText"`
	// SubjectKeyID is colon-separated uppercase hex.
	SubjectKeyID    string `json:"subjectKeyId"`
	CertificateType string `json:"certificateType"`
	// VID is 0 for a PAA not scoped to one vendor.
	VID           uint16 `json:"vid"`
	SchemaVersion int    `json:"schemaVersion"`
}

// DeviceModel is a device model record — DeviceModelDclSchema
// (packages/types/src/dcl/device.ts:53-314), the fields with a meaning
// once DclClient.mapRawDeviceModel (DclClient.ts:337-358) has cleared
// those that only count when their URL is set.
type DeviceModel struct {
	VID                                        uint16 `json:"vid"`
	PID                                        uint16 `json:"pid"`
	DeviceTypeID                               uint32 `json:"deviceTypeId"`
	ProductName                                string `json:"productName"`
	ProductLabel                               string `json:"productLabel"`
	PartNumber                                 string `json:"partNumber"`
	DiscoveryCapabilitiesBitmask               uint32 `json:"discoveryCapabilitiesBitmask"`
	CommissioningCustomFlow                    uint8  `json:"commissioningCustomFlow"`
	CommissioningCustomFlowURL                 string `json:"commissioningCustomFlowUrl"`
	CommissioningModeInitialStepsHint          uint32 `json:"commissioningModeInitialStepsHint"`
	CommissioningModeInitialStepsInstruction   string `json:"commissioningModeInitialStepsInstruction"`
	CommissioningModeSecondaryStepsHint        uint32 `json:"commissioningModeSecondaryStepsHint"`
	CommissioningModeSecondaryStepsInstruction string `json:"commissioningModeSecondaryStepsInstruction"`
	CommissioningFallbackURL                   string `json:"commissioningFallbackUrl"`
	UserManualURL                              string `json:"userManualUrl"`
	SupportURL                                 string `json:"supportUrl"`
	ProductURL                                 string `json:"productUrl"`
	LSFURL                                     string `json:"lsfUrl"`
	LSFRevision                                uint32 `json:"lsfRevision"`
	EnhancedSetupFlowOptions                   uint32 `json:"enhancedSetupFlowOptions"`
	EnhancedSetupFlowTCURL                     string `json:"enhancedSetupFlowTCUrl"`
	EnhancedSetupFlowTCRevision                uint32 `json:"enhancedSetupFlowTCRevision"`
	EnhancedSetupFlowTCDigest                  string `json:"enhancedSetupFlowTCDigest"`
	EnhancedSetupFlowTCFileSize                uint64 `json:"enhancedSetupFlowTCFileSize"`
	MaintenanceURL                             string `json:"maintenanceUrl"`
	ICDUserActiveModeTriggerHint               uint32 `json:"icdUserActiveModeTriggerHint"`
	ICDUserActiveModeTriggerInstruction        string `json:"icdUserActiveModeTriggerInstruction"`
	FactoryResetStepsHint                      uint32 `json:"factoryResetStepsHint"`
	FactoryResetStepsInstruction               string `json:"factoryResetStepsInstruction"`
	Creator                                    string `json:"creator"`
	SchemaVersion                              int    `json:"schemaVersion"`
}

// mapped is mapRawDeviceModel (DclClient.ts:337-358): the LSF revision only
// with an LSF URL, the Terms and Conditions revision, digest and size only
// with their URL.
func (m DeviceModel) mapped() DeviceModel {
	if m.LSFURL == "" {
		m.LSFRevision = 0
	}
	if m.EnhancedSetupFlowTCURL == "" {
		m.EnhancedSetupFlowTCRevision, m.EnhancedSetupFlowTCDigest, m.EnhancedSetupFlowTCFileSize = 0, "", 0
	}
	return m
}

// SoftwareVersion is a software version record — DeviceSoftwareVersionModelDclSchema
// (packages/types/src/dcl/device-software-version.ts:20-159).
type SoftwareVersion struct {
	VID                   uint16
	PID                   uint16
	SoftwareVersion       uint32
	SoftwareVersionString string
	CDVersionNumber       uint32
	Creator               string
	FirmwareInformation   string
	SoftwareVersionValid  bool
	// OTAURL is where the image is, "" when the ledger names none.
	OTAURL string
	// OTAFileSize is the image's size, 0 when unknown.
	OTAFileSize uint64
	// OTAChecksum is the base64 digest of the whole image, OTAChecksumType
	// its IANA Named Information hash id (1 is SHA-256); "" and 0 when the
	// ledger states none.
	OTAChecksum                  string
	OTAChecksumType              uint8
	MinApplicableSoftwareVersion uint32
	// MaxApplicableSoftwareVersion is math.MaxUint32 when the record
	// states no maximum (unbounded).
	MaxApplicableSoftwareVersion uint32
	ReleaseNotesURL              string
	SpecificationVersion         uint32
	SchemaVersion                int
}

// rawSoftwareVersion is DclDeviceSoftwareVersionModelRaw
// (DclRestApiTypes.ts:144-148): the uint64 otaFileSize as a decimal string.
type rawSoftwareVersion struct {
	VID                          uint16  `json:"vid"`
	PID                          uint16  `json:"pid"`
	SoftwareVersion              uint32  `json:"softwareVersion"`
	SoftwareVersionString        string  `json:"softwareVersionString"`
	CDVersionNumber              uint32  `json:"cdVersionNumber"`
	Creator                      string  `json:"creator"`
	FirmwareInformation          string  `json:"firmwareInformation"`
	SoftwareVersionValid         bool    `json:"softwareVersionValid"`
	OTAURL                       string  `json:"otaUrl"`
	OTAFileSize                  string  `json:"otaFileSize"`
	OTAChecksum                  string  `json:"otaChecksum"`
	OTAChecksumType              uint8   `json:"otaChecksumType"`
	MinApplicableSoftwareVersion uint32  `json:"minApplicableSoftwareVersion"`
	MaxApplicableSoftwareVersion *uint32 `json:"maxApplicableSoftwareVersion"`
	ReleaseNotesURL              string  `json:"releaseNotesUrl"`
	SpecificationVersion         uint32  `json:"specificationVersion"`
	SchemaVersion                int     `json:"schemaVersion"`
}

var decimal = regexp.MustCompile(`^\d+$`)

// mapped is mapRawModelVersion (DclClient.ts:387-399): a file size that is
// not a decimal number, or 0, is unknown.
//
// A record without maxApplicableSoftwareVersion has no upper bound: it maps
// to math.MaxUint32, as matter.js skips the check for an undefined maximum
// (DclOtaUpdateService.ts:811-816); it read as 0 and the update was never
// offered (W4-ctlint-06). An absent minimum is 0, unbounded as well.
func (r rawSoftwareVersion) mapped() SoftwareVersion {
	var size uint64
	if decimal.MatchString(r.OTAFileSize) {
		size, _ = strconv.ParseUint(r.OTAFileSize, 10, 64)
	}
	maxApplicable := uint32(math.MaxUint32)
	if r.MaxApplicableSoftwareVersion != nil {
		maxApplicable = *r.MaxApplicableSoftwareVersion
	}
	return SoftwareVersion{
		VID: r.VID, PID: r.PID, SoftwareVersion: r.SoftwareVersion, SoftwareVersionString: r.SoftwareVersionString,
		CDVersionNumber: r.CDVersionNumber, Creator: r.Creator, FirmwareInformation: r.FirmwareInformation,
		SoftwareVersionValid: r.SoftwareVersionValid, OTAURL: r.OTAURL, OTAFileSize: size,
		OTAChecksum: r.OTAChecksum, OTAChecksumType: r.OTAChecksumType,
		MinApplicableSoftwareVersion: r.MinApplicableSoftwareVersion, MaxApplicableSoftwareVersion: maxApplicable,
		ReleaseNotesURL: r.ReleaseNotesURL, SpecificationVersion: r.SpecificationVersion, SchemaVersion: r.SchemaVersion,
	}
}

// Vendor is a vendor record — VendorDclSchema
// (packages/types/src/dcl/vendor.ts:16-61); the optional preferred name
// and landing page are "" when unset (DclClient.ts:315-321).
type Vendor struct {
	VendorID             uint16 `json:"vendorID"`
	VendorName           string `json:"vendorName"`
	CompanyLegalName     string `json:"companyLegalName"`
	CompanyPreferredName string `json:"companyPreferredName"`
	VendorLandingPageURL string `json:"vendorLandingPageURL"`
	Creator              string `json:"creator"`
	SchemaVersion        int    `json:"schemaVersion"`
}

// ComplianceInfo is a compliance record — DeviceSoftwareComplianceDclSchema
// (packages/types/src/dcl/device-software-compliance.ts:63-208). The four
// fields the specification deprecates are not read, as mapRawComplianceInfo
// drops them (DclClient.ts:360-381); an unset optional field is "" or 0.
type ComplianceInfo struct {
	VID                                uint16                  `json:"vid"`
	PID                                uint16                  `json:"pid"`
	SoftwareVersion                    uint32                  `json:"softwareVersion"`
	SoftwareVersionString              string                  `json:"softwareVersionString"`
	CDVersionNumber                    uint32                  `json:"cDVersionNumber"`
	CDCertificateID                    string                  `json:"cDCertificateId"`
	CertificationType                  string                  `json:"certificationType"`
	SpecificationVersion               uint32                  `json:"specificationVersion"`
	SoftwareVersionCertificationStatus uint8                   `json:"softwareVersionCertificationStatus"`
	Date                               string                  `json:"date"`
	Owner                              string                  `json:"owner"`
	History                            []ComplianceHistoryItem `json:"history"`
	CertificationRoute                 string                  `json:"certificationRoute"`
	Reason                             string                  `json:"reason"`
	Transport                          string                  `json:"transport"`
	FamilyID                           string                  `json:"familyId"`
	SupportedClusters                  string                  `json:"supportedClusters"`
	ProgramType                        string                  `json:"programType"`
	ProgramTypeVersion                 string                  `json:"programTypeVersion"`
	ParentChild                        string                  `json:"parentChild"`
	SchemaVersion                      int                     `json:"schemaVersion"`
}

// ComplianceHistoryItem is one entry of a compliance record's history —
// ComplianceHistoryItemDclSchema (device-software-compliance.ts:23-49).
type ComplianceHistoryItem struct {
	SoftwareVersionCertificationStatus uint8  `json:"softwareVersionCertificationStatus"`
	Date                               string `json:"date"`
	Reason                             string `json:"reason"`
	CDVersionNumber                    uint32 `json:"cDVersionNumber"`
	SchemaVersion                      int    `json:"schemaVersion"`
}

// RevocationTypeCRL is RevocationTypeEnum.Crl
// (packages/types/src/dcl/device-attestation-revocation.ts:9-14).
const RevocationTypeCRL = 1

// RevocationPoint is a revocation distribution point —
// DeviceAttestationPkiRevocationDclSchema
// (packages/types/src/dcl/device-attestation-revocation.ts:24-148).
type RevocationPoint struct {
	VID uint16
	// PID is 0 when absent.
	PID   uint16
	IsPAA bool
	Label string
	// CRLSignerDelegator and CRLSignerCertificate are PEM; the delegator is
	// "" when absent.
	CRLSignerDelegator   string
	CRLSignerCertificate string
	// IssuerSubjectKeyID is uppercase hex without separators.
	IssuerSubjectKeyID string
	DataURL            string
	// DataFileSize is 0 when absent.
	DataFileSize uint64
	// DataDigest is base64, DataDigestType its IANA hash id; "" and 0
	// when absent.
	DataDigest     string
	DataDigestType uint8
	RevocationType int
	SchemaVersion  int
}

// rawRevocationPoint is DclPkiRevocationDistributionPointRaw
// (DclRestApiTypes.ts:165-179): issuerSubjectKeyID and dataURL with their
// capital endings, dataFileSize a decimal string.
type rawRevocationPoint struct {
	VID                  uint16 `json:"vid"`
	PID                  uint16 `json:"pid"`
	IsPAA                bool   `json:"isPAA"`
	Label                string `json:"label"`
	CRLSignerDelegator   string `json:"crlSignerDelegator"`
	CRLSignerCertificate string `json:"crlSignerCertificate"`
	IssuerSubjectKeyID   string `json:"issuerSubjectKeyID"`
	DataURL              string `json:"dataURL"`
	DataFileSize         string `json:"dataFileSize"`
	DataDigest           string `json:"dataDigest"`
	DataDigestType       uint8  `json:"dataDigestType"`
	RevocationType       int    `json:"revocationType"`
	SchemaVersion        int    `json:"schemaVersion"`
}

// mapped is mapRawRevocationPoint (DclClient.ts:406-422); a file size that
// does not parse is absent (parseInt(…, 10) || undefined).
func (r rawRevocationPoint) mapped() RevocationPoint {
	return RevocationPoint{
		VID: r.VID, PID: r.PID, IsPAA: r.IsPAA, Label: r.Label,
		CRLSignerDelegator: r.CRLSignerDelegator, CRLSignerCertificate: r.CRLSignerCertificate,
		IssuerSubjectKeyID: r.IssuerSubjectKeyID, DataURL: r.DataURL, DataFileSize: leadingDecimal(r.DataFileSize),
		DataDigest: r.DataDigest, DataDigestType: r.DataDigestType, RevocationType: r.RevocationType, SchemaVersion: r.SchemaVersion,
	}
}

// leadingDecimal is parseInt(s, 10) || 0 for a non-negative size: the
// leading decimal digits, 0 when there are none.
func leadingDecimal(s string) uint64 {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	v, err := strconv.ParseUint(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return v
}
