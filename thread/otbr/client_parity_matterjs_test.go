// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/thread"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// The cases of matter.js packages/thread-br-client/test/OtbrRestClientTest.ts
// at 862ad75f, ported by meaning. matter.js stubs fetch (installFetch,
// :28-41) and routes it (MockOtbrServer, :51-94); here an http.Client
// whose transport is the stub does the same, so the host "br.example" is
// never resolved. Left out: the cases of the mutations the client does not
// wrap (deletePendingDataset, factoryReset ×2, setCommissionerState,
// addJoiner ×2, removeJoiner) and of the collection API's actions
// (postAction ×2, getAction ×2, clearDiagnostics) — package doc, ADR 0027
// decision 4.

// stubRequest is one request the stub saw (RecordedRequest, :43-49).
type stubRequest struct {
	method, path, url, contentType, body string
	hasBody                              bool
}

// stub is MockOtbrServer: routes by method and path to canned responses,
// recording each request; an unrouted request is 501.
type stub struct {
	mu       sync.Mutex
	routes   map[string]func(*http.Request) (*http.Response, error)
	requests []stubRequest
}

func newStub() *stub {
	return &stub{routes: map[string]func(*http.Request) (*http.Response, error){}}
}

func (s *stub) on(method, path string, status int, body string) *stub {
	s.routes[method+" "+path] = func(*http.Request) (*http.Response, error) { return respond(status, body), nil }
	return s
}

func respond(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func (s *stub) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	s.mu.Lock()
	s.requests = append(s.requests, stubRequest{
		method: req.Method, path: req.URL.Path, url: req.URL.String(),
		contentType: req.Header.Get("Content-Type"), body: string(body), hasBody: req.Body != nil,
	})
	route, ok := s.routes[req.Method+" "+req.URL.Path]
	s.mu.Unlock()
	if !ok {
		return respond(http.StatusNotImplemented, "no route"), nil
	}
	return route(req)
}

func (s *stub) last(t *testing.T) stubRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("no request recorded")
	}
	return s.requests[len(s.requests)-1]
}

func (s *stub) client(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(Config{Host: "br.example", HTTPClient: &http.Client{Transport: s}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// roundTripFunc is a transport of one function (installFetch).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func expectCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error %v is no *otbr.Error", err)
	}
	if e.Code != code {
		t.Errorf("code %s, want %s (%v)", e.Code, code, err)
	}
	return e
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// :118-138.
func TestOtbrRestClientGetNodePascal(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodePascal)
	node, err := s.client(t).Node(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.last(t).url != "http://br.example:8081/node" {
		t.Errorf("url %s", s.last(t).url)
	}
	if node.NetworkName != "TestNet" || node.State != "router" || node.NumOfRouter != 5 || node.Rloc16 != 29696 ||
		hexOf(node.ExtPANID) != "1122334455667788" || hexOf(node.ExtAddress) != "0011223344556601" ||
		hexOf(node.BaID) != "00112233445566778899aabbccddeeff" || node.LeaderData.PartitionID != 305419896 ||
		node.LeaderData.LeaderRouterID != 61 {
		t.Errorf("node %+v", node)
	}
}

// :140-160.
func TestOtbrRestClientGetNodeCamel(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, otbrtest.NodeCamel)
	node, err := s.client(t).Node(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if node.NetworkName != "MockThread" || node.State != "router" || node.NumOfRouter != 7 || node.Rloc16 != 0x4800 ||
		hexOf(node.ExtPANID) != "1122334455667799" || hexOf(node.ExtAddress) != "0011223344556602" ||
		hexOf(node.BaID) != "0011223344556677889900aabbccddee" || node.LeaderData.LeaderRouterID != 5 {
		t.Errorf("node %+v", node)
	}
}

// camelWith is the camel /node fixture with key set to v (nil deletes it).
func camelWith(t *testing.T, key string, v any) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(otbrtest.NodeCamel), &payload); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		delete(payload, key)
	} else {
		payload[key] = v
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

// :161-173: nine malformed or out-of-range rloc16 values.
func TestOtbrRestClientGetNodeRejectsBadRloc16(t *testing.T) {
	t.Parallel()
	for _, bad := range []any{"", "-5", "1e2", "0x10000", "99999", "0xzzzz", -1, 0x10000, 1.5} {
		s := newStub().on("GET", "/node", 200, camelWith(t, "rloc16", bad))
		_, err := s.client(t).Node(context.Background())
		expectCode(t, err, CodeProtocol)
	}
}

// :175-187.
func TestOtbrRestClientGetNodeWithoutRouterCount(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 200, camelWith(t, "routerCount", nil))
	node, err := s.client(t).Node(context.Background())
	if err != nil || node.NumOfRouter != -1 || node.NetworkName != "MockThread" {
		t.Errorf("node %+v, %v", node, err)
	}
}

