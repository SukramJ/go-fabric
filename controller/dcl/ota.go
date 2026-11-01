// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

// DefaultDownloadTimeout bounds an image download — OTA_DOWNLOAD_TIMEOUT,
// Minutes(5) (DclOtaUpdateService.ts:53).
const DefaultDownloadTimeout = 5 * time.Minute

// OTAError is an update check or download that failed — OtaUpdateError
// (DclOtaUpdateService.ts:33-37).
type OTAError struct {
	Message string
	Err     error
}

func (e *OTAError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *OTAError) Unwrap() error { return e.Err }

// Update sources — OtaUpdateSource (DclOtaUpdateService.ts:39).
const (
	SourceProduction = "dcl-prod"
	SourceTest       = "dcl-test"
)

// OTAUpdate is an applicable software version and the ledger it came from
// — OtaUpdateInfo (DclOtaUpdateService.ts:43-51).
type OTAUpdate struct {
	SoftwareVersion
	Source string
}

// OTAVersions answers an OTA provider's ledger questions — the parts of
// matter.js DclOtaUpdateService (packages/protocol/src/dcl/
// DclOtaUpdateService.ts at 0ceb202d) that do not store images: which
// software version of a vendor and product the ledger offers over the
// current one, whether one version is applicable, its image download and
// the image's verification against the record. The provider's image
// catalogue stays the host's (cluster/core.OTAImageSource): a host's
// QueryImage calls [OTAVersions.CheckForUpdate], downloads with
// [OTAVersions.Download] and checks with [VerifyImage].
type OTAVersions struct {
	// Client asks the production ledger; nil asks none.
	Client *Client
	// TestClient asks the test-net ledger; nil asks none.
	TestClient *Client
	// Logger receives the lookups' log; nil discards it.
	Logger *slog.Logger
}

func (o *OTAVersions) log() *slog.Logger {
	if o.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Logger
}

// CheckForUpdate returns the ledger's applicable update of vid/pid over
// current — checkForUpdate (DclOtaUpdateService.ts:292-386) without the
// stored images: the production ledger, then the test-net ledger, the
// highest version of the two. With target set only that version is
// considered. A failed lookup is logged and finds nothing.
func (o *OTAVersions) CheckForUpdate(ctx context.Context, vid, pid uint16, current uint32, target *uint32) (OTAUpdate, bool) {
	var found []OTAUpdate
	for _, src := range []struct {
		client *Client
		source string
	}{{o.Client, SourceProduction}, {o.TestClient, SourceTest}} {
		if src.client == nil {
			continue
		}
		v, ok := o.queryDCLForUpdate(ctx, src.client, src.source, vid, pid, current, target)
		if !ok {
			continue
		}
		u := OTAUpdate{SoftwareVersion: v, Source: src.source}
		if target != nil && u.SoftwareVersion.SoftwareVersion == *target {
			return u, true
		}
		found = append(found, u)
	}
	// The loop would have returned the targeted version already (:378-381).
	if target != nil || len(found) == 0 {
		return OTAUpdate{}, false
	}
	slices.SortStableFunc(found, func(a, b OTAUpdate) int {
		switch {
		case a.SoftwareVersion.SoftwareVersion > b.SoftwareVersion.SoftwareVersion:
			return -1
		case a.SoftwareVersion.SoftwareVersion < b.SoftwareVersion.SoftwareVersion:
			return 1
		}
		return 0
	})
	return found[0], true
}

