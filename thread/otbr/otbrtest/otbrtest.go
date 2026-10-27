// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package otbrtest is an OpenThread Border Router's REST surface in
// process, for the tests of thread/otbr and of the Thread Border Router
// servers on it: an httptest server that answers the endpoints
// thread/otbr reads and writes, after the recorded responses of matter.js
// packages/thread-br-client/test/fixtures/otbr-rest/ at 862ad75f
// (fixtures.go) and the routing mock of OtbrRestClientTest.ts
// (MockOtbrServer, :51-94), with the behaviour of an ot-br-posix agent the
// writes need: a Thread interface that attaches after an enable, a 409 for
// a dataset replaced or deleted while it is up. It imports nothing of the
// module, so thread/otbr's own tests use it.
package otbrtest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// Options configure a [Server]; the zero value is a PascalCase build with
// the legacy diagnostics and no active dataset.
type Options struct {
	// Camel serves /node and /node/leader-data in the post-2024 camelCase
	// keys (node-camel.json.ts: routerCount, a hex-string rloc16); false
	// is the older PascalCase (node.json.ts).
	Camel bool
	// Diagnostics is the flavour served: "legacy" (default, GET
	// /diagnostics), "collection" (/api/diagnostics as well), "none".
	Diagnostics string
	// Active is the active dataset's TLVs the border router starts with,
	// nil for none; with one it starts as the leader.
	Active []byte
	// AttachPolls is how many /node/state reads after an enable answer
	// "detached" before the border router answers "leader"; 0 is 1.
	AttachPolls int
	// DiagnosticsBody is the /diagnostics array; empty is the fixture
	// [Diagnostics], whose record 0011223344556601 / 0x7400 is the border
	// router's own.
	DiagnosticsBody string
}

// Request is one request the server answered.
type Request struct {
	Method, Path, ContentType, Body string
}

// Server is the fake border router.
type Server struct {
	*httptest.Server

	mu        sync.Mutex
	opts      Options
	active    []byte
	pending   []byte
	state     string
	pollsLeft int
	requests  []Request
	faults    map[string]int
	down      bool
}

// The border router's own identity (node.json.ts).
var (
	borderAgentID = "00112233445566778899AABBCCDDEEFF"
	extAddress    = "0011223344556601"
	rloc16        = 0x7400
)

// New starts a fake border router; Close stops it.
func New(opts Options) *Server {
	s := &Server{opts: opts, active: clone(opts.Active), state: "disabled", faults: map[string]int{}}
	if s.active != nil {
		s.state = "leader"
	}
	if s.opts.AttachPolls == 0 {
		s.opts.AttachPolls = 1
	}
	if s.opts.Diagnostics == "" {
		s.opts.Diagnostics = "legacy"
	}
	if s.opts.DiagnosticsBody == "" {
		s.opts.DiagnosticsBody = Diagnostics
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}

// Host is the server's host, Port its port.
func (s *Server) Host() string {
	host, _, _ := net.SplitHostPort(s.Listener.Addr().String())
	return host
}

// Port is the server's port.
func (s *Server) Port() int {
	_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return n
}

// Requests are the requests answered so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request{}, s.requests...)
}

// Fail answers method path with status from now on; 0 lifts it.
func (s *Server) Fail(method, path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == 0 {
		delete(s.faults, method+" "+path)
		return
	}
	s.faults[method+" "+path] = status
}

// SetDown makes every request fail with 503 while down, a border router
// that stopped answering.
func (s *Server) SetDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

// SetState sets the role the border router reports.
func (s *Server) SetState(role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = role
}

// State is the role the border router reports.
func (s *Server) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Active is the active dataset's TLVs, nil for none.
func (s *Server) Active() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.active)
}

