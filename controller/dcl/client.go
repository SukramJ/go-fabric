// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The ledger's REST endpoints — matter.js DclConfig.production and
// DclConfig.test (packages/protocol/src/dcl/DclConfig.ts:17-25 at
// 0ceb202d).
const (
	ProductionURL = "https://on.dcl.csa-iot.org"
	TestNetURL    = "https://on.test-net.dcl.csa-iot.org"
)

// DefaultTimeout bounds one ledger request — DclClient.ts:39
// (DEFAULT_DCL_TIMEOUT = Seconds(5)).
const DefaultTimeout = 5 * time.Second

// CodeNotFound is the gRPC NOT_FOUND code the ledger answers an absent
// record with (DclRestApiTypes.ts:20-38, DclErrorCodes.NotFound).
const CodeNotFound = 5

// Error is a ledger answer the client cannot use — matter.js MatterDclError
// (DclClient.ts:42).
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func dclErrorf(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// ResponseError is a request that failed — matter.js MatterDclResponseError
// (DclClient.ts:45-52): the ledger's error body when it answered one, code
// 500 with the cause's message when the request itself failed
// (DclClient.ts:109-119).
type ResponseError struct {
	// Path is the requested path.
	Path string
	// Code, Message and Details are DclApiErrorResponse
	// (DclRestApiTypes.ts:40-44).
	Code    int
	Message string
	Details []string
	// Err is the cause of a failed request, nil for a ledger error body.
	Err error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("Error fetching %s from DCL: %d - %s", e.Path, e.Code, e.Message)
}

func (e *ResponseError) Unwrap() error { return e.Err }

// IsNotFound reports whether err is the ledger's NOT_FOUND answer.
func IsNotFound(err error) bool {
	var re *ResponseError
	return errors.As(err, &re) && re.Code == CodeNotFound
}

// Client reads the ledger's REST API — matter.js DclClient
// (DclClient.ts:55-299). The zero value asks the production ledger with
// the default HTTP client and timeout.
type Client struct {
	// BaseURL is the ledger; empty is [ProductionURL].
	BaseURL string
	// HTTPClient sends the requests; nil is http.DefaultClient.
	HTTPClient *http.Client
	// Timeout bounds each request; 0 is [DefaultTimeout].
	Timeout time.Duration
	// Logger receives the client's debug and warning lines; nil discards
	// them.
	Logger *slog.Logger
}

// NewClient returns a client of the ledger at baseURL.
func NewClient(baseURL string) *Client { return &Client{BaseURL: baseURL} }

func (c *Client) log() *slog.Logger {
	if c == nil || c.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.Logger
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// withTimeout is the client with another request timeout — the per-call
// DclClient.Options of matter.js (DclClient.ts:424-428).
func (c *Client) withTimeout(d time.Duration) *Client {
	cc := *c
	cc.Timeout = d
	return &cc
}

// baseURL is the ledger URL the client asks.
func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return ProductionURL
	}
	return c.BaseURL
}

