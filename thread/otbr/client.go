// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/thread"
)

// DefaultPort is the OTBR REST listener's port (OtbrRestClient.ts:59).
const DefaultPort = 8081

// DefaultTimeout bounds every request, its body included
// (OtbrRestClient.ts:60, :790-812).
const DefaultTimeout = 5 * time.Second

// Config is a [Client]'s connection: OtbrRestClientOptions
// (OtbrRestClient.ts:53-57).
type Config struct {
	// Host is the border router's host name or address; an IPv6 literal
	// is bracketed (OtbrRestClient.ts:166). Required.
	Host string
	// Port is the REST port; 0 is [DefaultPort].
	Port int
	// Timeout bounds each request; 0 is [DefaultTimeout].
	Timeout time.Duration
	// HTTPClient sends the requests; http.DefaultClient's transport when
	// nil.
	HTTPClient *http.Client
}

// Client is a thin HTTP client for the OTBR REST API, matter.js's
// OtbrRestClient (OtbrRestClient.ts:143-847). Every method fails with an
// [*Error]: [CodeUnreachable] on a network failure or a timeout,
// [CodeProtocol] on a response of the wrong shape, the status's code
// (codeForStatus) on a non-2xx status. A Client is safe for concurrent
// use.
type Client struct {
	baseURL string
	timeout time.Duration
	http    *http.Client
}

// NewClient returns a client of the border router cfg names.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, errors.New("otbr: no host")
	}
	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	host := cfg.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	c := &Client{baseURL: "http://" + host + ":" + strconv.Itoa(port), timeout: cfg.Timeout, http: cfg.HTTPClient}
	if c.timeout == 0 {
		c.timeout = DefaultTimeout
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	return c, nil
}

// BaseURL is "http://host:port" (OtbrRestClient.ts:171-173).
func (c *Client) BaseURL() string { return c.baseURL }

// LeaderData is the /node leader record (OtbrRestClient.ts:13-19).
type LeaderData = thread.LeaderData

// NodeInfo is the /node summary (OtbrRestClient.ts:21-32), binary fields
// as bytes. NumOfRouter is -1 on builds that send neither numOfRouter nor
// its post-2024 alias routerCount.
type NodeInfo struct {
	BaID        []byte
	State       string
	NumOfRouter int
	RlocAddress string
	ExtAddress  []byte
	NetworkName string
	Rloc16      uint16
	LeaderData  LeaderData
	ExtPANID    []byte
}

// response is a fetched body.
type response struct {
	status int
	body   []byte
}

func (r response) ok() bool { return r.status >= 200 && r.status < 300 }

// do sends one request inside the timeout, its body read inside it too —
// a border router that sends the headers and then stalls the body is a
// timeout (OtbrRestClient.ts:790-812, :814-846). A transport failure is
// [CodeUnreachable].
func (c *Client) do(ctx context.Context, method, path, accept, contentType string, body []byte) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return response{}, &Error{Code: CodeUnreachable, Message: fmt.Sprintf("%s %s failed", method, path), Cause: err}
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, &Error{Code: CodeUnreachable, Message: fmt.Sprintf("%s %s failed", method, path), Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	// A misbehaving border router costs bounded memory (W4-ctlint-10);
	// matter.js reads the whole answer (OtbrRestClient.ts:757-788).
	text, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxResponseBody))
	if err != nil {
		return response{}, &Error{Code: CodeUnreachable, Message: fmt.Sprintf("%s %s failed", method, path), Cause: err}
	}
	return response{status: resp.StatusCode, body: text}, nil
}

// maxResponseBody bounds a border router's answer; the REST API's largest,
// the dataset and the diagnostics, are a few KiB.
const maxResponseBody = 16 << 20

// statusError is a non-2xx answer (OtbrRestClient.ts:762-766).
func statusError(method, path string, status int) *Error {
	return &Error{Code: codeForStatus(status), Message: fmt.Sprintf("%s %s returned %d", method, path, status), HTTPStatus: status}
}

// fetchJSON is #fetchJson (OtbrRestClient.ts:757-777): 204 is a nil body
// without a key format; the body parsed, its key format detected on an
// object, its keys normalized.
func (c *Client) fetchJSON(ctx context.Context, path string) (any, KeyFormat, error) {
	r, err := c.do(ctx, http.MethodGet, path, "application/json", "", nil)
	if err != nil {
		return nil, "", err
	}
	if r.status == http.StatusNoContent {
		return nil, "", nil
	}
	if !r.ok() {
		return nil, "", statusError(http.MethodGet, path, r.status)
	}
	parsed, err := decodeJSON(r.body)
	if err != nil {
		return nil, "", &Error{Code: CodeProtocol, Message: fmt.Sprintf("GET %s returned non-JSON body", path), Cause: err}
	}
	var format KeyFormat
	if obj, ok := parsed.(map[string]any); ok {
		format = DetectKeyFormat(obj)
	}
	return NormalizeKeys(parsed), format, nil
}

