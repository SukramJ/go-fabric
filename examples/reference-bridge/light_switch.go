// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/client/interaction"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// --light-switch (TESTING ONLY): an On/Off Light Switch for the binding
// certification cases (TC-BIND-2.1, TC-BIND-2.3), after matter.js's cert
// light switch (support/chip-testing/src/LightSwitchTestInstance.ts at
// 862ad75f): the device type with its Binding server and OnOff client,
// whose entries the node's binding manager resolves (node.Node.Bindings),
// and the app-pipe command SendOnOffToBindings that has it send OnOff On or
// Off to every target its Binding attribute names — matter.js's
// backchannel sendOnOffToBindings (packages/testing/src/device/
// backchannel.ts:161-165), which the cases' trigger(dut, "on"|"off")
// sends (support/chip-testing/test/cert/tc-bind-support.ts:600-606).
//
// The flag turns on the node's client stack (node.Config.Client) the
// switch's unicast targets are reached through; group targets go out
// through the node's group sender. Off by default, so the daemon's
// composition — and the PICS slices and goldens the chip-tool leg holds it
// to — does not move.

// lightSwitchResolutionTimeout is how long a binding entry may take to
// resolve before a send through it fails (RESOLUTION_TIMEOUT,
// LightSwitchTestInstance.ts:38-39).
const lightSwitchResolutionTimeout = 30 * time.Second

// lightSwitchResolutionPoll is the poll of an unresolved entry
// (RESOLUTION_POLL, LightSwitchTestInstance.ts:41).
const lightSwitchResolutionPoll = 50 * time.Millisecond

// demoLightSwitch is the switch: its device, and the resolutions the
// node's binding manager reported for it (#resolved,
// LightSwitchTestInstance.ts:79-80).
type demoLightSwitch struct {
	dev    *device.OnOffLightSwitch
	logger *slog.Logger

	mu       sync.Mutex
	attached bool
	resolved map[resolutionKey]binding.Resolution
}

// resolutionKey is a resolution's source endpoint and entry.
type resolutionKey struct {
	source uint16
	entry  string
}

func newDemoLightSwitch(f *fleet, logger *slog.Logger) (*demoLightSwitch, error) {
	dev, err := device.NewOnOffLightSwitch(f.bridged("lightswitch", "Light Switch"))
	if err != nil {
		return nil, fmt.Errorf("light switch: %w", err)
	}
	return &demoLightSwitch{dev: dev, logger: logger, resolved: map[resolutionKey]binding.Resolution{}}, nil
}

