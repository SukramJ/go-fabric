// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclClientRevocationTest.ts at
// 862ad75f: the DCL client's revocation distribution points, listed and by
// issuer, their field mapping and schema versions.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// dclRevTransport is MockFetch: a response per URL fragment, the most
// recently added matching first, and the log of requested URLs. Unmatched
// requests answer 404.
type dclRevTransport struct {
	mu     sync.Mutex
	routes []dclRevRoute
	calls  []string
}

type dclRevRoute struct {
	part string
	body any
}

func (d *dclRevTransport) add(part string, body any) {
	d.mu.Lock()
	d.routes = append(d.routes, dclRevRoute{part, body})
	d.mu.Unlock()
}

func (d *dclRevTransport) log() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
}

func (d *dclRevTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	url := r.URL.String()
	d.mu.Lock()
	d.calls = append(d.calls, url)
	status, body := http.StatusNotFound, any(map[string]any{"code": 5, "message": "not found", "details": []any{}})
	for i := len(d.routes) - 1; i >= 0; i-- {
		if strings.Contains(url, d.routes[i].part) {
			status, body = http.StatusOK, d.routes[i].body
			break
		}
	}
	d.mu.Unlock()
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(b))), Request: r,
	}, nil
}

func dclRevClient(baseURL string, tr *dclRevTransport) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: &http.Client{Transport: tr}}
}

// dclRevPoint is one raw DCL revocation point (DclClientRevocationTest.ts:14-28).
func dclRevPoint(vid, pid uint16, label, delegator, cert, akid, dataURL, size, digest string, digestType, schema int) map[string]any {
	return map[string]any{
		"vid": vid, "pid": pid, "isPAA": false, "label": label, "crlSignerDelegator": delegator,
		"crlSignerCertificate": cert, "issuerSubjectKeyID": akid, "dataURL": dataURL, "dataFileSize": size,
		"dataDigest": digest, "dataDigestType": digestType, "revocationType": 1, "schemaVersion": schema,
	}
}

const (
	dclRevAKID1 = "A303136D54A84BE24C4887B341066DC270962F99"
	dclRevAKID2 = "E43183AE7B375D84EAB325F45F9E6D037D43DD00"
)

// mockRevocationPointsPage1 (:12-49).
func dclRevPage1() []any {
	return []any{
		dclRevPoint(0xFFF1, 0x8000, "label1", "", "-----BEGIN CERTIFICATE-----\nMIIBvTCCAWOgA\n-----END CERTIFICATE-----", dclRevAKID1, "https://example.com/crl1.der", "0", "", 0, 0),
		dclRevPoint(4701, 22, "label2", "", "-----BEGIN CERTIFICATE-----\nMIIBxTCCAW2gA\n-----END CERTIFICATE-----", dclRevAKID2, "https://example.com/crl2.der", "0", "", 0, 0),
	}
}

// mockRevocationPointsByIssuer's points (:96-133).
func dclRevByIssuerPoints() []any {
	return []any{
		dclRevPoint(0xFFF1, 0x8000, "label1", "", "-----BEGIN CERTIFICATE-----\nMIIBvTCCAWOgA\n-----END CERTIFICATE-----", dclRevAKID1, "https://example.com/crl1.der", "0", "", 0, 0),
		dclRevPoint(0xFFF1, 0x8001, "label2", "-----BEGIN CERTIFICATE-----\ndelegator\n-----END CERTIFICATE-----",
			"-----BEGIN CERTIFICATE-----\nMIIBxTCCAW2gA\n-----END CERTIFICATE-----", dclRevAKID1, "https://example.com/crl2.der", "1234", "abc123==", 1, 0),
	}
}

func dclRevByIssuer(akid string, points []any, schema int) map[string]any {
	return map[string]any{"pkiRevocationDistributionPointsByIssuerSubjectKeyID": map[string]any{
		"issuerSubjectKeyID": akid, "points": points, "schemaVersion": schema,
	}}
}

func dclRevLabels(points []RevocationPoint) []string {
	out := make([]string, len(points))
	for i := range points {
		out[i] = points[i].Label
	}
	return out
}

