// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tcmdef "thingfabric.dev/go-fabric/cluster/spec/tlscertificatemanagement"
	tcldef "thingfabric.dev/go-fabric/cluster/spec/tlsclientmanagement"
	"thingfabric.dev/go-fabric/cluster/tls"
	"thingfabric.dev/go-fabric/im"
)

// hookKeys is a CertificatePort whose CertificateValid and GenerateCSR
// run a hook first: the window in which the server holds no lock and a
// second session's command lands.
type hookKeys struct {
	*mockKeys
	hmu     sync.Mutex
	onValid func()
	onCSR   func(call int) error
	calls   int
}

func newHookKeys() *hookKeys { return &hookKeys{mockKeys: &mockKeys{invalid: map[string]bool{}}} }

func (h *hookKeys) CertificateValid(ctx context.Context, cert []byte) bool {
	h.hmu.Lock()
	f := h.onValid
	h.onValid = nil
	h.hmu.Unlock()
	if f != nil {
		f()
	}
	return h.mockKeys.CertificateValid(ctx, cert)
}

func (h *hookKeys) GenerateCSR(ctx context.Context, fabric uint8, ccdid uint16, fresh bool, nonce []byte) (csr, sig []byte, err error) {
	h.hmu.Lock()
	h.calls++
	call, f := h.calls, h.onCSR
	h.hmu.Unlock()
	if f != nil {
		if err := f(call); err != nil {
			return nil, nil, err
		}
	}
	return h.mockKeys.GenerateCSR(ctx, fabric, ccdid, fresh, nonce)
}

func freshCSR(ctx context.Context, c *tls.CertificateManagement) (tcmdef.ClientCsrResponse, error) {
	resp, err := c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.NullOf[uint16]()})
	if err != nil {
		return tcmdef.ClientCsrResponse{}, err
	}
	r, _ := resp.(tcmdef.ClientCsrResponse)
	return r, nil
}

// TestProvisionRootReplaceRacesRemove (R2-P6-conc-1101): a
// RemoveRootCertificate of the CAID that lands while
// ProvisionRootCertificate(CAID) is in the port answers the replace
// NOT_FOUND — no panic, and the lock is free for the next read.
func TestProvisionRootReplaceRacesRemove(t *testing.T) {
	keys := newHookKeys()
	c, _ := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.Port = keys })
	caid := provisionRoot(t, c, testCertificate(t, 1))
	keys.onValid = func() {
		_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: caid})
		wantStatus(t, "concurrent RemoveRootCertificate", err, im.StatusSuccess)
	}
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("ProvisionRootCertificate panicked: %v", p)
			}
		}()
		_, err = c.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionRootCertificate,
			tcmdef.ProvisionRootCertificateRequest{Certificate: testCertificate(t, 2), Caid: spec.ValueOf(caid)})
	}()
	wantStatus(t, "replace of a removed CAID", err, im.StatusNotFound)
	// A lock the replace stranded hangs this read (the test binary's
	// timeout reports it).
	c.MatterReadFiltered(peer(testFabric), tcmdef.AttrProvisionedRootCertificates)
}

// TestProvisionRootRechecksAfterThePort (R2-P6-conc-1109): a second
// session's ProvisionRootCertificate inside the first one's port call
// cannot carry the fabric past MaxRootCertificates or a certificate in
// twice.
func TestProvisionRootRechecksAfterThePort(t *testing.T) {
	keys := newHookKeys()
	c, _ := newCerts(t, func(cfg *tls.CertificateManagementConfig) {
		cfg.Port, cfg.MaxRootCertificates = keys, 5
	})
	for i := range 4 {
		provisionRoot(t, c, testCertificate(t, int64(10+i)))
	}
	keys.onValid = func() { provisionRoot(t, c, testCertificate(t, 20)) }
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionRootCertificate,
		tcmdef.ProvisionRootCertificateRequest{Certificate: testCertificate(t, 21), Caid: spec.NullOf[uint16]()})
	wantStatus(t, "the slot taken during the port call", err, im.StatusResourceExhausted)
	if n := len(c.MatterHeldFabrics()); n != 1 {
		t.Fatalf("held fabrics %d", n)
	}
	if n := len(readAs[[]tcmdef.TLSCertStruct](t, c.MatterRead, tcmdef.AttrProvisionedRootCertificates)); n != 5 {
		t.Fatalf("%d root certificates, capacity 5", n)
	}

	c2, _ := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.Port = keys })
	same := testCertificate(t, 30)
	keys.onValid = func() { provisionRoot(t, c2, same) }
	_, err = c2.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionRootCertificate,
		tcmdef.ProvisionRootCertificateRequest{Certificate: same, Caid: spec.NullOf[uint16]()})
	wantStatus(t, "the certificate provisioned during the port call", err, im.StatusAlreadyExists)
}

