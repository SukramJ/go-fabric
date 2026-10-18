// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const testPasscode uint32 = 20202021

// startNode starts an uncommissioned node on IPv4 loopback with the
// system clock — a fabrictest device, as these tests drive a controller
// manager of their own on a loopback socket — and returns its UDP address.
func startNode(t *testing.T) *net.UDPAddr {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Clock: clock.System{}})
	d := site.AddDevice(fabrictest.Topology{Configure: func(c *node.Config) {
		c.Network = node.Network{Listen: "127.0.0.1:0", PreferIPv4: true}
		c.Commissioning.Passcode = testPasscode
		c.BasicInformation = node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "interaction test", NodeLabel: "interaction node", SerialNumber: "IA-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		}
	}})
	return d.Addr()
}

// paseTo opens a PASE session to the node at addr on a loopback manager.
func paseTo(t *testing.T, addr *net.UDPAddr) (*exchange.Manager, *exchange.Session) {
	t.Helper()
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := exchange.NewManager(l, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.Serve(ctx, m.HandleDatagram)
	}()
	t.Cleanup(func() {
		m.Close()
		cancel()
		<-done
	})
	pctx, pcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer pcancel()
	s, err := handshake.PASE(pctx, m, addr, testPasscode, handshake.PASEOptions{SuppressPeerLoss: true})
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	return m, s
}

func rootAttr(cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestAgainstTheNode: Invoke and Read against the in-process node over a
// PASE session — a response with fields (ArmFailSafe), a status reply (an
// unknown command), a read of single attributes and a wildcard read of the
// root endpoint large enough to be chunked.
func TestAgainstTheNode(t *testing.T) {
	t.Parallel()
	m, s := paseTo(t, startNode(t))
	c := interaction.New(m, s, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := c.Invoke(ctx, armPath, gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1}, interaction.InvokeOptions{})
	if err != nil {
		t.Fatalf("ArmFailSafe: %v", err)
	}
	var arm gencomm.ArmFailSafeResponse
	if err := arm.DecodeTLV(res.Fields); err != nil || arm.ErrorCode != gencomm.CommissioningErrorOk || res.Command != gencomm.CmdArmFailSafeResponse {
		t.Fatalf("ArmFailSafeResponse %+v (%v)", arm, err)
	}

	_, err = c.Invoke(ctx, im.ConcreteCommandPath{Cluster: gencomm.ClusterID, Command: 0x77}, nil, interaction.InvokeOptions{ExtendedFailSafeTimeout: true})
	var se *interaction.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusUnsupportedCommand {
		t.Fatalf("unknown command: %v", err)
	}

	report, err := c.Read(ctx, rootAttr(basicinfo.ClusterID, basicinfo.AttrVendorId), rootAttr(basicinfo.ClusterID, basicinfo.AttrProductName))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	vid, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrVendorId)
	if !ok || vid.El.Uint != 0xFFF1 {
		t.Fatalf("VendorID %+v", vid)
	}
	if name, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrProductName); !ok {
		t.Fatal("no ProductName")
	} else if v, err := spec.DecodeString(name); err != nil || v != "interaction test" {
		t.Fatalf("ProductName %q (%v)", v, err)
	}

	all, err := c.Read(ctx, im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true})
	if err != nil {
		t.Fatalf("wildcard Read: %v", err)
	}
	if len(all.Attributes) < 60 {
		t.Fatalf("wildcard read returned %d attributes", len(all.Attributes))
	}
	if _, ok := all.Value(0, gencomm.ClusterID, gencomm.AttrBasicCommissioningInfo); !ok {
		t.Fatal("no BasicCommissioningInfo in the wildcard read")
	}
}
