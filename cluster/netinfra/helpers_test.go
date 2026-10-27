// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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

func (d *dirtyList) empty() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.ids) == 0
}

func (d *dirtyList) clear() {
	d.mu.Lock()
	d.ids = nil
	d.mu.Unlock()
}

// clusterAttributes is the cluster's own attributes of its AttributeList:
// chip's IsAttributesListEqualTo adds the globals itself.
func clusterAttributes(srv contract.ClusterAttributeLister) []uint32 {
	var out []uint32
	for _, id := range srv.MatterAttributes() {
		if id < 0xF000 {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func mustT(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
