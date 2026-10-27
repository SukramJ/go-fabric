// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"time"

	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/thread/otbr"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// threadbrCases are TestSelfHeldDefaults' cases of the Thread Border
// Router servers: a ThreadBorderRouter on a fake OTBR.
func threadbrCases() (cases []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
},
) {
	build := func() (*Device, error) {
		srv := otbrtest.New(otbrtest.Options{})
		c, err := otbr.NewClient(otbr.Config{Host: srv.Host(), Port: srv.Port(), Timeout: 2 * time.Second})
		if err != nil {
			return nil, err
		}
		br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "kit OTBR", ThreadVersion: 5})
		if err != nil {
			return nil, err
		}
		return New(&ThreadBorderRouterType, testOptions("tbr"), tbrmdef.Declare{BorderRouter: br})
	}
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{tbrmdef.ClusterID, build, (*threadbr.ManagementServer)(nil)},
		{0x0035, build, (*threadbr.DiagnosticsServer)(nil)},
	}
}