// :189-207.
func TestOtbrRestClientGetDiagnostics(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/diagnostics", 200, otbrtest.Diagnostics)
	list, err := s.client(t).Diagnostics(context.Background())
	if err != nil || len(list) != 5 {
		t.Fatalf("diagnostics %d, %v", len(list), err)
	}
	first := list[0].(map[string]any)
	for _, key := range []string{"extAddress", "ip6AddressList", "macCounters"} {
		if _, ok := first[key]; !ok {
			t.Errorf("key %s not normalized", key)
		}
	}
	if s.last(t).url != "http://br.example:8081/diagnostics" {
		t.Errorf("url %s", s.last(t).url)
	}
}

// :209-243.
func TestOtbrRestClientGetDataset(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node/dataset/active", 200, otbrtest.DatasetActiveHex).on("GET", "/node/dataset/pending", 204, "")
	ds, err := s.client(t).Datasets(context.Background())
	if err != nil || ds.Active != otbrtest.DatasetActiveHex || ds.Pending != "" {
		t.Errorf("datasets %+v, %v", ds, err)
	}
	s = newStub().on("GET", "/node/dataset/active", 200, "AABB").on("GET", "/node/dataset/pending", 204, "")
	ds, err = s.client(t).Datasets(context.Background())
	if err != nil || ds.Active != "AABB" || ds.Pending != "" {
		t.Errorf("datasets %+v, %v", ds, err)
	}
}

// :245-262.
func TestOtbrRestClientUnreachable(t *testing.T) {
	t.Parallel()
	c, _ := NewClient(Config{Host: "br.example", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fetch failed")
	})}})
	_, err := c.Node(context.Background())
	expectCode(t, err, CodeUnreachable)
}

// :264-280.
func TestOtbrRestClientNon2xx(t *testing.T) {
	t.Parallel()
	s := newStub().on("GET", "/node", 500, "nope")
	_, err := s.client(t).Node(context.Background())
	if e := expectCode(t, err, CodeProtocol); e.HTTPStatus != 500 {
		t.Errorf("httpStatus %d", e.HTTPStatus)
	}
}

// :282-308: a border router that never answers is unreachable once the
// timeout passes.
func TestOtbrRestClientTimeout(t *testing.T) {
	t.Parallel()
	c, _ := NewClient(Config{Host: "br.example", Timeout: 20 * time.Millisecond, HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}})
	_, err := c.Node(context.Background())
	expectCode(t, err, CodeUnreachable)
}

// :310-324.
func TestOtbrRestClientBracketsIPv6(t *testing.T) {
	t.Parallel()
	var seen string
	c, _ := NewClient(Config{Host: "fd00::1", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.URL.String()
		return respond(200, otbrtest.NodePascal), nil
	})}})
	if _, err := c.Node(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen != "http://[fd00::1]:8081/node" {
		t.Errorf("url %s", seen)
	}
	if c.BaseURL() != "http://[fd00::1]:8081" {
		t.Errorf("BaseURL %s", c.BaseURL())
	}
}

