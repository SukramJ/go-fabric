// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
)

const defaultTimeout = 60 * time.Second

// pairing is what a code or the passcode and discriminator flags give.
type pairing struct {
	passcode uint32
	id       discovery.Identifier
	what     string
}

// parseCode reads a QR code ("MT:…") or a manual code (11 or 21 digits).
func parseCode(code string) (pairing, error) {
	if strings.HasPrefix(code, "MT:") {
		p, err := setup.DecodeQRCode(code)
		if err != nil {
			return pairing{}, err
		}
		return pairing{passcode: p.Passcode, id: discovery.ByLongDiscriminator(p.Discriminator), what: fmt.Sprintf("long discriminator %d", p.Discriminator)}, nil
	}
	m, err := setup.DecodeManualCode(code)
	if err != nil {
		return pairing{}, err
	}
	return pairing{passcode: m.Passcode, id: discovery.ByShortDiscriminator(m.ShortDiscriminator), what: fmt.Sprintf("short discriminator %d", m.ShortDiscriminator)}, nil
}

// networkOptions are the operational network the device is to join
// (commissioner.Options.WiFiNetwork, ThreadNetwork): the flow's network
// steps configure it (ControllerCommissioningFlow.ts:1527-1958) when the
// device advertises Wi-Fi or Thread.
func networkOptions(ssid, credentials, dataset string) (*commissioner.WiFiNetwork, *commissioner.ThreadNetwork, error) {
	var wifi *commissioner.WiFiNetwork
	var thread *commissioner.ThreadNetwork
	if ssid != "" {
		wifi = &commissioner.WiFiNetwork{SSID: ssid, Credentials: credentials}
	} else if credentials != "" {
		return nil, nil, errors.New("--wifi-credentials needs --wifi-ssid")
	}
	if dataset != "" {
		b, err := hex.DecodeString(dataset)
		if err != nil || len(b) == 0 {
			return nil, nil, fmt.Errorf("--thread-dataset is not a hex operational dataset: %w", err)
		}
		thread = &commissioner.ThreadNetwork{OperationalDataset: b}
	}
	return wifi, thread, nil
}

// cmdPair: discovery over DNS-SD, PASE, attestation, NOC, the registry's
// record — the flow of node/binding_live_test.go:392-410 (ForDevice and
// Commission) after the scanner found the device by its discriminator.
// --wifi-ssid / --wifi-credentials and --thread-dataset give the
// operational network the flow configures on a Wi-Fi or Thread device.
func cmdPair(e *env, args []string) error {
	var code, label string
	var passcode, discriminator uint
	var nodeIDArg string
	var wifiSSID, wifiCredentials, threadDataset string
	pos, err := parseArgs(e, "pair", args, func(fs *flag.FlagSet) {
		fs.StringVar(&wifiSSID, "wifi-ssid", "", "the SSID of the Wi-Fi network the device is to join")
		fs.StringVar(&wifiCredentials, "wifi-credentials", "", "the Wi-Fi network's passphrase (with --wifi-ssid)")
		fs.StringVar(&threadDataset, "thread-dataset", "", "the Thread operational dataset the device is to join, as hex")
		fs.StringVar(&code, "code", "", "the QR code (MT:…) or the manual code (11 or 21 digits)")
		fs.UintVar(&passcode, "passcode", 0, "the setup passcode (with --discriminator, instead of --code)")
		fs.UintVar(&discriminator, "discriminator", 0, "the 12-bit long discriminator")
		fs.StringVar(&nodeIDArg, "node-id", "", "the node id to assign (default random)")
		fs.StringVar(&label, "label", "", "writes the node's BasicInformation.NodeLabel after pairing")
	})
	if err != nil {
		return err
	}
	if err := wantArgs("pair", pos, 0, 0); err != nil {
		return err
	}
	var pr pairing
	switch {
	case code != "":
		if pr, err = parseCode(code); err != nil {
			return err
		}
	case passcode != 0:
		if discriminator > setup.MaxDiscriminator || passcode > 0x7FFFFFF {
			return errors.New("--discriminator or --passcode out of range")
		}
		if !setup.IsValidPasscode(uint32(passcode)) {
			return fmt.Errorf("--passcode %d is not a valid setup passcode", passcode)
		}
		pr = pairing{passcode: uint32(passcode), id: discovery.ByLongDiscriminator(uint16(discriminator)), what: fmt.Sprintf("long discriminator %d", discriminator)}
	default:
		return fmt.Errorf("usage: %s", commands["pair"].usage)
	}
	wifi, thread, err := networkOptions(wifiSSID, wifiCredentials, threadDataset)
	if err != nil {
		return err
	}
	var nodeID uint64
	if nodeIDArg != "" {
		if nodeID, err = parseNodeID(nodeIDArg); err != nil {
			return err
		}
	}
	e.attests = true
	return withStack(e, func(s *stack) error {
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		dev, err := findCommissionable(ctx, s, pr)
		if err != nil {
			return err
		}
		n, err := s.reg.ForDevice(ctx, dev)
		if err != nil {
			return err
		}
		noSubscribe := false
		if err := s.reg.Commission(ctx, n, client.CommissionOptions{
			Options: commissioner.Options{
				Passcode: pr.passcode, NodeID: nodeID, OnAttestationFailure: s.policy,
				WiFiNetwork: wifi, ThreadNetwork: thread,
			},
			AutoSubscribe: &noSubscribe,
		}); err != nil {
			return fmt.Errorf("pairing: %w", err)
		}
		addr, _ := n.PeerAddress()
		if label != "" {
			if err := setNodeLabel(ctx, n, label); err != nil {
				e.log.Warn("controller.pair.label", "err", err)
			}
		}
		e.printf("paired node %d (0x%016X) as %s\n", addr.NodeID, addr.NodeID, n.ID())
		printDevice(e, n)
		return nil
	})
}