// queryDCLForUpdate is #queryDclForUpdate (:182-281): the targeted version
// alone, or every listed version above current, highest first, until one
// is applicable.
func (o *OTAVersions) queryDCLForUpdate(ctx context.Context, c *Client, source string, vid, pid uint16, current uint32, target *uint32) (SoftwareVersion, bool) {
	attrs := []any{slog.String("source", source), slog.Int("vendor_id", int(vid)), slog.Int("product_id", int(pid)), slog.Uint64("current", uint64(current))}
	if target != nil {
		v, ok, err := o.CheckVersion(ctx, c, vid, pid, *target, current)
		if err != nil {
			o.logLookupFailure(err, vid, pid, attrs)
			return SoftwareVersion{}, false
		}
		return v, ok
	}
	versions, err := c.ModelVersions(ctx, vid, pid)
	if err != nil {
		o.logLookupFailure(err, vid, pid, attrs)
		return SoftwareVersion{}, false
	}
	var newer []uint32
	for _, v := range versions {
		if v > current {
			newer = append(newer, v)
		}
	}
	slices.Sort(newer)
	slices.Reverse(newer)
	for _, version := range newer {
		v, ok, err := o.CheckVersion(ctx, c, vid, pid, version, current)
		if err != nil {
			o.log().Info("dcl.ota.check_failed", append(attrs, slog.Any("err", err))...)
			continue
		}
		if ok {
			o.log().Info("dcl.ota.update_available", append(attrs, slog.Uint64("new", uint64(version)))...)
			return v, true
		}
	}
	o.log().Debug("dcl.ota.no_applicable_update", attrs...)
	return SoftwareVersion{}, false
}

// logLookupFailure is :273-280: NOT_FOUND is no update, anything else a
// failed check.
func (o *OTAVersions) logLookupFailure(err error, vid, pid uint16, attrs []any) {
	if IsNotFound(err) {
		o.log().Debug("dcl.ota.no_applicable_update", attrs...)
		return
	}
	o.log().Info("dcl.ota.check_failed", append(attrs, slog.Any("err", err),
		slog.String("message", fmt.Sprintf("Failed to check for updates for VID: %d, PID: %d", vid, pid)))...)
}

// CheckVersion fetches the record of one software version and reports
// whether it is applicable over current — #checkSpecificVersion
// (:722-764). An applicable version carries the specification version of
// its compliance record, else the version record's.
func (o *OTAVersions) CheckVersion(ctx context.Context, c *Client, vid, pid uint16, version, current uint32) (SoftwareVersion, bool, error) {
	v, err := c.ModelVersion(ctx, vid, pid, version)
	if err != nil {
		return SoftwareVersion{}, false, err
	}
	if v.SoftwareVersion != version {
		return SoftwareVersion{}, false, &OTAError{Message: fmt.Sprintf("Version mismatch: requested %d, but DCL returned %d", version, v.SoftwareVersion)}
	}
	if !o.IsApplicable(v, current) {
		o.log().Debug("dcl.ota.not_applicable", slog.Uint64("version", uint64(version)), slog.Bool("valid", v.SoftwareVersionValid),
			slog.Bool("url", v.OTAURL != ""), slog.Uint64("min", uint64(v.MinApplicableSoftwareVersion)), slog.Uint64("max", uint64(v.MaxApplicableSoftwareVersion)))
		return v, false, nil
	}
	// Mirrors matter.js packages/protocol/src/dcl/DclOtaUpdateService.ts:747-752:
	// the compliance record's specification version, the version record's
	// when it has none.
	if spec := o.specificationVersionOf(ctx, c, vid, pid, version); spec != 0 {
		v.SpecificationVersion = spec
	}
	return v, true, nil
}

// specificationVersionOf reads the specification version of a software
// version from its compliance record of the "matter" certification; 0
// when the record states none or the lookup fails, which does not fail
// the check.
//
// Mirrors matter.js packages/protocol/src/dcl/DclOtaUpdateService.ts:771-781
// (#specificationVersionOf) with DclClient.fetchComplianceInfo
// (packages/protocol/src/dcl/DclClient.ts:229-257): the record must answer
// that vid, pid, version and certification type in schema version 0 or 1.
func (o *OTAVersions) specificationVersionOf(ctx context.Context, c *Client, vid, pid uint16, version uint32) uint32 {
	info, err := c.ComplianceInfo(ctx, vid, pid, version, "matter")
	if err == nil {
		return info.SpecificationVersion
	}
	o.log().Debug("dcl.ota.no_specification_version", slog.String("message",
		fmt.Sprintf("No specification version from compliance info for VID: %d, PID: %d, version %d: %s", vid, pid, version, err.Error())))
	return 0
}