// sendOnOff is #sendOnOffToBindings (LightSwitchTestInstance.ts:181-210):
// the command to every resolved target the switch's Binding attribute
// names for OnOff, each failure collected.
func (s *demoLightSwitch) sendOnOff(ctx context.Context, endpointID uint16, command string) error {
	if s == nil {
		return errors.New("SendOnOffToBindings needs --light-switch")
	}
	ep, ok := s.dev.EndpointNumber()
	if !ok {
		return errors.New("the light switch has no endpoint yet")
	}
	if endpointID != ep {
		return fmt.Errorf("the light switch sends bound commands from endpoint %d, not %d", ep, endpointID)
	}
	var cmd uint32
	switch command {
	case "on":
		cmd = onoff.CmdOn
	case "off":
		cmd = onoff.CmdOff
	default:
		return fmt.Errorf("SendOnOffToBindings: Command %q is neither on nor off", command)
	}
	targets, err := s.resolveTargets(ctx, ep)
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range targets {
		s.logger.Info("lightswitch.send", slog.String("command", command), slog.String("kind", r.Kind.String()),
			slog.String("entry", entryKey(r.Entry)))
		if err := sendTo(ctx, r, cmd); err != nil {
			failures = append(failures, fmt.Errorf("OnOff.%s to binding target %s: %w", command, entryKey(r.Entry), err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("OnOff.%s failed for %d of %d binding targets: %w", command, len(failures), len(targets), errors.Join(failures...))
	}
	return nil
}

// sendTo invokes cmd on one resolved target: a client's endpoint through
// its interaction, a group through the group sender.
func sendTo(ctx context.Context, r binding.Resolution, cmd uint32) error {
	path := im.ConcreteCommandPath{Endpoint: r.Endpoint, Cluster: onoff.ClusterID, Command: cmd}
	switch r.Kind {
	case binding.KindClient:
		res, err := r.Interaction.Invoke(ctx, path, nil, interaction.InvokeOptions{})
		if err != nil {
			return err
		}
		if res.Status != im.StatusSuccess {
			return fmt.Errorf("status %v", res.Status)
		}
		return nil
	case binding.KindGroup:
		return r.Group.Invoke(ctx, path, nil)
	default:
		return fmt.Errorf("a %s binding is not one the switch sends to", r.Kind)
	}
}

// resolveTargets is #resolveTargets (LightSwitchTestInstance.ts:216-246):
// the resolutions of every entry for OnOff (or for no cluster), waiting
// until each resolved, at most [lightSwitchResolutionTimeout].
func (s *demoLightSwitch) resolveTargets(ctx context.Context, ep uint16) ([]binding.Resolution, error) {
	b, ok := s.dev.Server(bindingdef.ClusterID).(*mattercore.Binding)
	s.mu.Lock()
	attached := s.attached
	s.mu.Unlock()
	if !ok || !attached {
		return nil, errors.New("the light switch has no binding manager")
	}
	deadline := time.Now().Add(lightSwitchResolutionTimeout)
	for {
		resolved := map[string]binding.Resolution{}
		s.mu.Lock()
		for k, r := range s.resolved {
			if k.source == ep {
				resolved[k.entry] = r
			}
		}
		s.mu.Unlock()
		var targets []binding.Resolution
		var unresolved []string
		for _, e := range b.Entries() {
			if e.Cluster != nil && *e.Cluster != onoff.ClusterID {
				continue
			}
			key := defEntryKey(e)
			if r, ok := resolved[key]; ok {
				targets = append(targets, r)
			} else {
				unresolved = append(unresolved, key)
			}
		}
		if len(unresolved) == 0 {
			return targets, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("binding entries %s did not resolve within %s", strings.Join(unresolved, ", "), lightSwitchResolutionTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lightSwitchResolutionPoll):
		}
	}
}

// entryKey identifies a binding entry by every field that makes it a
// distinct target (entryKey, LightSwitchTestInstance.ts:65-68).
func entryKey(e mattercore.TargetStruct) string {
	return keyOf(e.FabricIndex, e.Node != 0, e.Node, e.Group != 0, e.Group, e.Endpoint != 0 || e.EndpointPresent, e.Endpoint,
		e.Cluster != 0 || e.ClusterPresent, e.Cluster)
}

func defEntryKey(e bindingdef.TargetStruct) string {
	var nodeID uint64
	var group, ep uint16
	var cluster uint32
	if e.Node != nil {
		nodeID = *e.Node
	}
	if e.Group != nil {
		group = *e.Group
	}
	if e.Endpoint != nil {
		ep = *e.Endpoint
	}
	if e.Cluster != nil {
		cluster = *e.Cluster
	}
	return keyOf(e.FabricIndex, e.Node != nil, nodeID, e.Group != nil, group, e.Endpoint != nil, ep, e.Cluster != nil, cluster)
}

func keyOf(fabric uint8, hasNode bool, nodeID uint64, hasGroup bool, group uint16, hasEP bool, ep uint16, hasCluster bool, cluster uint32) string {
	field := func(has bool, v uint64) string {
		if !has {
			return ""
		}
		return strconv.FormatUint(v, 10)
	}
	return strings.Join([]string{
		strconv.Itoa(int(fabric)), field(hasNode, nodeID), field(hasGroup, uint64(group)), field(hasEP, uint64(ep)), field(hasCluster, uint64(cluster)),
	}, "/")
}

// attachLightSwitch observes the node's binding manager for the switch,
// as the cert light switch observes its BindingServer's established and
// removed events (LightSwitchTestInstance.ts:148-157). Observing is what
// has the manager resolve the entries at all: an entry nothing observes
// waits dormant (binding.Manager.OnEstablished).
func attachLightSwitch(s *demoLightSwitch, n *node.Node) {
	if s == nil || n.Bindings() == nil {
		return
	}
	m := n.Bindings()
	m.OnEstablished(func(r binding.Resolution) {
		s.mu.Lock()
		s.resolved[resolutionKey{r.SourceEndpoint, entryKey(r.Entry)}] = r
		s.mu.Unlock()
	})
	m.OnRemoved(func(r binding.Resolution) {
		k := resolutionKey{r.SourceEndpoint, entryKey(r.Entry)}
		s.mu.Lock()
		// Only the resolution it replaced: a later one for the same entry
		// stays (:152-157); a group resolution carries no peer to tell.
		if cur, ok := s.resolved[k]; ok && cur.Kind == r.Kind && cur.Peer == r.Peer {
			delete(s.resolved, k)
		}
		s.mu.Unlock()
	})
	s.mu.Lock()
	s.attached = true
	s.mu.Unlock()
}