// :326-447, the read-only getters.
func TestOtbrRestClientReadOnlyGetters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStub().
		on("GET", "/node/state", 200, `"router"`).
		on("GET", "/node/network-name", 200, `"TestNet"`).
		on("GET", "/node/rloc", 200, `"fd00:1122:3344:5566:0:ff:fe00:7400"`).
		on("GET", "/node/rloc16", 200, `29696`).
		on("GET", "/node/num-of-router", 200, `5`).
		on("GET", "/node/ext-address", 200, `"0011223344556601"`).
		on("GET", "/node/ext-panid", 200, `"1122334455667788"`).
		on("GET", "/node/ba-id", 200, `"00112233445566778899AABBCCDDEEFF"`).
		on("GET", "/node/leader-data", 200, otbrtest.LeaderData).
		on("GET", "/node/dataset/active", 200, otbrtest.DatasetActiveHex).
		on("GET", "/node/dataset/pending", 204, "").
		on("GET", "/node/coprocessor/version", 404, "").
		on("GET", "/node/commissioner/state", 404, "").
		on("GET", "/node/commissioner/joiner", 404, "")
	c := s.client(t)
	if v, err := c.State(ctx); err != nil || v != thread.RoleRouter {
		t.Errorf("State %q, %v", v, err)
	}
	if v, err := c.NetworkName(ctx); err != nil || v != "TestNet" {
		t.Errorf("NetworkName %q, %v", v, err)
	}
	if v, err := c.Rloc(ctx); err != nil || v != "fd00:1122:3344:5566:0:ff:fe00:7400" {
		t.Errorf("Rloc %q, %v", v, err)
	}
	if v, err := c.Rloc16(ctx); err != nil || v != 29696 {
		t.Errorf("Rloc16 %d, %v", v, err)
	}
	if v, err := c.NumOfRouter(ctx); err != nil || v != 5 {
		t.Errorf("NumOfRouter %d, %v", v, err)
	}
	if v, err := c.ExtAddress(ctx); err != nil || hexOf(v) != "0011223344556601" {
		t.Errorf("ExtAddress %x, %v", v, err)
	}
	if v, err := c.ExtPANID(ctx); err != nil || hexOf(v) != "1122334455667788" {
		t.Errorf("ExtPANID %x, %v", v, err)
	}
	if v, err := c.BorderAgentID(ctx); err != nil || hexOf(v) != "00112233445566778899aabbccddeeff" {
		t.Errorf("BorderAgentID %x, %v", v, err)
	}
	if v, err := c.LeaderData(ctx); err != nil || v != (LeaderData{PartitionID: 305419896, Weighting: 64, DataVersion: 147, StableDataVersion: 205, LeaderRouterID: 61}) {
		t.Errorf("LeaderData %+v, %v", v, err)
	}
	ds, err := c.ActiveDataset(ctx)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := thread.DecodeDataset(ds)
	if err != nil || decoded.NetworkName == nil || *decoded.NetworkName != "MockThread" {
		t.Errorf("ActiveDataset %x, %v", ds, err)
	}
	if v, err := c.PendingDataset(ctx); err != nil || v != nil {
		t.Errorf("PendingDataset on 204 = %x, %v", v, err)
	}
	_, err = c.CoprocessorVersion(ctx)
	expectCode(t, err, CodeUnsupported)
	_, err = c.CommissionerState(ctx)
	expectCode(t, err, CodeUnsupported)
	_, err = c.Joiners(ctx)
	expectCode(t, err, CodeUnsupported)
}

// :397-415 and :432-446: the dataset's 204 and empty body, an undecodable
// one, the normalized joiners, a state that is not a string.
func TestOtbrRestClientGetterEdges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for body, want := range map[string]Code{"": "", "nothex!!": CodeProtocol} {
		s := newStub().on("GET", "/node/dataset/active", 200, body)
		ds, err := s.client(t).ActiveDataset(ctx)
		if want == "" {
			if err != nil || ds != nil {
				t.Errorf("an empty body = %x, %v", ds, err)
			}
			continue
		}
		expectCode(t, err, want)
	}
	s := newStub().on("GET", "/node/dataset/active", 204, "")
	if ds, err := s.client(t).ActiveDataset(ctx); err != nil || ds != nil {
		t.Errorf("204 = %x, %v", ds, err)
	}
	s = newStub().on("GET", "/node/commissioner/joiner", 200, `[{"Pskd":"J01NME","Eui64":"*","Timeout":120}]`)
	joiners, err := s.client(t).Joiners(ctx)
	if err != nil || len(joiners) != 1 {
		t.Fatalf("joiners %v, %v", joiners, err)
	}
	if _, ok := joiners[0].(map[string]any)["pskd"]; !ok {
		t.Error("joiner keys not normalized")
	}
	s = newStub().on("GET", "/node/state", 200, "123")
	_, err = s.client(t).State(ctx)
	expectCode(t, err, CodeProtocol)
}

