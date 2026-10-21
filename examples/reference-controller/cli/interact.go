// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
)

// attributePaths parses attribute paths.
func attributePaths(args []string) ([]im.ConcreteAttributePath, error) {
	out := make([]im.ConcreteAttributePath, 0, len(args))
	for _, a := range args {
		p, err := parsePath(a)
		if err != nil {
			return nil, err
		}
		ap, _, err := p.attributePath()
		if err != nil {
			return nil, err
		}
		out = append(out, ap)
	}
	return out, nil
}

// eventPaths parses event paths.
func eventPaths(args []string) ([]im.ConcreteEventPath, error) {
	out := make([]im.ConcreteEventPath, 0, len(args))
	for _, a := range args {
		p, err := parsePath(a)
		if err != nil {
			return nil, err
		}
		ep, err := p.eventPath()
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, nil
}

// cmdRead reads attributes and prints one JSON line per attribute.
func cmdRead(e *env, args []string) error {
	pos, err := parseArgs(e, "read", args, nil)
	if err != nil {
		return err
	}
	if err := wantArgs("read", pos, 2, -1); err != nil {
		return err
	}
	paths, err := attributePaths(pos[1:])
	if err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, _, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: paths}, IncludeKnownVersions: true})
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		return printJSONLines(e, reportLines(rep))
	})
}

// cmdEvents reads events and prints one JSON line per event.
func cmdEvents(e *env, args []string) error {
	pos, err := parseArgs(e, "events", args, nil)
	if err != nil {
		return err
	}
	if err := wantArgs("events", pos, 2, -1); err != nil {
		return err
	}
	paths, err := eventPaths(pos[1:])
	if err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, _, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Events: paths}})
		if err != nil {
			return fmt.Errorf("events: %w", err)
		}
		return printJSONLines(e, reportLines(rep))
	})
}

// cmdWrite writes one attribute from a JSON value.
func cmdWrite(e *env, args []string) error {
	var timed durationFlag
	pos, err := parseArgs(e, "write", args, timedFlagMS(&timed))
	if err != nil {
		return err
	}
	if err := wantArgs("write", pos, 3, 3); err != nil {
		return err
	}
	p, err := parsePath(pos[1])
	if err != nil {
		return err
	}
	ap, attr, err := p.attributePath()
	if err != nil {
		return err
	}
	if !ap.HasEndpoint || !ap.HasCluster || !ap.HasAttribute {
		return errors.New("a write path is concrete: <endpoint>/<Cluster>/<Attribute>")
	}
	var field *spec.Field
	if attr != nil {
		field = spec.AttributeField(attr)
	}
	value, err := encodeArg(field, pos[2])
	if err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, _, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		st, err := n.Write(ctx, []interaction.WriteRequest{{Path: ap, Value: interaction.Raw(value)}},
			interaction.WriteOptions{Timed: timed.set, TimedTimeout: timed.d, DisableListChunking: true})
		if err != nil {
			return fmt.Errorf("write: %w", err)
		}
		if err := st.Err(); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		e.println("ok")
		return nil
	})
}

// timedFlagMS is --timed with a bare number in milliseconds (chip-tool's
// --timedInteractionTimeoutMs).
func timedFlagMS(timed *durationFlag) func(fs *flag.FlagSet) {
	return func(fs *flag.FlagSet) {
		fs.Func("timed", "a timed request with this timeout (milliseconds, or a duration)", func(s string) error {
			d, err := parseDuration(s, time.Millisecond)
			if err != nil {
				return err
			}
			timed.d, timed.set = d, true
			return nil
		})
	}
}

// invokeLine is an invoke's result as a JSON line.
type invokeLine struct {
	Status        string          `json:"status"`
	ClusterStatus *uint8          `json:"clusterStatus,omitempty"`
	Response      string          `json:"response,omitempty"`
	Fields        json.RawMessage `json:"fields,omitempty"`
}