// fetchText is #fetchText (OtbrRestClient.ts:779-788): false on 204.
func (c *Client) fetchText(ctx context.Context, path string) (text string, ok bool, err error) {
	r, err := c.do(ctx, http.MethodGet, path, "text/plain", "", nil)
	if err != nil {
		return "", false, err
	}
	if r.status == http.StatusNoContent {
		return "", false, nil
	}
	if !r.ok() {
		return "", false, statusError(http.MethodGet, path, r.status)
	}
	return string(r.body), true, nil
}

// mutate is #mutate (OtbrRestClient.ts:814-846): the body drained inside
// the timeout, a non-2xx status its code.
func (c *Client) mutate(ctx context.Context, method, path, contentType string, body []byte) error {
	r, err := c.do(ctx, method, path, "", contentType, body)
	if err != nil {
		return err
	}
	if !r.ok() {
		return statusError(method, path, r.status)
	}
	return nil
}

// record is the normalized JSON object at path, [CodeProtocol] for another
// shape.
func (c *Client) record(ctx context.Context, path string) (map[string]any, error) {
	body, _, err := c.fetchJSON(ctx, path)
	if err != nil {
		return nil, err
	}
	obj, ok := body.(map[string]any)
	if !ok {
		return nil, protocolError("%s did not return a JSON object", path)
	}
	return obj, nil
}

// expectString is a /node field that must be a string.
func expectString(rec map[string]any, key string) (string, error) {
	s, ok := rec[key].(string)
	if !ok {
		return "", protocolError("/node: missing or non-string field %s", key)
	}
	return s, nil
}

func expectUint(rec map[string]any, key, where string, maxValue uint64) (uint64, error) {
	if _, ok := asNumber(rec[key]); !ok {
		return 0, protocolError("%s: missing or non-numeric field %s", where, key)
	}
	n, ok := asUint(rec[key], maxValue)
	if !ok {
		return 0, protocolError("%s: field %s out of range", where, key)
	}
	return n, nil
}

// leaderData reads a leader record (OtbrRestClient.ts:189-196, :338-351).
func leaderData(rec map[string]any, where string) (LeaderData, error) {
	var errs []error
	get := func(key string, maxValue uint64) uint64 {
		n, err := expectUint(rec, key, where, maxValue)
		errs = append(errs, err)
		return n
	}
	ld := LeaderData{
		PartitionID:       uint32(get("partitionId", math.MaxUint32)),     //nolint:gosec // bounded by expectUint
		Weighting:         uint8(get("weighting", math.MaxUint8)),         //nolint:gosec // bounded by expectUint
		DataVersion:       uint8(get("dataVersion", math.MaxUint8)),       //nolint:gosec // bounded by expectUint
		StableDataVersion: uint8(get("stableDataVersion", math.MaxUint8)), //nolint:gosec // bounded by expectUint
		LeaderRouterID:    uint8(get("leaderRouterId", math.MaxUint8)),    //nolint:gosec // bounded by expectUint
	}
	if err := errors.Join(errs...); err != nil {
		return LeaderData{}, err
	}
	return ld, nil
}

// Node fetches /node (getNode, OtbrRestClient.ts:183-208): every field
// typed, the binary ones decoded from hex in their sizes, rloc16 a JSON
// number or a hex string.
func (c *Client) Node(ctx context.Context) (NodeInfo, error) {
	const where = "/node"
	body, err := c.record(ctx, where)
	if err != nil {
		return NodeInfo{}, err
	}
	leaderRaw, ok := body["leaderData"].(map[string]any)
	if !ok {
		return NodeInfo{}, protocolError("%s: missing or non-object field leaderData", where)
	}
	ld, err := leaderData(leaderRaw, where+".leaderData")
	if err != nil {
		return NodeInfo{}, err
	}
	var errs []error
	str := func(key string) string {
		s, err := expectString(body, key)
		errs = append(errs, err)
		return s
	}
	hexOf := func(key string, n int) []byte {
		s, err := expectString(body, key)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		b, err := parseHexBytes(s, where+"."+key, n)
		errs = append(errs, err)
		return b
	}
	info := NodeInfo{
		BaID:        hexOf("baId", 16),
		State:       str("state"),
		NumOfRouter: -1,
		RlocAddress: str("rlocAddress"),
		ExtAddress:  hexOf("extAddress", 8),
		NetworkName: str("networkName"),
		LeaderData:  ld,
		ExtPANID:    hexOf("extPanId", 8),
	}
	if err := errors.Join(errs...); err != nil {
		return NodeInfo{}, err
	}
	rloc16, ok := parseRloc16(body["rloc16"])
	if !ok {
		return NodeInfo{}, protocolError("%s: missing or invalid field rloc16", where)
	}
	info.Rloc16 = rloc16
	for _, key := range []string{"numOfRouter", "routerCount"} { // OtbrRestClient.ts:200
		if n, ok := asNumber(body[key]); ok {
			info.NumOfRouter = int(n)
			break
		}
	}
	return info, nil
}