// TestDclClientRevocationFetchRevocationDistributionPoints is the describes
// "fetchRevocationDistributionPoints" and "… schema versions" (:146-263),
// through Client.RevocationPoints.
func TestDclClientRevocationFetchRevocationDistributionPoints(t *testing.T) {
	ctx := context.Background()
	list := func(t *testing.T, routes ...dclRevRoute) []RevocationPoint {
		t.Helper()
		tr := &dclRevTransport{}
		for _, r := range routes {
			tr.add(r.part, r.body)
		}
		points, err := dclRevClient(ProductionURL, tr).RevocationPoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return points
	}
	page := func(points []any, pagination map[string]any) map[string]any {
		return map[string]any{"PkiRevocationDistributionPoint": points, "pagination": pagination}
	}
	page1 := page(dclRevPage1(), map[string]any{"next_key": nil, "total": "2"})

	t.Run("fetches all revocation points", func(t *testing.T) {
		points := list(t, dclRevRoute{"/dcl/pki/revocation-points", page1})
		if len(points) != 2 {
			t.Fatalf("%d points", len(points))
		}
		p := points[0]
		if p.IssuerSubjectKeyID != dclRevAKID1 || p.DataURL != "https://example.com/crl1.der" || p.VID != 0xFFF1 || p.PID != 0x8000 ||
			p.IsPAA || p.Label != "label1" || p.RevocationType != 1 || p.SchemaVersion != 0 {
			t.Fatalf("first point %+v", p)
		}
		if points[1].IssuerSubjectKeyID != dclRevAKID2 || points[1].VID != 4701 {
			t.Fatalf("second point %+v", points[1])
		}
	})

	// The Go point is a struct whose fields carry the mapped names; the raw
	// issuerSubjectKeyID and dataURL arrive in IssuerSubjectKeyID and DataURL.
	t.Run("maps DCL field names correctly (issuerSubjectKeyID -> issuerSubjectKeyId, dataURL -> dataUrl)", func(t *testing.T) {
		points := list(t, dclRevRoute{"/dcl/pki/revocation-points", page1})
		if points[0].IssuerSubjectKeyID != dclRevAKID1 || points[0].DataURL != "https://example.com/crl1.der" {
			t.Fatalf("point %+v", points[0])
		}
	})

	t.Run("handles paginated responses", func(t *testing.T) {
		first := dclRevPage1()[:1]
		first[0].(map[string]any)["label"] = "page1-label"
		second := dclRevPage1()[1:]
		second[0].(map[string]any)["label"] = "page2-label"
		points := list(t,
			dclRevRoute{"/dcl/pki/revocation-points", page(first, map[string]any{"next_key": "page2key", "total": "2"})},
			dclRevRoute{"pagination.key=page2key", page(second, map[string]any{})})
		if got := dclRevLabels(points); !reflect.DeepEqual(got, []string{"page1-label", "page2-label"}) {
			t.Fatalf("labels %v", got)
		}
	})

	t.Run("returns empty array when no revocation points exist", func(t *testing.T) {
		if points := list(t, dclRevRoute{"/dcl/pki/revocation-points", page([]any{}, map[string]any{})}); len(points) != 0 {
			t.Fatalf("%d points", len(points))
		}
	})

	t.Run("maps optional fields correctly", func(t *testing.T) {
		raw := dclRevPoint(0xFFF1, 0, "paa-label", "", "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----", "AABBCCDD",
			"https://example.com/crl.der", "5678", "digest==", 1, 0)
		raw["isPAA"] = true
		points := list(t, dclRevRoute{"/dcl/pki/revocation-points", page([]any{raw}, map[string]any{})})
		if len(points) != 1 {
			t.Fatalf("%d points", len(points))
		}
		// pid 0 and an empty delegator are absent: 0 and "" in Go.
		p := points[0]
		if !p.IsPAA || p.PID != 0 || p.CRLSignerDelegator != "" || p.DataFileSize != 5678 || p.DataDigest != "digest==" || p.DataDigestType != 1 {
			t.Fatalf("point %+v", p)
		}
	})

	t.Run("ignores revocation points with an unsupported schema version", func(t *testing.T) {
		pts := dclRevPage1()
		pts[1].(map[string]any)["schemaVersion"] = 1
		points := list(t, dclRevRoute{"/dcl/pki/revocation-points", page(pts, map[string]any{})})
		if got := dclRevLabels(points); !reflect.DeepEqual(got, []string{"label1"}) {
			t.Fatalf("labels %v", got)
		}
	})
}

