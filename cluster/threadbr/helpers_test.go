// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/thread"
)

// The subject descriptors chip's ClusterTester sets
// (src/app/server-cluster/testing/ClusterTester.h): a CASE session on
// fabric 1, a PASE session, a group message.
func caseCtx() context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x1122, nil)
}

func paseCtx() context.Context {
	return im.WithAuthModePASE(im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0, nil))
}

func groupCtx() context.Context {
	return im.WithGroupSubject(im.WithFabricFilter(context.Background(), false, 1), im.GroupSubject{GroupID: 1, HasValidMapping: true})
}

// statusOf is the IM status err carries; Success for nil, Failure for an
// error that carries none.
func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return im.StatusFailure
}

// chipDataset is the valid dataset of chip's
// TestThreadBorderRouterManagementCluster.cpp:303-308 at 6170af84 (Active
// Timestamp 0, Channel 11, "OpenThread", PAN ID 0xc115).
var chipDataset = []byte{
	0x0e, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x0b, 0x35, 0x06,
	0x00, 0x04, 0x00, 0x1f, 0xff, 0xe0, 0x02, 0x08, 0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef, 0x07,
	0x08, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00, 0x05, 0x10, 0xb7, 0x28, 0x08, 0x04, 0x85, 0xcf,
	0xc5, 0x25, 0x7f, 0x68, 0x4c, 0x54, 0x9d, 0x6a, 0x57, 0x5e, 0x03, 0x0a, 0x4f, 0x70, 0x65, 0x6e, 0x54,
	0x68, 0x72, 0x65, 0x61, 0x64, 0x01, 0x02, 0xc1, 0x15, 0x04, 0x10, 0xcb, 0x13, 0x47, 0xeb, 0x0c, 0xd4,
	0xb3, 0x5c, 0xd1, 0x42, 0xda, 0x5e, 0x6d, 0xf1, 0x8b, 0x88, 0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

// timestamped is a dataset that carries only an Active Timestamp of ts:
// what chip's MockDelegate::GetDataset builds with SetActiveTimestamp
// (:61-72).
func timestamped(ts uint64) []byte {
	return binary.BigEndian.AppendUint64([]byte{0x0e, 0x08}, ts)
}

// fakeBorderRouter is chip's MockDelegate (:42-115) as a BorderRouter:
// the name "MockBR", a 16-byte agent id, Thread version 1, the interface
// enabled, an active dataset with Active Timestamp 12345 and an empty
// pending one. deferActivation keeps an activation waiting until release
// or the server ends it; notFound answers no dataset (CHIP_ERROR_NOT_FOUND).
type fakeBorderRouter struct {
	mu              sync.Mutex
	name            string
	agentID         []byte
	agentErr        error
	version         uint16
	enabled         bool
	enabledErr      error
	active, pending []byte
	notFound        bool
	datasetErr      error
	activateErr     error
	pendingErr      error
	deferActivation bool
	started         chan []byte
	release         chan error
	revertCalled    bool
	commitCalled    bool
	setPending      []byte
	diag            thread.NodeDiagnostics
	diagErr         error
}

func newFake() *fakeBorderRouter {
	return &fakeBorderRouter{
		name: "MockBR", agentID: make([]byte, 16), version: 1, enabled: true,
		active: timestamped(12345), pending: []byte{},
		started: make(chan []byte, 4), release: make(chan error, 1),
	}
}

func (f *fakeBorderRouter) BorderRouterName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name
}

func (f *fakeBorderRouter) BorderAgentID(context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.agentID), f.agentErr
}

func (f *fakeBorderRouter) ThreadVersion(context.Context) (uint16, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, nil
}

func (f *fakeBorderRouter) InterfaceEnabled(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled, f.enabledErr
}

func (f *fakeBorderRouter) Dataset(_ context.Context, pending bool) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.datasetErr != nil:
		return nil, f.datasetErr
	case f.notFound:
		return nil, nil
	case pending:
		return slices.Clone(f.pending), nil
	}
	return slices.Clone(f.active), nil
}

func (f *fakeBorderRouter) SetActiveDataset(ctx context.Context, dataset []byte) error {
	f.mu.Lock()
	wait, err := f.deferActivation, f.activateErr
	f.mu.Unlock()
	f.started <- dataset
	if !wait {
		return err
	}
	select {
	case err := <-f.release:
		return err
	case <-ctx.Done():
		// chip's mCompleteOnRevert: the delegate completes the kept
		// callback with CHIP_ERROR_CANCELLED (:92-102).
		return ctx.Err()
	}
}

func (f *fakeBorderRouter) CommitActiveDataset(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commitCalled = true
	return nil
}

func (f *fakeBorderRouter) RevertActiveDataset(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revertCalled = true
	return nil
}

func (f *fakeBorderRouter) SetPendingDataset(_ context.Context, dataset []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setPending = dataset
	return f.pendingErr
}

func (f *fakeBorderRouter) Diagnostics(context.Context) (thread.NodeDiagnostics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.diag, f.diagErr
}

func (f *fakeBorderRouter) set(fn func(f *fakeBorderRouter)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// breadcrumbs is chip's MockBreadcrumbTracker (:117-127).
type breadcrumbs struct {
	mu     sync.Mutex
	value  uint64
	called bool
}

func (b *breadcrumbs) set(v uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.value, b.called = v, true
}

func (b *breadcrumbs) get() (uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value, b.called
}

// dirtyList is chip's ClusterTester dirty list: the attributes the
// server reported changed.
type dirtyList struct {
	mu  sync.Mutex
	ids []uint32
}

func watch(srv contract.AttributeChangeNotifier) *dirtyList {
	d := &dirtyList{}
	srv.OnAttributeChange(func(c contract.Change) {
		d.mu.Lock()
		d.ids = append(d.ids, c.Attributes...)
		d.mu.Unlock()
	})
	return d
}

func (d *dirtyList) has(id uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Contains(d.ids, id)
}

func mustT(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
