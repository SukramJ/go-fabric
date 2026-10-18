// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
)

// The wire helpers: a test speaks to a commissioned pair's device over the
// wire through the pair's controller, one interaction per call, after
// matter.js's node-helpers.ts `interaction` namespace
// (packages/node/src/testing/node-helpers.ts: connect :370, write :381,
// read :400, invoke :427, subscribe :465). matter.js's helpers call the
// device's InteractionServer directly over a mock exchange; these send the
// messages over the site's network, so the device's whole receive path —
// session, MRP, the IM gates, the dispatcher, ACL — is in the test. Every
// helper runs under [Site.Run], so a fake site clock moves when the
// interaction waits on a timer. A status other than Success is returned
// as an *interaction.StatusError carrying the IM status (and the path, for
// an attribute or event).

// ErrNotCommissioned is returned by the wire helpers for a pair whose
// device is not commissioned.
var ErrNotCommissioned = errors.New("fabrictest: the pair is not commissioned")

// run runs fn under the pair's site's clock.
func run(ctx context.Context, p *Pair, fn func(ctx context.Context) error) error {
	if p == nil || (!p.Commissioned() && p.PASESession == nil) {
		return ErrNotCommissioned
	}
	return p.Controller.site.Run(ctx, fn)
}

// Connect returns the controller's interaction with the pair's device,
// opening a CASE session first when the peer holds none —
// node-helpers.ts interaction.connect (:370), which creates a secure
// session on the node.
func Connect(ctx context.Context, p *Pair) (*interaction.Interaction, error) {
	err := run(ctx, p, func(ctx context.Context) error {
		if p.Peer == nil || p.Peer.HasSession() {
			return nil
		}
		_, err := p.Peer.Connect(ctx, peer.ConnectOptions{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return p.Interaction, nil
}

// Read reads one attribute and returns its data — node-helpers.ts
// interaction.read (:400), which returns the first attribute data's
// payload. The read is fabric-filtered. An attribute status in place of
// the data is an *interaction.StatusError with the path.
func Read(ctx context.Context, p *Pair, path im.ConcreteAttributePath) (interaction.AttributeData, error) {
	r, err := ReadWith(ctx, p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}})
	if err != nil {
		return interaction.AttributeData{}, err
	}
	for _, st := range r.Statuses {
		if st.Status != im.StatusSuccess {
			at := st.Path
			return interaction.AttributeData{}, &interaction.StatusError{Status: st.Status, ClusterStatus: st.ClusterStatus, Path: &at}
		}
	}
	if len(r.Attributes) == 0 {
		return interaction.AttributeData{}, &interaction.StatusError{Status: im.StatusUnsupportedAttribute, Path: &path}
	}
	return r.Attributes[0], nil
}

// ReadWith runs one read request — attribute and event paths, wildcards
// and filters — and returns the whole report.
func ReadWith(ctx context.Context, p *Pair, req interaction.ReadRequest) (*interaction.Report, error) {
	var r *interaction.Report
	err := run(ctx, p, func(ctx context.Context) error {
		var err error
		r, err = p.Interaction.ReadWith(ctx, req)
		return err
	})
	return r, err
}

// Events reads the events of paths and returns them in report order. An
// event status other than Success is an *interaction.StatusError; its Path
// is nil, the event path is in the error's text.
func Events(ctx context.Context, p *Pair, paths ...im.ConcreteEventPath) ([]interaction.EventData, error) {
	r, err := ReadWith(ctx, p, interaction.ReadRequest{Events: paths})
	if err != nil {
		return nil, err
	}
	for _, st := range r.EventStatuses {
		if st.Status != im.StatusSuccess {
			return r.Events, &eventStatusError{StatusError: interaction.StatusError{Status: st.Status, ClusterStatus: st.ClusterStatus}, path: st.Path}
		}
	}
	return r.Events, nil
}

// eventStatusError is an event path's status.
type eventStatusError struct {
	interaction.StatusError
	path im.ConcreteEventPath
}

func (e *eventStatusError) Error() string {
	return e.StatusError.Error() + " for event " + eventPathString(e.path)
}

// Unwrap lets errors.As find the *interaction.StatusError.
func (e *eventStatusError) Unwrap() error { return &e.StatusError }

// Write writes one attribute — node-helpers.ts interaction.write (:381), a
// single-item write request. The path's status other than Success is an
// *interaction.StatusError with the path.
func Write(ctx context.Context, p *Pair, path im.ConcreteAttributePath, value spec.Encodable) error {
	return write(ctx, p, path, value, interaction.WriteOptions{})
}

// WriteTimed is [Write] as a timed write: a TimedRequest first.
func WriteTimed(ctx context.Context, p *Pair, path im.ConcreteAttributePath, value spec.Encodable) error {
	return write(ctx, p, path, value, interaction.WriteOptions{Timed: true})
}

func write(ctx context.Context, p *Pair, path im.ConcreteAttributePath, value spec.Encodable, opts interaction.WriteOptions) error {
	return run(ctx, p, func(ctx context.Context) error {
		res, err := p.Interaction.Write(ctx, []interaction.WriteRequest{{Path: path, Value: value}}, opts)
		if err != nil {
			return err
		}
		return res.Err()
	})
}

// Invoke invokes one command and returns the device's answer —
// node-helpers.ts interaction.invoke (:427), which hands the first invoke
// response to its responder. fields is the request payload, nil for an
// empty one; a command status other than Success is an
// *interaction.StatusError.
func Invoke(ctx context.Context, p *Pair, path im.ConcreteCommandPath, fields spec.Encodable) (interaction.Result, error) {
	return invoke(ctx, p, path, fields, interaction.InvokeOptions{})
}

// InvokeTimed is [Invoke] as a timed invoke — interaction.invoke with
// options.timed (:427).
func InvokeTimed(ctx context.Context, p *Pair, path im.ConcreteCommandPath, fields spec.Encodable) (interaction.Result, error) {
	return invoke(ctx, p, path, fields, interaction.InvokeOptions{Timed: true})
}

func invoke(ctx context.Context, p *Pair, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
	var res interaction.Result
	err := run(ctx, p, func(ctx context.Context) error {
		var err error
		res, err = p.Interaction.Invoke(ctx, path, fields, opts)
		return err
	})
	return res, err
}

// Subscribe subscribes and returns once the device has answered with its
// priming report and SubscribeResponse — node-helpers.ts
// interaction.subscribe (:465). The reports that follow arrive through
// req.Updated as the site's clock reaches the subscription's intervals.
func Subscribe(ctx context.Context, p *Pair, req interaction.SubscribeRequest) (*interaction.Subscription, error) {
	var sub *interaction.Subscription
	err := run(ctx, p, func(ctx context.Context) error {
		var err error
		sub, err = p.Interaction.Subscribe(ctx, req)
		return err
	})
	return sub, err
}

func eventPathString(p im.ConcreteEventPath) string {
	return fmt.Sprintf("%d/0x%04X/0x%02X", p.Endpoint, p.Cluster, p.Event)
}