// :449-581, the mutations the client wraps.
func TestOtbrRestClientMutations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStub().on("PUT", "/node/state", 200, "")
	c := s.client(t)
	if err := c.setState(ctx, true); err != nil {
		t.Fatal(err)
	}
	if r := s.last(t); r.method != "PUT" || r.path != "/node/state" || r.contentType != "application/json" || r.body != `"enable"` {
		t.Errorf("setState(true) %+v", r)
	}
	_ = c.setState(ctx, false)
	if s.last(t).body != `"disable"` {
		t.Errorf("setState(false) %+v", s.last(t))
	}
	// :479-497: the dataset as text/plain hex, the encoded TLVs of a
	// decoded dataset equal to the source.
	s.on("PUT", "/node/dataset/active", 200, "")
	if err := c.setDataset(ctx, false, []byte{0xaa, 0xbb, 0xcc}); err != nil {
		t.Fatal(err)
	}
	if r := s.last(t); r.path != "/node/dataset/active" || r.contentType != "text/plain" || !strings.EqualFold(r.body, "AABBCC") {
		t.Errorf("setDataset %+v", r)
	}
	ds, err := thread.DecodeDatasetHex(otbrtest.DatasetActiveHex)
	if err != nil {
		t.Fatal(err)
	}
	tlvs, _ := ds.Encode()
	_ = c.setDataset(ctx, false, tlvs)
	if !strings.EqualFold(s.last(t).body, strings.TrimSpace(otbrtest.DatasetActiveHex)) {
		t.Errorf("encoded dataset %s", s.last(t).body)
	}
	s.on("PUT", "/node/dataset/pending", 200, "")
	_ = c.setDataset(ctx, true, []byte{0xaa, 0xbb})
	if s.last(t).path != "/node/dataset/pending" {
		t.Errorf("pending %+v", s.last(t))
	}
	s.on("DELETE", "/node/dataset/active", 204, "")
	if err := c.deleteActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if r := s.last(t); r.method != "DELETE" || r.path != "/node/dataset/active" || r.hasBody {
		t.Errorf("delete %+v", r)
	}
	s.on("PUT", "/node/dataset/active", 409, "").on("PUT", "/node/state", 409, "")
	expectCode(t, c.setDataset(ctx, false, []byte{0xaa}), CodeConflict)
	expectCode(t, c.setState(ctx, true), CodeConflict)
}

// :661-690, the collection API's reads.
func TestOtbrRestClientCollectionReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newStub().on("GET", "/api/diagnostics", 200, `[{"rloc16":"0x4800"},{"rloc16":"0x0400"}]`).
		on("GET", "/api/devices", 200, `[{"extAddress":"0011223344556677"},{"extAddress":"8899aabbccddeeff"}]`)
	c := s.client(t)
	if l, err := c.DiagnosticsCollection(ctx); err != nil || len(l) != 2 || s.last(t).path != "/api/diagnostics" {
		t.Errorf("collection %v, %v", l, err)
	}
	if l, err := c.Devices(ctx); err != nil || len(l) != 2 || s.last(t).path != "/api/devices" {
		t.Errorf("devices %v, %v", l, err)
	}
	s.on("GET", "/api/diagnostics", 200, `{"data":[{"rloc16":"0x4800"}],"meta":{"collection":{"total":1}}}`)
	if l, err := c.DiagnosticsCollection(ctx); err != nil || len(l) != 1 {
		t.Errorf("envelope %v, %v", l, err)
	}
	s.on("GET", "/api/diagnostics", 200, `{"foo":1}`)
	_, err := c.DiagnosticsCollection(ctx)
	expectCode(t, err, CodeProtocol)
}

// :701-729.
func TestOtbrRestClientDetectDiagnosticsAPI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		routes func(*stub)
		want   DiagnosticsAPI
	}{
		{func(s *stub) { s.on("GET", "/api/diagnostics", 200, "[]") }, DiagnosticsAPICollection},
		{func(s *stub) { s.on("GET", "/api/diagnostics", 404, "").on("GET", "/diagnostics", 200, "[]") }, DiagnosticsAPILegacy},
		{func(s *stub) { s.on("GET", "/api/diagnostics", 404, "").on("GET", "/diagnostics", 404, "") }, DiagnosticsAPINone},
	} {
		s := newStub()
		c.routes(s)
		if got, err := s.client(t).DetectDiagnosticsAPI(ctx); err != nil || got != c.want {
			t.Errorf("DetectDiagnosticsAPI = %s, %v, want %s", got, err, c.want)
		}
	}
}