// TestClientCSRReservesItsSlot (R2-P6-conc-1109): a fresh ClientCSR holds
// its slot and its CCDID while the port makes the key: a second one in
// that window counts it against MaxClientCertificates, and an id given
// back by a failed CSR is never handed to a third while another CSR in
// flight holds the id after it.
func TestClientCSRReservesItsSlot(t *testing.T) {
	keys := newHookKeys()
	c, _ := newCerts(t, func(cfg *tls.CertificateManagementConfig) {
		cfg.Port, cfg.MaxClientCertificates = keys, 5
	})
	for range 4 {
		clientCSR(t, c)
	}
	keys.onCSR = func(call int) error {
		if call == 5 {
			_, err := freshCSR(peer(testFabric), c)
			wantStatus(t, "the CSR inside the reserved window", err, im.StatusResourceExhausted)
		}
		return nil
	}
	if _, err := freshCSR(peer(testFabric), c); err != nil {
		t.Fatal(err)
	}
	if n := len(readAs[[]tcmdef.TLSClientCertificateDetailStruct](t, c.MatterRead, tcmdef.AttrProvisionedClientCertificates)); n != 5 {
		t.Fatalf("%d client certificates, capacity 5", n)
	}

	// A (id 0) and B (id 1) in flight; A fails and gives its id back; C
	// takes 0; D must not get 1, which B holds.
	keys2 := newHookKeys()
	c2, _ := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.Port = keys2 })
	aIn, aGo, bIn, bGo := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	keys2.onCSR = func(call int) error {
		switch call {
		case 1:
			close(aIn)
			<-aGo
			return spec.Errorf(im.StatusBusy, "busy")
		case 2:
			close(bIn)
			<-bGo
		}
		return nil
	}
	var bResp tcmdef.ClientCsrResponse
	var bErr error
	aDone, bDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(aDone)
		if _, err := freshCSR(peer(testFabric), c2); statusOf(err) != im.StatusBusy {
			t.Errorf("A: %v", err)
		}
	}()
	<-aIn
	go func() {
		defer close(bDone)
		bResp, bErr = freshCSR(peer(testFabric), c2)
	}()
	<-bIn
	close(aGo)
	<-aDone
	cResp, err := freshCSR(peer(testFabric), c2)
	if err != nil {
		t.Fatal(err)
	}
	dResp, err := freshCSR(peer(testFabric), c2)
	if err != nil {
		t.Fatal(err)
	}
	close(bGo)
	<-bDone
	if bErr != nil {
		t.Fatal(bErr)
	}
	ids := map[uint16]int{bResp.Ccdid: 1}
	ids[cResp.Ccdid]++
	ids[dResp.Ccdid]++
	for id, n := range ids {
		if n > 1 {
			t.Fatalf("CCDID %d handed out %d times (B %d, C %d, D %d)", id, n, bResp.Ccdid, cResp.Ccdid, dResp.Ccdid)
		}
	}
}

// TestClientCSRFailedSaveDropsTheKey (R2-P6-err-1110): a fresh ClientCSR
// whose save fails drops the key the port made.
func TestClientCSRFailedSaveDropsTheKey(t *testing.T) {
	st := &mapStore{}
	c, keys := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.Store, cfg.Key = st, "k" })
	st.failSet = true
	_, err := freshCSR(peer(testFabric), c)
	wantStatus(t, "failed save", err, im.StatusFailure)
	keys.mu.Lock()
	removed := append([]uint16(nil), keys.removed...)
	keys.mu.Unlock()
	if len(removed) != 1 || removed[0] != 0 {
		t.Fatalf("the port's key was not dropped: removed %v", removed)
	}
}

// lookupHook is a CertificateLookup that runs a hook after the real
// RootCertificate: the window between ProvisionEndpoint's root check and
// its insert.
type lookupHook struct {
	*tls.CertificateManagement
	after func()
}

func (l *lookupHook) RootCertificate(fabric uint8, caid uint16) (tcmdef.TLSCertStruct, bool) {
	r, ok := l.CertificateManagement.RootCertificate(fabric, caid)
	if f := l.after; f != nil {
		l.after = nil
		f()
	}
	return r, ok
}