// ProbeNode is the light probe of /node (probeNode, OtbrRestClient.ts:
// 210-232): the observed key casing, the network name and the Extended
// PAN ID, without validating the rest of the node.
func (c *Client) ProbeNode(ctx context.Context) (format KeyFormat, networkName string, extPANID []byte, err error) {
	body, format, err := c.fetchJSON(ctx, "/node")
	if err != nil {
		return "", "", nil, err
	}
	obj, ok := body.(map[string]any)
	if !ok || format == "" {
		return "", "", nil, protocolError("/node did not return an OTBR node object")
	}
	if networkName, err = expectString(obj, "networkName"); err != nil {
		return "", "", nil, err
	}
	xp, err := expectString(obj, "extPanId")
	if err != nil {
		return "", "", nil, err
	}
	if extPANID, err = parseHexBytes(xp, "/node.extPanId", 8); err != nil {
		return "", "", nil, err
	}
	return format, networkName, extPANID, nil
}

// Diagnostics fetches the /diagnostics array as-is, keys normalized
// (getDiagnostics, OtbrRestClient.ts:234-251); [Translate] types an
// entry.
func (c *Client) Diagnostics(ctx context.Context) ([]any, error) {
	body, _, err := c.fetchJSON(ctx, "/diagnostics")
	if err != nil {
		return nil, err
	}
	list, ok := body.([]any)
	if !ok {
		return nil, protocolError("/diagnostics did not return a JSON array")
	}
	return list, nil
}

// DatasetHex is the active and pending dataset as the border router
// serves them, "" for none (OtbrDatasetHex, OtbrRestClient.ts:34-37).
type DatasetHex struct {
	Active, Pending string
}

// Datasets fetches both datasets' hex (getDataset, OtbrRestClient.ts:
// 253-272); 204 is none.
func (c *Client) Datasets(ctx context.Context) (DatasetHex, error) {
	active, _, err := c.fetchText(ctx, "/node/dataset/active")
	if err != nil {
		return DatasetHex{}, err
	}
	pending, _, err := c.fetchText(ctx, "/node/dataset/pending")
	if err != nil {
		return DatasetHex{}, err
	}
	return DatasetHex{Active: active, Pending: pending}, nil
}

// jsonString is #getJsonString (OtbrRestClient.ts:526-532).
func (c *Client) jsonString(ctx context.Context, path string) (string, error) {
	body, _, err := c.fetchJSON(ctx, path)
	if err != nil {
		return "", err
	}
	s, ok := body.(string)
	if !ok {
		return "", protocolError("GET %s did not return a string", path)
	}
	return s, nil
}

// jsonNumber is #getJsonNumber (OtbrRestClient.ts:534-540).
func (c *Client) jsonNumber(ctx context.Context, path string) (float64, error) {
	body, _, err := c.fetchJSON(ctx, path)
	if err != nil {
		return 0, err
	}
	n, ok := asNumber(body)
	if !ok {
		return 0, protocolError("GET %s did not return a number", path)
	}
	return n, nil
}

// State fetches the node role from /node/state ("router", "leader",
// "child", "detached", "disabled"; getState, OtbrRestClient.ts:274-282).
func (c *Client) State(ctx context.Context) (thread.DeviceRole, error) {
	s, err := c.jsonString(ctx, "/node/state")
	return thread.DeviceRole(s), err
}

// NetworkName fetches /node/network-name (OtbrRestClient.ts:284-289).
func (c *Client) NetworkName(ctx context.Context) (string, error) {
	return c.jsonString(ctx, "/node/network-name")
}