// The client's own edges: no host, a body that is not JSON, a /node that
// is not an object, a missing leaderData or field, a short ba-id, a
// rloc16 out of range, a wrong-shaped diagnostics and joiners body, and a
// number that is not one.
func TestOtbrRestClientShapeErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := NewClient(Config{}); err == nil {
		t.Error("a client without a host")
	}
	cases := []struct {
		path, body string
		call       func(*Client) error
	}{
		{"/node", "{", func(c *Client) error { _, err := c.Node(ctx); return err }},
		{"/node", "[]", func(c *Client) error { _, err := c.Node(ctx); return err }},
		{"/node", `{"state":"router"}`, func(c *Client) error { _, err := c.Node(ctx); return err }},
		{"/node", camelWithLeader(t, map[string]any{"partitionId": -1}), func(c *Client) error { _, err := c.Node(ctx); return err }},
		{"/node", camelWithBadField(t), func(c *Client) error { _, err := c.Node(ctx); return err }},
		{"/node/ba-id", `"0011"`, func(c *Client) error { _, err := c.BorderAgentID(ctx); return err }},
		{"/node/rloc16", `70000`, func(c *Client) error { _, err := c.Rloc16(ctx); return err }},
		{"/node/rloc16", `"x"`, func(c *Client) error { _, err := c.Rloc16(ctx); return err }},
		{"/diagnostics", `{}`, func(c *Client) error { _, err := c.Diagnostics(ctx); return err }},
		{"/node/commissioner/joiner", `{}`, func(c *Client) error { _, err := c.Joiners(ctx); return err }},
		{"/node/leader-data", `[]`, func(c *Client) error { _, err := c.LeaderData(ctx); return err }},
		{"/api/devices", `{}`, func(c *Client) error { _, err := c.Devices(ctx); return err }},
		{"/node/ext-address", `"zz"`, func(c *Client) error { _, err := c.ExtAddress(ctx); return err }},
		{"/node/ext-address", `"001"`, func(c *Client) error { _, err := c.ExtAddress(ctx); return err }},
		{"/node", `{"networkName":"x"}`, func(c *Client) error { _, _, _, err := c.ProbeNode(ctx); return err }},
		{"/node", `{"networkName":"x","extPanId":"00"}`, func(c *Client) error { _, _, _, err := c.ProbeNode(ctx); return err }},
		{"/node", `{"extPanId":"0011223344556677"}`, func(c *Client) error { _, _, _, err := c.ProbeNode(ctx); return err }},
	}
	for _, tc := range cases {
		s := newStub().on("GET", tc.path, 200, tc.body)
		if e := expectCode(t, tc.call(s.client(t)), CodeProtocol); e.Error() == "" {
			t.Error("empty message")
		}
	}
	for _, path := range []string{"/node/dataset/active", "/node/dataset/pending", "/diagnostics", "/api/diagnostics", "/api/devices", "/node/commissioner/joiner", "/node/leader-data", "/node/num-of-router"} {
		s := newStub().on("GET", path, 405, "")
		c := s.client(t)
		var err error
		switch path {
		case "/node/dataset/active":
			_, err = c.Datasets(ctx)
		case "/node/dataset/pending":
			s.on("GET", "/node/dataset/active", 204, "")
			_, err = c.Datasets(ctx)
		case "/diagnostics":
			_, err = c.Diagnostics(ctx)
		case "/api/diagnostics":
			_, err = c.DiagnosticsCollection(ctx)
		case "/api/devices":
			_, err = c.Devices(ctx)
		case "/node/commissioner/joiner":
			_, err = c.Joiners(ctx)
		case "/node/leader-data":
			_, err = c.LeaderData(ctx)
		case "/node/num-of-router":
			_, err = c.NumOfRouter(ctx)
		}
		expectCode(t, err, CodeNotAllowed)
	}
	down, _ := NewClient(Config{Host: "br.example", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("down")
	})}})
	if _, err := down.DetectDiagnosticsAPI(ctx); err == nil {
		t.Error("DetectDiagnosticsAPI of an unreachable border router")
	}
	expectCode(t, down.setState(ctx, true), CodeUnreachable)
	if _, err := down.Datasets(ctx); err == nil {
		t.Error("Datasets of an unreachable border router")
	}
	if e := (&Error{Code: CodeProtocol, Message: "m", Cause: io.EOF}); !errors.Is(e, io.EOF) || !strings.Contains(e.Error(), "EOF") {
		t.Errorf("Error %v", e)
	}
	failing, _ := NewClient(Config{Host: "br.example", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errReader{}), Header: http.Header{}}, nil
	})}})
	_, err := failing.Node(ctx)
	expectCode(t, err, CodeUnreachable)
	bad, _ := NewClient(Config{Host: "br example\x7f"})
	_, err = bad.Node(ctx)
	expectCode(t, err, CodeUnreachable)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stalled") }

func camelWithLeader(t *testing.T, leader map[string]any) string {
	t.Helper()
	return camelWith(t, "leaderData", leader)
}

func camelWithBadField(t *testing.T) string {
	t.Helper()
	return camelWith(t, "extAddress", "001122")
}

// The unexported decode helpers: trailing data after the JSON value is
// refused, an infinite number is no number, a bool no RLOC16.
func TestOtbrDecodeJSON(t *testing.T) {
	t.Parallel()
	if _, err := decodeJSON([]byte(`1 2`)); err == nil {
		t.Error("two values accepted")
	}
	if _, ok := asNumber(json.Number("1e999")); ok {
		t.Error("an infinite number accepted")
	}
	if _, ok := parseRloc16(true); ok {
		t.Error("a bool rloc16 accepted")
	}
}