// Pending is the pending dataset's TLVs, nil for none.
func (s *Server) Pending() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.pending)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) { //nolint:funlen // the routing table: one case per endpoint
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"), Body: string(body)})
	if s.down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if status, ok := s.faults[r.Method+" "+r.URL.Path]; ok {
		w.WriteHeader(status)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /node":
		writeJSON(w, s.node())
	case "GET /node/state":
		if s.state == "detached" && s.pollsLeft > 0 {
			s.pollsLeft--
			if s.pollsLeft == 0 {
				s.state = "leader"
			}
			writeJSON(w, "detached")
			return
		}
		writeJSON(w, s.state)
	case "PUT /node/state":
		s.putState(w, strings.TrimSpace(string(body)))
	case "GET /node/dataset/active":
		s.serveDataset(w, s.active)
	case "GET /node/dataset/pending":
		s.serveDataset(w, s.pending)
	case "PUT /node/dataset/active":
		if s.state != "disabled" && s.active != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		s.putDataset(w, &s.active, body)
	case "PUT /node/dataset/pending":
		s.putDataset(w, &s.pending, body)
	case "DELETE /node/dataset/active":
		if s.state != "disabled" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		s.active = nil
		w.WriteHeader(http.StatusOK)
	case "GET /node/ba-id":
		writeJSON(w, borderAgentID)
	case "GET /node/ext-address":
		writeJSON(w, extAddress)
	case "GET /node/ext-panid":
		writeJSON(w, s.extPANID())
	case "GET /node/rloc16":
		writeJSON(w, rloc16)
	case "GET /node/rloc":
		writeJSON(w, "fd00:0:0:1:0:ff:fe00:7400")
	case "GET /node/network-name":
		writeJSON(w, s.networkName())
	case "GET /node/num-of-router":
		writeJSON(w, 5)
	case "GET /node/leader-data":
		writeJSON(w, s.leaderData())
	case "GET /diagnostics":
		if s.opts.Diagnostics == "none" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, s.opts.DiagnosticsBody)
	case "GET /api/diagnostics":
		if s.opts.Diagnostics != "collection" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, []any{})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// putState is PUT /node/state: "disable" disables the interface;
// "enable" with an active dataset brings it up detached until it
// attaches, without one it is a conflict.
func (s *Server) putState(w http.ResponseWriter, body string) {
	switch body {
	case `"disable"`:
		s.state = "disabled"
	case `"enable"`:
		if s.active == nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if s.state == "disabled" {
			s.state, s.pollsLeft = "detached", s.opts.AttachPolls
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) serveDataset(w http.ResponseWriter, ds []byte) {
	if ds == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, strings.ToUpper(hex.EncodeToString(ds)))
}

func (s *Server) putDataset(w http.ResponseWriter, into *[]byte, body []byte) {
	ds, err := hex.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	*into = ds
	w.WriteHeader(http.StatusOK)
}

// tlv is the value of the first TLV of type t in the active dataset.
func (s *Server) tlv(t byte) []byte {
	for i := 0; i+2 <= len(s.active); {
		n := int(s.active[i+1])
		if i+2+n > len(s.active) {
			return nil
		}
		if s.active[i] == t {
			return s.active[i+2 : i+2+n]
		}
		i += 2 + n
	}
	return nil
}

func (s *Server) extPANID() string {
	if xp := s.tlv(2); len(xp) == 8 {
		return strings.ToUpper(hex.EncodeToString(xp))
	}
	return "0000000000000000"
}

func (s *Server) networkName() string { return string(s.tlv(3)) }

func (s *Server) leaderData() map[string]any {
	if s.opts.Camel {
		return map[string]any{"partitionId": 305419896, "weighting": 64, "dataVersion": 147, "stableDataVersion": 205, "leaderRouterId": 61}
	}
	return map[string]any{"PartitionId": 305419896, "Weighting": 64, "DataVersion": 147, "StableDataVersion": 205, "LeaderRouterId": 61}
}

// node is /node in the configured casing (node.json.ts, node-camel.json.ts).
func (s *Server) node() map[string]any {
	if s.opts.Camel {
		return map[string]any{
			"baId": strings.ToLower(borderAgentID), "baState": "", "state": s.state, "routerCount": 5,
			"rlocAddress": "fd00:0:0:1:0:ff:fe00:7400", "extAddress": extAddress, "networkName": s.networkName(),
			"rloc16": fmt.Sprintf("0x%04x", rloc16), "leaderData": s.leaderData(), "extPanId": s.extPANID(),
		}
	}
	return map[string]any{
		"BaId": borderAgentID, "State": s.state, "NumOfRouter": 5, "RlocAddress": "fd00:0:0:1:0:ff:fe00:7400",
		"ExtAddress": extAddress, "NetworkName": s.networkName(), "Rloc16": rloc16,
		"LeaderData": s.leaderData(), "ExtPanId": s.extPANID(),
	}
}