// Rloc fetches the RLOC IPv6 address from /node/rloc
// (OtbrRestClient.ts:291-296).
func (c *Client) Rloc(ctx context.Context) (string, error) {
	return c.jsonString(ctx, "/node/rloc")
}

// Rloc16 fetches /node/rloc16 (OtbrRestClient.ts:298-303).
func (c *Client) Rloc16(ctx context.Context) (uint16, error) {
	n, err := c.jsonNumber(ctx, "/node/rloc16")
	if err != nil {
		return 0, err
	}
	if n < 0 || n > math.MaxUint16 || n != math.Trunc(n) {
		return 0, protocolError("GET /node/rloc16 out of range")
	}
	return uint16(n), nil
}

// NumOfRouter fetches /node/num-of-router (OtbrRestClient.ts:305-310).
func (c *Client) NumOfRouter(ctx context.Context) (int, error) {
	n, err := c.jsonNumber(ctx, "/node/num-of-router")
	return int(n), err
}

// hexEndpoint is a JSON string of n hex bytes at path.
func (c *Client) hexEndpoint(ctx context.Context, path string, n int) ([]byte, error) {
	s, err := c.jsonString(ctx, path)
	if err != nil {
		return nil, err
	}
	return parseHexBytes(s, path, n)
}

// ExtAddress fetches the 8-byte extended address from /node/ext-address
// (OtbrRestClient.ts:312-317).
func (c *Client) ExtAddress(ctx context.Context) ([]byte, error) {
	return c.hexEndpoint(ctx, "/node/ext-address", 8)
}

// ExtPANID fetches the 8-byte Extended PAN ID from /node/ext-panid
// (OtbrRestClient.ts:319-324).
func (c *Client) ExtPANID(ctx context.Context) ([]byte, error) {
	return c.hexEndpoint(ctx, "/node/ext-panid", 8)
}

// BorderAgentID fetches the 16-byte Border Agent ID from /node/ba-id
// (OtbrRestClient.ts:326-333); [CodeUnsupported] on older firmware.
func (c *Client) BorderAgentID(ctx context.Context) ([]byte, error) {
	return c.hexEndpoint(ctx, "/node/ba-id", 16)
}

// LeaderData fetches /node/leader-data (OtbrRestClient.ts:335-351).
func (c *Client) LeaderData(ctx context.Context) (LeaderData, error) {
	rec, err := c.record(ctx, "/node/leader-data")
	if err != nil {
		return LeaderData{}, err
	}
	return leaderData(rec, "/node/leader-data")
}

// dataset is #getDataset (OtbrRestClient.ts:509-519): nil on 204 or an
// empty body; the TLVs of the hex, which must decode as an operational
// dataset (matter.js decodes it with OperationalDataset.decode; a dataset
// that does not is [CodeProtocol]).
func (c *Client) dataset(ctx context.Context, path string) ([]byte, error) {
	text, ok, err := c.fetchText(ctx, path)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	ds, err := thread.DecodeDatasetHex(text)
	if err != nil {
		return nil, &Error{Code: CodeProtocol, Message: fmt.Sprintf("GET %s returned an undecodable dataset", path), Cause: err}
	}
	return ds.Raw, nil
}

// ActiveDataset fetches the active dataset's TLVs, nil when the border
// router has none (getActiveDataset, OtbrRestClient.ts:353-360).
func (c *Client) ActiveDataset(ctx context.Context) ([]byte, error) {
	return c.dataset(ctx, "/node/dataset/active")
}

// PendingDataset fetches the pending dataset's TLVs, nil when there is
// none (getPendingDataset, OtbrRestClient.ts:362-369).
func (c *Client) PendingDataset(ctx context.Context) ([]byte, error) {
	return c.dataset(ctx, "/node/dataset/pending")
}

// CoprocessorVersion fetches the RCP's version string
// (OtbrRestClient.ts:371-378); [CodeUnsupported] on older firmware.
func (c *Client) CoprocessorVersion(ctx context.Context) (string, error) {
	return c.jsonString(ctx, "/node/coprocessor/version")
}

// CommissionerState fetches the on-border-router commissioner's state
// (OtbrRestClient.ts:380-387); [CodeUnsupported] without one. Its setter
// is not wrapped (package doc).
func (c *Client) CommissionerState(ctx context.Context) (string, error) {
	return c.jsonString(ctx, "/node/commissioner/state")
}