// IsApplicable is #isVersionApplicable (:786-819): a valid version with an
// https OTA URL whose applicable range holds current. The ledger states
// both bounds on every record, so a maximum of 0 applies to version 0
// only.
func (o *OTAVersions) IsApplicable(v SoftwareVersion, current uint32) bool {
	if !v.SoftwareVersionValid || strings.TrimSpace(v.OTAURL) == "" {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(v.OTAURL), "https://") {
		o.log().Warn("dcl.ota.invalid_url", slog.String("url", v.OTAURL),
			slog.String("message", fmt.Sprintf("Invalid OTA URL from DCL: %q - must use https:// protocol", v.OTAURL)))
		return false
	}
	return current >= v.MinApplicableSoftwareVersion && current <= v.MaxApplicableSoftwareVersion
}

// Download writes the image of u to w — downloadUpdate's network half
// (:667-709): an http or https OTA URL only, the GET bounded by
// [DefaultDownloadTimeout]. The caller verifies the image ([VerifyImage])
// before it serves it. When the record states the image's size
// (OTAFileSize), at most that many bytes are copied: a longer answer is an
// OTAError, not an image held in full. matter.js streams the answer to
// storage without a bound (DclOtaUpdateService.ts:703, :571) and checks
// the size on verification (:408, OtaImageReader.file with otaFileSize);
// the bound keeps that verdict without
// buffering what an OTA host sends past the announced size.
func (o *OTAVersions) Download(ctx context.Context, u SoftwareVersion, w io.Writer) error {
	if strings.TrimSpace(u.OTAURL) == "" {
		return &OTAError{Message: "No OTA URL provided in update info"}
	}
	parsed, err := url.Parse(u.OTAURL)
	if err != nil || parsed.Scheme == "" {
		return &OTAError{Message: "Invalid OTA URL: " + u.OTAURL}
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "https" && scheme != "http" {
		return &OTAError{Message: fmt.Sprintf(`Unsupported protocol "%s:" in OTA URL. Only "https:", "http:" are supported`, scheme)}
	}
	o.log().Info("dcl.ota.download", slog.String("url", u.OTAURL), slog.Int("vendor_id", int(u.VID)), slog.Int("product_id", int(u.PID)),
		slog.String("version", fmt.Sprintf("%d (%s)", u.SoftwareVersion, u.SoftwareVersionString)))
	ctx, cancel := context.WithTimeout(ctx, DefaultDownloadTimeout)
	defer cancel()
	fail := func(err error) error {
		return &OTAError{Message: "Failed to download OTA image from " + u.OTAURL, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.OTAURL, http.NoBody)
	if err != nil {
		return fail(err)
	}
	hc := http.DefaultClient
	if o.Client != nil {
		hc = o.Client.httpClient()
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &OTAError{Message: fmt.Sprintf("Failed to download OTA image: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	if u.OTAFileSize == 0 {
		if _, err := io.Copy(w, resp.Body); err != nil {
			return fail(err)
		}
		return nil
	}
	limit := int64(min(u.OTAFileSize, 1<<62)) //nolint:gosec // bounded above
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fail(err)
	}
	if n > limit {
		return &OTAError{Message: fmt.Sprintf("OTA image from %s exceeds its announced size of %d bytes", u.OTAURL, u.OTAFileSize)}
	}
	return nil
}

// prefixCapture keeps the first 16 bytes of an image, its fixed prefix.
type prefixCapture struct{ b []byte }

func (p *prefixCapture) Write(b []byte) (int, error) {
	if n := 16 - len(p.b); n > 0 {
		p.b = append(p.b, b[:min(n, len(b))]...)
	}
	return len(b), nil
}

// VerifyImage reads the whole image r and checks it against the ledger's
// record u — #verifyUpdate (DclOtaUpdateService.ts:388-424) with
// OtaImageReader.file (ota/OtaImageReader.ts:65-110): the image parses
// and carries its payload, the whole-file checksum matches when the
// ledger states one (its type defaulting to SHA-256, an unsupported type
// an error), the total size matches the ledger's file size unless the
// checksum matched, and the header names u's vendor, product and version.
func VerifyImage(r io.Reader, u SoftwareVersion, log *slog.Logger) (otaimage.Header, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	checksumType := u.OTAChecksumType
	if checksumType == 0 {
		checksumType = otaimage.DigestSHA256
	}
	sum := otaimage.NewDigest(checksumType)
	if u.OTAChecksum != "" && sum == nil {
		return otaimage.Header{}, &OTAError{Message: fmt.Sprintf("Unsupported OTA checksum type: %d", u.OTAChecksumType)}
	}
	if sum == nil {
		sum = otaimage.NewDigest(otaimage.DigestSHA256)
	}
	prefix := &prefixCapture{}
	h, err := otaimage.ValidateFile(io.TeeReader(r, io.MultiWriter(sum, prefix)), otaimage.FileOptions{})
	if err != nil {
		return otaimage.Header{}, err
	}
	matched := false
	if u.OTAChecksum != "" {
		if got := base64.StdEncoding.EncodeToString(sum.Sum(nil)); got != u.OTAChecksum {
			return otaimage.Header{}, fmt.Errorf("%w: OTA full file checksum mismatch: expected %q, got %q", otaimage.ErrInvalid, u.OTAChecksum, got)
		}
		matched = true
	}
	if u.OTAFileSize > 0 && len(prefix.b) >= 12 {
		if total := binary.LittleEndian.Uint64(prefix.b[4:12]); total != u.OTAFileSize {
			if !matched {
				return otaimage.Header{}, fmt.Errorf("%w: OTA file size mismatch: expected %d, got %d", otaimage.ErrInvalid, u.OTAFileSize, total)
			}
			log.Warn("dcl.ota.size_mismatch", slog.String("message",
				fmt.Sprintf("OTA file size mismatch, but checksum matched: expected %d, got %d", u.OTAFileSize, total)))
		}
	}
	switch {
	case h.VendorID != u.VID:
		return otaimage.Header{}, &OTAError{Message: fmt.Sprintf("OTA image vendor ID mismatch: expected %d, got %d", u.VID, h.VendorID)}
	case h.ProductID != u.PID:
		return otaimage.Header{}, &OTAError{Message: fmt.Sprintf("OTA image product ID mismatch: expected %d, got %d", u.PID, h.ProductID)}
	case h.SoftwareVersion != u.SoftwareVersion:
		return otaimage.Header{}, &OTAError{Message: fmt.Sprintf("OTA image software version mismatch: expected %d, got %d", u.SoftwareVersion, h.SoftwareVersion)}
	}
	return h, nil
}

// errNoClient: a lookup without a ledger.
var errNoClient = errors.New("dcl: no client")

// Lookup returns the production ledger's record of one software version —
// "is this version in the ledger, with which checksum": false when the
// ledger has none (NOT_FOUND, or an answer for another version).
func (o *OTAVersions) Lookup(ctx context.Context, vid, pid uint16, version uint32) (SoftwareVersion, bool, error) {
	if o.Client == nil {
		return SoftwareVersion{}, false, errNoClient
	}
	v, err := o.Client.ModelVersion(ctx, vid, pid, version)
	var de *Error
	switch {
	case IsNotFound(err), errors.As(err, &de):
		return SoftwareVersion{}, false, nil
	case err != nil:
		return SoftwareVersion{}, false, err
	}
	return v, true, nil
}