// findCommissionable runs the scanner until the first device of the
// identifier answers.
func findCommissionable(ctx context.Context, s *stack, pr pairing) (discovery.Device, error) {
	sc := discovery.NewScanner(s.names)
	defer sc.Close()
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var found *discovery.Device
	_, err := sc.FindCommissionable(fctx, pr.id, func(d discovery.Device) {
		if found == nil && len(d.Addresses) > 0 {
			found = &d
			cancel()
		}
	})
	if found != nil {
		return *found, nil
	}
	if err == nil {
		err = context.DeadlineExceeded
	}
	return discovery.Device{}, fmt.Errorf("no commissionable device with %s answered: %w", pr.what, err)
}

// setNodeLabel writes BasicInformation.NodeLabel.
func setNodeLabel(ctx context.Context, n *client.Node, label string) error {
	v, err := encodeArg(spec.AttributeField(basicinfo.Definition.Attribute(basicinfo.AttrNodeLabel)), fmt.Sprintf("%q", label))
	if err != nil {
		return err
	}
	st, err := n.Write(ctx, []interaction.WriteRequest{{
		Path:  im.ConcreteAttributePath{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		Value: interaction.Raw(v),
	}}, interaction.WriteOptions{})
	if err != nil {
		return err
	}
	return st.Err()
}

// printDevice prints the device view of the node's image: each endpoint,
// its device type and its clusters.
func printDevice(e *env, n *client.Node) {
	eps := n.Endpoints()
	sort.Slice(eps, func(i, j int) bool { return eps[i].Number() < eps[j].Number() })
	for _, ep := range eps {
		kind := fmt.Sprintf("device type 0x%04X", ep.DeviceType())
		if d := ep.Device(); d != nil {
			kind = fmt.Sprintf("%s (0x%04X)", d.Name, d.ID)
		}
		names := make([]string, 0, len(ep.Clusters()))
		for _, c := range ep.Clusters() {
			names = append(names, c.Name())
		}
		sort.Strings(names)
		e.printf("  endpoint %d: %s: %s\n", ep.Number(), kind, strings.Join(names, ", "))
	}
}

// cmdUnpair: RemoveFabric on the node, then the registry's record is
// deleted (client.Registry.Decommission).
func cmdUnpair(e *env, args []string) error {
	pos, err := parseArgs(e, "unpair", args, nil)
	if err != nil {
		return err
	}
	if err := wantArgs("unpair", pos, 1, 1); err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, id, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		if err := s.reg.Decommission(ctx, n); err != nil {
			return fmt.Errorf("unpair: %w", err)
		}
		e.printf("unpaired node %d (0x%016X)\n", id, id)
		return nil
	})
}