// fetchJSON is DclClient.#fetchJson (DclClient.ts:91-121): a GET of path
// resolved against the base URL, with a JSON Content-Type header and the
// timeout; a non-OK answer is the ledger's error body, any other failure
// a ResponseError of code 500.
func (c *Client) fetchJSON(ctx context.Context, path string, out any) error {
	wrap := func(err error) error {
		var re *ResponseError
		if errors.As(err, &re) {
			return err
		}
		return &ResponseError{Path: path, Code: 500, Message: err.Error(), Details: []string{}, Err: err}
	}
	base, err := url.Parse(c.baseURL())
	if err != nil {
		return wrap(err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return wrap(err)
	}
	u := base.ResolveReference(ref).String()
	c.log().Debug("dcl.fetch", slog.String("url", u))
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return wrap(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return wrap(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return wrap(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Code    int      `json:"code"`
			Message string   `json:"message"`
			Details []string `json:"details"`
		}
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return wrap(err)
		}
		return &ResponseError{Path: path, Code: apiErr.Code, Message: apiErr.Message, Details: apiErr.Details}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return wrap(err)
	}
	return nil
}

// fetchPaginated is DclClient.#fetchPaginatedJson (DclClient.ts:62-89):
// the items of field, page after page while the answer names a
// pagination.next_key, which the next request carries as pagination.key.
func fetchPaginated[T any](ctx context.Context, c *Client, path, field string) ([]T, error) {
	var all []T
	nextKey := ""
	for first := true; first || nextKey != ""; first = false {
		current := path
		if nextKey != "" {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			current = path + sep + "pagination.key=" + encodeURIComponent(nextKey)
		}
		var page map[string]json.RawMessage
		if err := c.fetchJSON(ctx, current, &page); err != nil {
			return nil, err
		}
		// Items that are not an array are skipped (:79-82).
		if raw, ok := page[field]; ok {
			var items []T
			if json.Unmarshal(raw, &items) == nil {
				all = append(all, items...)
			}
		}
		nextKey = ""
		if raw, ok := page["pagination"]; ok {
			var p struct {
				NextKey *string `json:"next_key"`
			}
			if json.Unmarshal(raw, &p) == nil && p.NextKey != nil {
				nextKey = *p.NextKey
			}
		}
	}
	return all, nil
}

// encodeURIComponent is JavaScript's: every byte but A-Z a-z 0-9 and
// - _ . ! ~ * ' ( ) percent-encoded.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := range len(s) {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || strings.IndexByte("-_.!~*'()", ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

// RootCertificates lists the approved root certificates (PAAs) by subject
// and key identifier — DclClient.fetchRootCertificateList
// (DclClient.ts:123-131): GET /dcl/pki/root-certificates, schema version 0.
func (c *Client) RootCertificates(ctx context.Context) ([]SubjectReference, error) {
	var resp struct {
		ApprovedRootCertificates *rootCertificateList `json:"approvedRootCertificates"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/root-certificates", &resp); err != nil {
		return nil, err
	}
	if resp.ApprovedRootCertificates == nil || resp.ApprovedRootCertificates.SchemaVersion == nil || *resp.ApprovedRootCertificates.SchemaVersion != 0 {
		return nil, dclErrorf("Unsupported DCL Root Certificate schema version: %s", schemaVersionText(resp.ApprovedRootCertificates))
	}
	return resp.ApprovedRootCertificates.Certs, nil
}

// rootCertificateList is DclPkiRootCertificatesResponse's record
// (DclRestApiTypes.ts:52-57).
type rootCertificateList struct {
	Certs         []SubjectReference `json:"certs"`
	SchemaVersion *int               `json:"schemaVersion"`
}

func schemaVersionText(v *rootCertificateList) string {
	if v == nil || v.SchemaVersion == nil {
		return "undefined"
	}
	return strconv.Itoa(*v.SchemaVersion)
}

// hexPairs splits a key identifier into the pairs of the ledger's form,
// colon-separated uppercase hex (DclClient.ts:141-143).
var hexPairs = regexp.MustCompile(`.{1,2}`)

// CertificatesBySubjectKeyID returns every certificate the ledger holds
// with the key identifier skid (hex, colons allowed) — DclClient.
// fetchCertificatesBySubjectKeyId (DclClient.ts:140-152): GET
// /dcl/pki/all-certificates?subjectKeyId=<AA:BB:…>, the groups and
// certificates of schema version 0. None is an empty list.
func (c *Client) CertificatesBySubjectKeyID(ctx context.Context, skid string) ([]Certificate, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(skid, ":", ""))
	withColons := normalized
	if pairs := hexPairs.FindAllString(normalized, -1); pairs != nil {
		withColons = strings.Join(pairs, ":")
	}
	var resp struct {
		Certificates []certificateGroup `json:"certificates"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/all-certificates?subjectKeyId="+encodeURIComponent(withColons), &resp); err != nil {
		return nil, err
	}
	var out []Certificate
	groups := slices.DeleteFunc(slices.Clone(resp.Certificates), func(g certificateGroup) bool { return g.SchemaVersion != 0 })
	c.warnDropped(len(resp.Certificates)-len(groups), "certificate group")
	for _, g := range groups {
		out = append(out, c.supportedCertificates(g.Certs)...)
	}
	return out, nil
}

// certificateGroup is one subject's certificates in
// DclPkiAllCertificatesBySkidResponse (DclRestApiTypes.ts:60-67).
type certificateGroup struct {
	Certs         []Certificate `json:"certs"`
	SchemaVersion int           `json:"schemaVersion"`
}

// RootCertificateBySubject returns the approved certificates of one root —
// DclClient.fetchRootCertificateBySubject (DclClient.ts:154-169): GET
// /dcl/pki/certificates/{subject}/{subjectKeyId}, which must answer the
// subject and key identifier asked for, schema version 0.
func (c *Client) RootCertificateBySubject(ctx context.Context, ref SubjectReference) ([]Certificate, error) {
	path := "/dcl/pki/certificates/" + encodeURIComponent(ref.Subject) + "/" + encodeURIComponent(ref.SubjectKeyID)
	var resp struct {
		ApprovedCertificates *struct {
			Subject       string        `json:"subject"`
			SubjectKeyID  string        `json:"subjectKeyId"`
			Certs         []Certificate `json:"certs"`
			SchemaVersion *int          `json:"schemaVersion"`
		} `json:"approvedCertificates"`
	}
	if err := c.fetchJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	a := resp.ApprovedCertificates
	if a == nil || a.Subject != ref.Subject || a.SubjectKeyID != ref.SubjectKeyID || a.SchemaVersion == nil || *a.SchemaVersion != 0 {
		return nil, dclErrorf("Root certificate not found for subject: %s, subjectKeyId: %s", ref.Subject, ref.SubjectKeyID)
	}
	return c.supportedCertificates(a.Certs), nil
}

// Model returns the device model of vid/pid — DclClient.fetchModelByVidPid
// (DclClient.ts:171-184): GET /dcl/model/models/{vid}/{pid}, which must
// answer that vid and pid, schema version 0.
func (c *Client) Model(ctx context.Context, vid, pid uint16) (DeviceModel, error) {
	var resp struct {
		Model *DeviceModel `json:"model"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/models/%d/%d", vid, pid), &resp); err != nil {
		return DeviceModel{}, err
	}
	m := resp.Model
	if m == nil || m.VID != vid || m.PID != pid || m.SchemaVersion != 0 {
		return DeviceModel{}, dclErrorf("Model not found for VID: %d, PID: %d", vid, pid)
	}
	return m.mapped(), nil
}

// ModelVersions lists the software versions of vid/pid — DclClient.
// fetchModelVersionsByVidPid (DclClient.ts:186-199): GET
// /dcl/model/versions/{vid}/{pid}, which must answer that vid and pid,
// schema version 0.
func (c *Client) ModelVersions(ctx context.Context, vid, pid uint16) ([]uint32, error) {
	var resp struct {
		ModelVersions *struct {
			VID              uint16   `json:"vid"`
			PID              uint16   `json:"pid"`
			SoftwareVersions []uint32 `json:"softwareVersions"`
			SchemaVersion    int      `json:"schemaVersion"`
		} `json:"modelVersions"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/versions/%d/%d", vid, pid), &resp); err != nil {
		return nil, err
	}
	m := resp.ModelVersions
	if m == nil || m.VID != vid || m.PID != pid || m.SchemaVersion != 0 {
		return nil, dclErrorf("Model versions not found for VID: %d, PID: %d", vid, pid)
	}
	return m.SoftwareVersions, nil
}

// ModelVersion returns the record of one software version — DclClient.
// fetchModelVersionByVidPidSoftwareVersion (DclClient.ts:201-222): GET
// /dcl/model/versions/{vid}/{pid}/{softwareVersion}, which must answer that
// vid, pid and version, schema version 0.
func (c *Client) ModelVersion(ctx context.Context, vid, pid uint16, softwareVersion uint32) (SoftwareVersion, error) {
	var resp struct {
		ModelVersion *rawSoftwareVersion `json:"modelVersion"`
	}
	if err := c.fetchJSON(ctx, fmt.Sprintf("/dcl/model/versions/%d/%d/%d", vid, pid, softwareVersion), &resp); err != nil {
		return SoftwareVersion{}, err
	}
	m := resp.ModelVersion
	if m == nil || m.VID != vid || m.PID != pid || m.SoftwareVersion != softwareVersion || m.SchemaVersion != 0 {
		return SoftwareVersion{}, dclErrorf("Model version not found for VID: %d, PID: %d, Software Version: %d", vid, pid, softwareVersion)
	}
	return m.mapped(), nil
}

// ComplianceInfo returns the compliance record of a software version for
// a certification program — DclClient.fetchComplianceInfo
// (DclClient.ts:229-257): GET
// /dcl/compliance/compliance-info/{vid}/{pid}/{softwareVersion}/{certificationType},
// which must answer that vid, pid, version and program, schema version 0
// or 1.
func (c *Client) ComplianceInfo(ctx context.Context, vid, pid uint16, softwareVersion uint32, certificationType string) (ComplianceInfo, error) {
	var resp struct {
		ComplianceInfo *ComplianceInfo `json:"complianceInfo"`
	}
	path := fmt.Sprintf("/dcl/compliance/compliance-info/%d/%d/%d/%s", vid, pid, softwareVersion, encodeURIComponent(certificationType))
	if err := c.fetchJSON(ctx, path, &resp); err != nil {
		return ComplianceInfo{}, err
	}
	info := resp.ComplianceInfo
	if info == nil || info.VID != vid || info.PID != pid || info.SoftwareVersion != softwareVersion || info.CertificationType != certificationType {
		return ComplianceInfo{}, dclErrorf("Compliance info not found for VID: %d, PID: %d, Software Version: %d, Certification Type: %s", vid, pid, softwareVersion, certificationType)
	}
	// Schema versions 0 and 1 are the ones Core §11.23.10.15 defines.
	if info.SchemaVersion != 0 && info.SchemaVersion != 1 {
		return ComplianceInfo{}, dclErrorf("Unsupported DCL compliance info schema version %d for VID: %d, PID: %d, Software Version: %d", info.SchemaVersion, vid, pid, softwareVersion)
	}
	return *info, nil
}

// Vendors lists every vendor record — DclClient.fetchAllVendors
// (DclClient.ts:262-265): GET /dcl/vendorinfo/vendors, paginated under
// vendorInfo, the records of schema version 0.
func (c *Client) Vendors(ctx context.Context) ([]Vendor, error) {
	raw, err := fetchPaginated[Vendor](ctx, c, "/dcl/vendorinfo/vendors", "vendorInfo")
	if err != nil {
		return nil, err
	}
	n := len(raw)
	out := slices.DeleteFunc(raw, func(v Vendor) bool { return v.SchemaVersion != 0 })
	c.warnDropped(n-len(out), "vendor")
	return out, nil
}

// RevocationPoints lists every revocation distribution point — DclClient.
// fetchRevocationDistributionPoints (DclClient.ts:271-280): GET
// /dcl/pki/revocation-points, paginated under PkiRevocationDistributionPoint,
// the records of schema version 0.
func (c *Client) RevocationPoints(ctx context.Context) ([]RevocationPoint, error) {
	raw, err := fetchPaginated[rawRevocationPoint](ctx, c, "/dcl/pki/revocation-points", "PkiRevocationDistributionPoint")
	if err != nil {
		return nil, err
	}
	return c.supportedPoints(raw), nil
}

// RevocationPointsByIssuer lists the revocation distribution points of the
// issuer with key identifier issuerSKID — DclClient.
// fetchRevocationDistributionPointsByIssuer (DclClient.ts:285-298): GET
// /dcl/pki/revocation-points/{issuerSubjectKeyId}; an answer without the
// record is no point, one of another schema version an error.
func (c *Client) RevocationPointsByIssuer(ctx context.Context, issuerSKID string) ([]RevocationPoint, error) {
	var resp struct {
		ByIssuer *struct {
			Points        []rawRevocationPoint `json:"points"`
			SchemaVersion int                  `json:"schemaVersion"`
		} `json:"pkiRevocationDistributionPointsByIssuerSubjectKeyID"`
	}
	if err := c.fetchJSON(ctx, "/dcl/pki/revocation-points/"+encodeURIComponent(issuerSKID), &resp); err != nil {
		return nil, err
	}
	if resp.ByIssuer == nil {
		return nil, nil
	}
	if resp.ByIssuer.SchemaVersion != 0 {
		return nil, dclErrorf("Unsupported DCL revocation points schema version %d for issuer %s", resp.ByIssuer.SchemaVersion, issuerSKID)
	}
	return c.supportedPoints(resp.ByIssuer.Points), nil
}

// warnDropped is supportedRecords' warning (DclClient.ts:305-313).
func (c *Client) warnDropped(n int, kind string) {
	if n > 0 {
		c.log().Warn("dcl.unsupported_schema_version", slog.String("kind", kind), slog.Int("ignored", n),
			slog.String("message", fmt.Sprintf("Ignoring %d DCL %s record(s) with unsupported schema version", n, kind)))
	}
}

// supportedCertificates is supportedRecords for certificates: those of
// schema version 0, a later version may change what a field means.
func (c *Client) supportedCertificates(in []Certificate) []Certificate {
	out := slices.DeleteFunc(slices.Clone(in), func(x Certificate) bool { return x.SchemaVersion != 0 })
	c.warnDropped(len(in)-len(out), "certificate")
	return out
}

func (c *Client) supportedPoints(in []rawRevocationPoint) []RevocationPoint {
	out := make([]RevocationPoint, 0, len(in))
	for i := range in {
		if in[i].SchemaVersion == 0 {
			out = append(out, in[i].mapped())
		}
	}
	c.warnDropped(len(in)-len(out), "revocation point")
	return out
}