// TestDclClientRevocationFetchRevocationDistributionPointsByIssuer is the
// describe "fetchRevocationDistributionPointsByIssuer" (:265-396), through
// Client.RevocationPointsByIssuer.
func TestDclClientRevocationFetchRevocationDistributionPointsByIssuer(t *testing.T) {
	ctx := context.Background()
	const path1 = "/dcl/pki/revocation-points/" + dclRevAKID1

	t.Run("fetches revocation points for a specific issuer", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add(path1, dclRevByIssuer(dclRevAKID1, dclRevByIssuerPoints(), 0))
		points, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, dclRevAKID1)
		if err != nil || len(points) != 2 {
			t.Fatalf("%+v (%v)", points, err)
		}
		if points[0].IssuerSubjectKeyID != dclRevAKID1 || points[0].VID != 0xFFF1 || points[0].PID != 0x8000 || points[1].PID != 0x8001 {
			t.Fatalf("points %+v", points)
		}
	})

	t.Run("maps crlSignerDelegator and dataFileSize from by-issuer response", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add(path1, dclRevByIssuer(dclRevAKID1, dclRevByIssuerPoints(), 0))
		points, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, dclRevAKID1)
		if err != nil || len(points) != 2 {
			t.Fatalf("%+v (%v)", points, err)
		}
		if points[0].CRLSignerDelegator != "" || points[1].CRLSignerDelegator != "-----BEGIN CERTIFICATE-----\ndelegator\n-----END CERTIFICATE-----" {
			t.Fatalf("delegators %q %q", points[0].CRLSignerDelegator, points[1].CRLSignerDelegator)
		}
		if points[1].DataFileSize != 1234 {
			t.Fatalf("size %d", points[1].DataFileSize)
		}
	})

	t.Run("ignores revocation points with an unsupported schema version", func(t *testing.T) {
		pts := dclRevByIssuerPoints()
		pts[1].(map[string]any)["schemaVersion"] = 1
		tr := &dclRevTransport{}
		tr.add(path1, dclRevByIssuer(dclRevAKID1, pts, 0))
		points, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, dclRevAKID1)
		if got := dclRevLabels(points); err != nil || !reflect.DeepEqual(got, []string{"label1"}) {
			t.Fatalf("labels %v (%v)", got, err)
		}
	})

	t.Run("rejects a response with an unsupported schema version", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add(path1, dclRevByIssuer(dclRevAKID1, dclRevByIssuerPoints(), 1))
		_, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, dclRevAKID1)
		var dclErr *Error
		if !errors.As(err, &dclErr) || !strings.Contains(err.Error(), "Unsupported DCL revocation points schema version") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("returns empty array when issuer has no revocation points", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add("/dcl/pki/revocation-points/NONEXISTENT", dclRevByIssuer("NONEXISTENT", []any{}, 0))
		points, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, "NONEXISTENT")
		if err != nil || len(points) != 0 {
			t.Fatalf("%+v (%v)", points, err)
		}
	})

	t.Run("encodes issuer subject key ID in URL path", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add("/dcl/pki/revocation-points/AA%3ABB%3ACC", dclRevByIssuer("AA:BB:CC", []any{}, 0))
		points, err := dclRevClient(ProductionURL, tr).RevocationPointsByIssuer(ctx, "AA:BB:CC")
		if err != nil || len(points) != 0 {
			t.Fatalf("%+v (%v)", points, err)
		}
		if calls := tr.log(); len(calls) != 1 || !strings.Contains(calls[0], "AA%3ABB%3ACC") {
			t.Fatalf("calls %v", calls)
		}
	})

	t.Run("uses test-net URL when production is false", func(t *testing.T) {
		tr := &dclRevTransport{}
		tr.add("test-net.dcl.csa-iot.org/dcl/pki/revocation-points/AABB", dclRevByIssuer("AABB", []any{}, 0))
		points, err := dclRevClient(TestNetURL, tr).RevocationPointsByIssuer(ctx, "AABB")
		if err != nil || len(points) != 0 {
			t.Fatalf("%+v (%v)", points, err)
		}
		if calls := tr.log(); len(calls) == 0 || !strings.Contains(calls[0], "test-net.dcl.csa-iot.org") {
			t.Fatalf("calls %v", calls)
		}
	})
}