// verdict is the node's conformance: the worst of its endpoints'.
func verdict(n *client.Node) client.ConformanceStatus {
	worst := client.ConformanceClean
	rank := map[client.ConformanceStatus]int{client.ConformanceClean: 0, client.ConformanceUnknown: 1, client.ConformanceIncomplete: 2, client.ConformanceViolated: 3}
	eps := n.Endpoints()
	if len(eps) == 0 {
		return client.ConformanceUnknown
	}
	for _, ep := range eps {
		if st := ep.Conformance().Status; rank[st] > rank[worst] {
			worst = st
		}
	}
	return worst
}

// cmdNodes lists every record with reachability (a read of
// BasicInformation.NodeLabel within --timeout, 5s here by default) and the
// conformance verdict of its image.
func cmdNodes(e *env, args []string) error {
	pos, err := parseArgs(e, "nodes", args, nil)
	if err != nil {
		return err
	}
	if err := wantArgs("nodes", pos, 0, 0); err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		nodes := s.reg.Nodes()
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID() < nodes[j].ID() })
		if len(nodes) == 0 {
			e.println("no paired nodes")
		}
		for _, n := range nodes {
			addr, ok := n.PeerAddress()
			if !ok {
				e.printf("%s: not commissioned\n", n.ID())
				continue
			}
			reach := "reachable"
			ctx, cancel := context.WithTimeout(e.ctx, e.opts.timeout.or(5*time.Second))
			label, err := readNodeLabel(ctx, n)
			cancel()
			if err != nil {
				reach = "unreachable (" + err.Error() + ")"
			}
			e.printf("%d (0x%016X) %s label=%q %s conformance=%s\n", addr.NodeID, addr.NodeID, n.ID(), label, reach, verdict(n))
		}
		return nil
	})
}

func readNodeLabel(ctx context.Context, n *client.Node) (string, error) {
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{
		{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true},
	}}})
	if err != nil {
		return "", err
	}
	v, ok := rep.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	if !ok {
		return "", errors.New("no NodeLabel")
	}
	return spec.DecodeString(v)
}

// cmdNode prints the node's full image after a wildcard read: every
// endpoint, its device type and conformance, every cluster and every
// attribute value.
func cmdNode(e *env, args []string) error {
	pos, err := parseArgs(e, "node", args, nil)
	if err != nil {
		return err
	}
	if err := wantArgs("node", pos, 1, 1); err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, id, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		if _, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}}, IncludeKnownVersions: true}); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		e.printf("node %d (0x%016X) %s conformance=%s\n", id, id, n.ID(), verdict(n))
		eps := n.Endpoints()
		sort.Slice(eps, func(i, j int) bool { return eps[i].Number() < eps[j].Number() })
		for _, ep := range eps {
			kind := fmt.Sprintf("0x%04X", ep.DeviceType())
			if d := ep.Device(); d != nil {
				kind = fmt.Sprintf("%s (0x%04X)", d.Name, d.ID)
			}
			e.printf("endpoint %d: %s conformance=%s\n", ep.Number(), kind, ep.Conformance().Status)
			cls := ep.Clusters()
			sort.Slice(cls, func(i, j int) bool { return cls[i].ID() < cls[j].ID() })
			for _, c := range cls {
				e.printf("  %s (0x%04X) revision %d\n", c.Name(), c.ID(), c.Revision())
				for _, attr := range c.AttributeList() {
					v, ok := c.Value(attr)
					if !ok {
						continue
					}
					name, a := attributeName(c.Definition(), attr)
					var field *spec.Field
					if a != nil {
						field = spec.AttributeField(a)
					}
					e.printf("    %s = %s\n", name, valueJSON(field, v))
				}
			}
		}
		return nil
	})
}