// checkerHook is a DependencyChecker that runs a hook after the real
// check: the window between RemoveRootCertificate's in-use check and its
// delete.
type checkerHook struct {
	tls.DependencyChecker
	after func()
}

func (h *checkerHook) RootCertificateInUse(fabric uint8, caid uint16) bool {
	inUse := h.DependencyChecker.RootCertificateInUse(fabric, caid)
	if f := h.after; f != nil {
		h.after = nil
		f()
	}
	return inUse
}

// danglingEndpoints are the endpoints whose root certificate is gone.
func danglingEndpoints(m *tls.ClientManagement, c *tls.CertificateManagement) []uint16 {
	v, _ := m.MatterRead(tcldef.AttrProvisionedEndpoints)
	var eps []tcldef.TLSEndpointStruct
	spec.Assign(&eps, v)
	var out []uint16
	for _, e := range eps {
		if _, ok := c.RootCertificate(e.FabricIndex, e.Caid); !ok {
			out = append(out, e.EndpointId)
		}
	}
	return out
}

// TestRemoveRootAndProvisionEndpointInterleave (R2-P6-conc-1109): neither
// order of RemoveRootCertificate and ProvisionEndpoint leaves an endpoint
// naming a removed root certificate.
func TestRemoveRootAndProvisionEndpointInterleave(t *testing.T) {
	t.Run("provision inside the remove's window", func(t *testing.T) {
		c, _ := newCerts(t)
		caid := provisionRoot(t, c, testCertificate(t, 1))
		m, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5, Certificates: c})
		if err != nil {
			t.Fatal(err)
		}
		c.SetDependencyChecker(&checkerHook{DependencyChecker: m, after: func() {
			_, _ = m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, provisionRequest(caid))
		}})
		_, _ = c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: caid})
		if d := danglingEndpoints(m, c); len(d) != 0 {
			t.Fatalf("endpoints %v name a removed root certificate", d)
		}
	})
	t.Run("remove inside the provision's window", func(t *testing.T) {
		c, _ := newCerts(t)
		caid := provisionRoot(t, c, testCertificate(t, 1))
		look := &lookupHook{CertificateManagement: c}
		m, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5, Certificates: look})
		if err != nil {
			t.Fatal(err)
		}
		c.SetDependencyChecker(m)
		removed := make(chan error, 1)
		look.after = func() {
			go func() {
				_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: caid})
				removed <- err
			}()
			// Wait until the remove has hidden the certificate: its mark
			// (it then waits in its dependency check for the provision to
			// end) or, without one, its delete (the provision then inserts
			// an endpoint naming a removed certificate).
			for {
				if _, ok := c.RootCertificate(testFabric, caid); !ok {
					break
				}
				runtime.Gosched()
			}
		}
		_, _ = m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, provisionRequest(caid))
		<-removed
		if d := danglingEndpoints(m, c); len(d) != 0 {
			t.Fatalf("endpoints %v name a removed root certificate", d)
		}
	})
}

// TestEveryLockIsDeferred is the standing guard of R2-P6-conc-1101's
// class: in this package every mu.Lock() is followed by its deferred
// Unlock, so a panic in a locked section cannot strand the lock (the
// UDP listener recovers a handler's panic and the process lives on).
func TestEveryLockIsDeferred(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			block, ok := n.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, st := range block.List {
				recv, ok := lockCall(st, "Lock")
				if !ok {
					continue
				}
				next := ""
				if i+1 < len(block.List) {
					if d, ok := block.List[i+1].(*ast.DeferStmt); ok {
						next, _ = callRecv(d.Call, "Unlock")
					}
				}
				if next != recv {
					t.Errorf("%s: %s.Lock() is not followed by defer %s.Unlock()", fset.Position(st.Pos()), recv, recv)
				}
			}
			return true
		})
	}
}

func lockCall(st ast.Stmt, method string) (string, bool) {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	return callRecv(call, method)
}

func callRecv(call *ast.CallExpr, method string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != method {
		return "", false
	}
	var b strings.Builder
	var walk func(ast.Expr) bool
	walk = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			b.WriteString(x.Name)
			return true
		case *ast.SelectorExpr:
			if !walk(x.X) {
				return false
			}
			b.WriteString("." + x.Sel.Name)
			return true
		}
		return false
	}
	if !walk(sel.X) || !strings.HasSuffix(b.String(), "mu") {
		return "", false
	}
	return b.String(), true
}
