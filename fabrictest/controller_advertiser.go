// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/mdns"
)

// controllerAdvertiser records what a controller built on
// [Site.ControllerConfig] publishes — its commissioner record and, with a
// served tree, the operational record of its root node — so that the site
// answers for it: a device resolves the controller's operational address
// ([siteResolver]) the way it resolves a device's, and a DNS-SD query of a
// controller's browser sees the records. The real network's responder is
// what [mdns.NewResponder] gives a program.
type controllerAdvertiser struct {
	addr  *net.UDPAddr
	index int

	mu       sync.Mutex
	services []mdns.Service
}

var _ mdns.Advertiser = (*controllerAdvertiser)(nil)

func (a *controllerAdvertiser) Publish(_ context.Context, svc mdns.Service) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.services {
		if a.services[i].InstanceName == svc.InstanceName && a.services[i].ServiceType == svc.ServiceType {
			a.services[i] = svc
			return nil
		}
	}
	a.services = append(a.services, svc)
	return nil
}

func (a *controllerAdvertiser) Withdraw(_ context.Context, instanceName, serviceType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.services {
		if a.services[i].InstanceName == instanceName && a.services[i].ServiceType == serviceType {
			a.services = append(a.services[:i], a.services[i+1:]...)
			return nil
		}
	}
	return nil
}

func (a *controllerAdvertiser) Active() []mdns.Service {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]mdns.Service(nil), a.services...)
}

func (a *controllerAdvertiser) Close() error {
	a.mu.Lock()
	a.services = nil
	a.mu.Unlock()
	return nil
}

// answersOperational reports whether the controller published the
// operational record of nodeID on compressedFabricID.
func (a *controllerAdvertiser) answersOperational(compressedFabricID [8]byte, nodeID uint64) bool {
	want := fmt.Sprintf("%016X-%016X", compressedFabricID, nodeID)
	services := a.Active()
	for i := range services {
		if services[i].ServiceType == mdns.ServiceTypeOperational && strings.EqualFold(services[i].InstanceName, want) {
			return true
		}
	}
	return false
}

// controllerAdvertiser returns the advertiser a controller of index on
// addr publishes through, kept by the site.
func (s *Site) controllerAdvertiser(index int, addr *net.UDPAddr) *controllerAdvertiser {
	a := &controllerAdvertiser{addr: addr, index: index}
	s.advMu.Lock()
	s.advertisers = append(s.advertisers, a)
	s.advMu.Unlock()
	return a
}

func (s *Site) controllerAdvertisers() []*controllerAdvertiser {
	s.advMu.Lock()
	defer s.advMu.Unlock()
	return append([]*controllerAdvertiser(nil), s.advertisers...)
}
