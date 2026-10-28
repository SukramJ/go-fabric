// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// ErrNoCommands is an [Interaction.InvokeBatch] of no command — matter.js
// Invoke's MalformedRequestError "Invocation requires at least one
// command" (packages/protocol/src/action/request/Invoke.ts:116-118 at
// 862ad75f).
var ErrNoCommands = errors.New("interaction: an invoke needs at least one command")

// CommandRequest is one command of an [Interaction.InvokeBatch]: its path
// and its request fields (a generated request's value, nil for an empty
// payload).
type CommandRequest struct {
	Path   im.ConcreteCommandPath
	Fields spec.Encodable
}

// InvokeBatch invokes cmds on the peer and returns one [Result] per
// command, in the order of cmds. A command's own status — a
// CommandStatusIB other than Success, or NoCommandResponse for a command
// the peer answered nothing for — is its Result's Status, no error; the
// error is the exchange layer's, of every batch that failed. With
// opts.SuppressResponse the Results are zero.
//
// The commands go in batches of at most maxPaths (the peer's
// MaxPathsPerInvoke; at least 1), each on an exchange of its own, the
// batches in parallel — matter.js ClientInteraction.invoke and
// #invokeWithSplitting (packages/protocol/src/action/client/ClientInteraction.ts:654-737
// at 862ad75f; a batch size below one is an error, :662-664). A batch of
// several commands numbers them with a CommandRef and matches the
// responses by it; a batch of one sends no CommandRef and takes the
// responses by position (:510-517); a response for no command of the batch
// is dropped, and a command without a response is NoCommandResponse
// (:616-646). The batching of single commands across one batch window
// (#invokeWithBatching) is [Interaction.Invoke]'s, no part of it: the
// caller's batch is the batch.
func (c *Interaction) InvokeBatch(ctx context.Context, cmds []CommandRequest, maxPaths int, opts InvokeOptions) ([]Result, error) {
	if len(cmds) == 0 {
		return nil, ErrNoCommands
	}
	if maxPaths < 1 {
		return nil, fmt.Errorf("interaction: cannot split an invoke into batches of %d paths", maxPaths)
	}
	// The inference covers the whole request before it is split, so every
	// part carries it (ClientInteraction.ts:716-737).
	paths := make([]im.ConcreteCommandPath, len(cmds))
	for i, cmd := range cmds {
		paths[i] = cmd.Path
	}
	opts = opts.inferLargeMessage(paths...)
	results := make([]Result, len(cmds))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for start := 0; start < len(cmds); start += maxPaths {
		end := min(start+maxPaths, len(cmds))
		wg.Go(func() {
			refs := make([]uint16, end-start)
			for i := range refs {
				refs[i] = uint16(start + i) //nolint:gosec // a batch holds at most MaxPathsPerInvoke commands
			}
			answered := make([]bool, end-start)
			err := c.invokeBatch(ctx, cmds[start:end], refs, results[start:end], answered, opts)
			for i, ok := range answered {
				if !ok && err == nil && !opts.SuppressResponse {
					results[start+i] = Result{Status: im.StatusNoCommandResponse}
				}
			}
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return results, fmt.Errorf("interaction: one or more invoke batches failed: %w", errors.Join(errs...))
	}
	return results, nil
}

// invokeBatch sends one batch — cmds, numbered by refs when it holds
// several — and fills out with the Result of every command the peer
// answered, marking it in answered; a batch of one goes without its
// CommandRef and takes the answer by position (ClientInteraction.ts:510-517).
func (c *Interaction) invokeBatch(ctx context.Context, cmds []CommandRequest, refs []uint16, out []Result, answered []bool, opts InvokeOptions) error {
	multiple := len(cmds) > 1
	invokes := make([]im.CommandInvocation, len(cmds))
	index := make(map[uint16]int, len(cmds))
	for i, cmd := range cmds {
		path := cmd.Path
		path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
		invokes[i] = im.CommandInvocation{Path: path, Fields: cmd.Fields}
		if multiple {
			invokes[i].CommandRef, invokes[i].HasCommandRef = refs[i], true
			index[refs[i]] = i
		}
	}
	entries, err := c.exchangeInvoke(ctx, invokes, opts)
	if err != nil || opts.SuppressResponse {
		return err
	}
	for _, e := range entries {
		i := 0
		if multiple {
			var ok bool
			i, ok = index[e.CommandRef]
			if !e.HasCommandRef || !ok {
				c.log.Warn("matter.controller.interaction.invoke_response_discarded",
					slog.Uint64("endpoint", uint64(e.Path.Endpoint)), slog.Uint64("cluster", uint64(e.Path.Cluster)), slog.Uint64("command", uint64(e.Path.Command)))
				continue
			}
		}
		if answered[i] {
			continue
		}
		out[i], answered[i] = resultOf(e), true
	}
	return nil
}