// Joiners fetches the commissioner's pending joiners, keys normalized
// (getJoiners, OtbrRestClient.ts:389-403).
func (c *Client) Joiners(ctx context.Context) ([]any, error) {
	body, _, err := c.fetchJSON(ctx, "/node/commissioner/joiner")
	if err != nil {
		return nil, err
	}
	list, ok := body.([]any)
	if !ok {
		return nil, protocolError("/node/commissioner/joiner did not return a JSON array")
	}
	return list, nil
}

// collection reads a collection response as an array, the bare form or
// the JSON:API envelope {data: [...]} (asCollectionArray,
// OtbrRestClient.ts:92-100).
func collection(body any, where string) ([]any, error) {
	if list, ok := body.([]any); ok {
		return list, nil
	}
	if obj, ok := body.(map[string]any); ok {
		if list, ok := obj["data"].([]any); ok {
			return list, nil
		}
	}
	return nil, protocolError("%s did not return a JSON array", where)
}

// DiagnosticsCollection fetches the post-2024 diagnostics collection
// /api/diagnostics, keys normalized (getDiagnosticsCollection,
// OtbrRestClient.ts:627-634). It holds what the border router's actions
// last collected; the actions themselves (postAction, getAction,
// clearDiagnostics) are not wrapped.
func (c *Client) DiagnosticsCollection(ctx context.Context) ([]any, error) {
	body, _, err := c.fetchJSON(ctx, "/api/diagnostics")
	if err != nil {
		return nil, err
	}
	return collection(body, "/api/diagnostics")
}

// Devices fetches the discovered-device directory /api/devices
// (listDevices, OtbrRestClient.ts:636-640).
func (c *Client) Devices(ctx context.Context) ([]any, error) {
	body, _, err := c.fetchJSON(ctx, "/api/devices")
	if err != nil {
		return nil, err
	}
	return collection(body, "/api/devices")
}

// DiagnosticsAPI is the diagnostics flavour a border router serves
// (OtbrRestCapability.ts:23-28).
type DiagnosticsAPI string

// The diagnostics flavours.
const (
	// DiagnosticsAPICollection is the post-2024 action model under
	// /api/diagnostics.
	DiagnosticsAPICollection DiagnosticsAPI = "collection"
	// DiagnosticsAPILegacy is the GET /diagnostics snapshot.
	DiagnosticsAPILegacy DiagnosticsAPI = "legacy"
	// DiagnosticsAPINone is neither.
	DiagnosticsAPINone DiagnosticsAPI = "none"
)

// DetectDiagnosticsAPI tells the flavour by the endpoints' statuses alone
// (detectDiagnosticsApi, OtbrRestClient.ts:647-686): a 404 is a negative
// answer, only a network failure an error.
func (c *Client) DetectDiagnosticsAPI(ctx context.Context) (DiagnosticsAPI, error) {
	for _, probe := range []struct {
		path string
		api  DiagnosticsAPI
	}{{"/api/diagnostics", DiagnosticsAPICollection}, {"/diagnostics", DiagnosticsAPILegacy}} {
		r, err := c.do(ctx, http.MethodGet, probe.path, "application/json", "", nil)
		if err != nil {
			return "", err
		}
		if r.ok() {
			return probe.api, nil
		}
	}
	return DiagnosticsAPINone, nil
}

// The mutating operations, reached only through [BorderRouter].

// setState enables or disables the Thread interface: PUT /node/state with
// "enable" or "disable" as JSON (setState, OtbrRestClient.ts:405-415).
func (c *Client) setState(ctx context.Context, enable bool) error {
	body := `"disable"`
	if enable {
		body = `"enable"`
	}
	return c.mutate(ctx, http.MethodPut, "/node/state", "application/json", []byte(body))
}

// setDataset PUTs a dataset's TLVs as text/plain hex to the active or the
// pending endpoint (setActiveDataset, setPendingDataset, #putDataset,
// OtbrRestClient.ts:417-442, :521-524); [CodeConflict] when Thread is
// active and the active dataset cannot be replaced in place.
func (c *Client) setDataset(ctx context.Context, pending bool, tlvs []byte) error {
	path := "/node/dataset/active"
	if pending {
		path = "/node/dataset/pending"
	}
	return c.mutate(ctx, http.MethodPut, path, "text/plain", []byte(hex.EncodeToString(tlvs)))
}

// deleteActiveDataset clears the active dataset: DELETE
// /node/dataset/active (OtbrRestClient.ts:428-433).
func (c *Client) deleteActiveDataset(ctx context.Context) error {
	return c.mutate(ctx, http.MethodDelete, "/node/dataset/active", "", nil)
}