// cmdInvoke invokes a command with its fields from a JSON object.
func cmdInvoke(e *env, args []string) error {
	var timed durationFlag
	pos, err := parseArgs(e, "invoke", args, timedFlagMS(&timed))
	if err != nil {
		return err
	}
	if err := wantArgs("invoke", pos, 2, 3); err != nil {
		return err
	}
	p, err := parsePath(pos[1])
	if err != nil {
		return err
	}
	cp, cmd, err := p.commandPath(false)
	if err != nil {
		return err
	}
	fields, err := commandFields(cmd, pos[2:])
	if err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		n, _, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		res, err := n.Invoke(ctx, cp, fields, interaction.InvokeOptions{Timed: timed.set, TimedTimeout: timed.d})
		if err != nil {
			return fmt.Errorf("invoke: %w", err)
		}
		line := invokeLine{Status: res.Status.String(), ClusterStatus: res.ClusterStatus}
		if res.HasFields {
			var field *spec.Field
			line.Response = fmt.Sprintf("0x%02X", res.Command)
			if p.def != nil {
				if rc := p.def.Command(res.Command, spec.Response); rc != nil {
					line.Response, field = rc.Name, spec.CommandField(rc)
				}
			}
			line.Fields = valueJSON(field, res.Fields)
		}
		return printJSONLines(e, []any{line})
	})
}

// commandFields encodes a command's JSON argument; none is an empty
// payload.
func commandFields(cmd *spec.Command, args []string) (spec.Encodable, error) {
	if len(args) == 0 {
		if cmd != nil {
			// A command with mandatory fields refuses an empty object
			// below, as chip-tool's EnsureMemberExist does.
			args = []string{"{}"}
		} else {
			return nil, nil
		}
	}
	var field *spec.Field
	if cmd != nil {
		field = spec.CommandField(cmd)
	}
	n, err := encodeArg(field, args[0])
	if err != nil {
		return nil, err
	}
	return interaction.Raw(n), nil
}

// cmdSubscribe subscribes to attribute and event paths and prints every
// report as JSON lines until Ctrl-C (or --count reports).
func cmdSubscribe(e *env, args []string) error {
	var minI, maxI durationFlag
	count := 0
	pos, err := parseArgs(e, "subscribe", args, func(fs *flag.FlagSet) {
		fs.Var(&minI, "min", "the minimum interval floor (seconds, or a duration; default 0)")
		fs.Var(&maxI, "max", "the maximum interval ceiling (seconds, or a duration; default 60s)")
		fs.IntVar(&count, "count", 0, "stop after this many reports (0: until Ctrl-C)")
	})
	if err != nil {
		return err
	}
	if err := wantArgs("subscribe", pos, 2, -1); err != nil {
		return err
	}
	// A path whose element names an event of its cluster is an event path.
	var attrs []im.ConcreteAttributePath
	var events []im.ConcreteEventPath
	for _, a := range pos[1:] {
		p, err := parsePath(a)
		if err != nil {
			return err
		}
		if ep, eerr := p.eventPath(); eerr == nil && ep.HasEvent && p.def != nil && p.def.AttributeByName(p.element) == nil {
			if _, nerr := parseUint(p.element, 32); nerr != nil {
				events = append(events, ep)
				continue
			}
		}
		ap, _, err := p.attributePath()
		if err != nil {
			return err
		}
		attrs = append(attrs, ap)
	}
	return withStack(e, func(s *stack) error {
		n, _, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(e.ctx)
		defer cancel()
		reports := make(chan *interaction.Report, 16)
		sctx, scancel := e.timeoutCtx()
		defer scancel()
		sub, err := n.Subscribe(sctx, interaction.SubscribeRequest{
			Attributes: attrs, Events: events,
			MinIntervalFloor:   interaction.Interval(minI.or(0)),
			MaxIntervalCeiling: interaction.Interval(maxI.or(60 * time.Second)),
			Updated: func(r *interaction.Report) {
				select {
				case reports <- r:
				case <-ctx.Done():
				}
			},
		})
		if err != nil {
			return fmt.Errorf("subscribe: %w", err)
		}
		defer sub.Close()
		e.printf("subscription %d\n", sub.ID())
		seen := 0
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-sub.Done():
				return fmt.Errorf("subscription %d ended", sub.ID())
			case r := <-reports:
				if err := printJSONLines(e, reportLines(r)); err != nil {
					return err
				}
				seen++
				if count > 0 && seen >= count {
					return nil
				}
			}
		}
	})
}
