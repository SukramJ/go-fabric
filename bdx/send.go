// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
)

// DefaultMaxTransferSize is the largest transfer a sender offers: matter.js
// BdxSessionConfiguration.DefaultConfig.maxTransferSize
// (BdxSessionConfiguration.ts:110, `1_024 * 100_000`, "100 MB").
const DefaultMaxTransferSize = 1_024 * 100_000

// SendOptions configures [Send] and [StartSend].
type SendOptions struct {
	// TransferFileDesignator names the file in the SendInit (matter.js
	// `transferFileDesignator`).
	TransferFileDesignator FileDesignator
	// Data is the content, read block by block.
	Data io.Reader
	// Length is the content's size when it is known up front. matter.js
	// always states it: its sender reads a stored blob and offers its size
	// as the SendInit's definite length (bdx-session-initiator.ts:126-153,
	// buildInitMessage). nil offers an indefinite length — what chip's
	// DiagnosticLogs sender does, which streams a log whose end it learns
	// as it reads (BDXDiagnosticLogsProvider.cpp:72-76: no MaxLength in
	// the TransferInitData).
	Length *uint64
	// PreferredDriverModes are the modes offered, in order of preference;
	// empty means matter.js's default [SenderDrive, ReceiverDrive]
	// (BdxSessionConfiguration.ts:104-111).
	PreferredDriverModes []DriverMode
	// MaxBlockSize, when set, lowers the offered block size below what the
	// exchange carries (bdx-session-initiator.ts:155-165).
	MaxBlockSize uint16
	// MaxTransferSize refuses a Length above it; zero is
	// [DefaultMaxTransferSize] (bdx-session-initiator.ts:137-143).
	MaxTransferSize uint64
	// StartOffset, when set, is stated in the SendInit as the offset in
	// the file Data begins at (matter.js senderStartOffset); Data must
	// already start there. Mirrors matter.js
	// packages/protocol/src/bdx/bdx-session-initiator.ts:128-135 and :167-178.
	StartOffset *uint64
	// Progress, when set, is told the bytes transferred and the transfer's
	// size (flow/Flow.ts:121-147, progressInfo).
	Progress func(transferred uint64, total *uint64)
}

// Transfer is an outbound transfer this node initiated whose SendInit the
// peer accepted: [StartSend] negotiated it, [Transfer.Run] moves the data.
// The split exists for a caller that must act between the two, as chip's
// DiagnosticLogs server answers its command once the SendAccept arrived
// and only then streams (BDXDiagnosticLogsProvider.cpp:114-118, :167-174).
type Transfer struct {
	f        *outboundFlow
	isDriver bool
	res      Result
}

// Result is what the transfer settled on; Transferred is still zero.
func (t *Transfer) Result() Result { return t.res }

// Send runs one outbound transfer this node initiates: SendInit, the
// peer's SendAccept, then the blocks — pushed one by one and each
// acknowledged when this node drives, served per BlockQuery when the peer
// does — until BlockEof, answered with BlockAckEof.
//
// A BDX [Error] sends the peer a failure StatusReport with its code and is
// returned with the bytes transferred filled in; any other error is
// returned as it is. The exchange is left open for the caller to close.
//
// Mirrors matter.js BdxClient.asSender → BdxSession.processTransfer
// (BdxSession.ts:154-200) with bdxSessionInitiator's initiator/sender
// branch (bdx-session-initiator.ts:35-45, buildInitMessage :114-179,
// collectAcceptParameters :224-268) and the two outbound flows,
// DrivenSendingFlow (sender drive) and FollowingReceivingFlow (receiver
// drive).
func Send(ctx context.Context, m *Messenger, opts SendOptions) (Result, error) {
	t, err := StartSend(ctx, m, opts)
	if err != nil {
		return Result{}, err
	}
	return t.Run(ctx)
}

// StartSend is the first half of [Send]: SendInit and the peer's
// SendAccept. A failure is handled as [Send] handles it.
func StartSend(ctx context.Context, m *Messenger, opts SendOptions) (*Transfer, error) {
	if opts.Data == nil {
		return nil, errors.New("bdx: send needs data")
	}
	modes := opts.PreferredDriverModes
	if len(modes) == 0 {
		modes = []DriverMode{SenderDrive, ReceiverDrive}
	}
	f := &outboundFlow{m: m, data: opts.Data, counter: -1}
	t, err := startSend(ctx, f, modes, opts)
	if err != nil {
		return nil, f.fail(ctx, err)
	}
	f.hooks = flowHooks{progress: opts.Progress}
	return t, nil
}

func startSend(ctx context.Context, f *outboundFlow, modes []DriverMode, opts SendOptions) (*Transfer, error) {
	// buildInitMessage, sender branch (bdx-session-initiator.ts:126-153).
	var maxLength *uint64
	if opts.Length != nil {
		limit := opts.MaxTransferSize
		if limit == 0 {
			limit = DefaultMaxTransferSize
		}
		if *opts.Length > limit {
			return nil, fmt.Errorf("bdx: requested maxLength %dbytes exceeds maximum transfer size %dbytes", *opts.Length, limit)
		}
		l := *opts.Length
		maxLength = &l
	}
	maxBlockSize := f.m.MaxPayloadSize() - blockCounterOverhead
	if opts.MaxBlockSize != 0 && maxBlockSize > int(opts.MaxBlockSize) {
		maxBlockSize = int(opts.MaxBlockSize)
	}
	if maxBlockSize < 1 || maxBlockSize > 0xFFFF {
		return nil, fmt.Errorf("bdx: block size %d outside 1..65535", maxBlockSize)
	}
	init := Init{
		TransferControl: TransferControl{
			Version:       Version,
			SenderDrive:   slices.Contains(modes, SenderDrive),
			ReceiverDrive: slices.Contains(modes, ReceiverDrive),
		},
		MaxBlockSize:   uint16(maxBlockSize), //nolint:gosec // bounded above
		StartOffset:    opts.StartOffset,
		MaxLength:      maxLength,
		FileDesignator: opts.TransferFileDesignator,
	}
	var startOffset uint64
	if opts.StartOffset != nil {
		startOffset = *opts.StartOffset
	}
	accept, err := f.m.SendSendInit(ctx, init)
	if err != nil {
		return nil, err
	}
	// collectAcceptParameters (bdx-session-initiator.ts:224-268): a
	// SendAccept carries no length, so the transfer's is the one offered.
	if accept.TransferControl.AsynchronousTransfer {
		return nil, newError(StatusTransferMethodNotSupported, "Asynchronous transfer is not supported in this implementation")
	}
	f.blockSize = capBlockSizeToTransport(f.m, int(accept.MaxBlockSize))
	if maxLength != nil && *maxLength != 0 {
		f.negotiate(*maxLength)
	}
	mode := ReceiverDrive
	if accept.TransferControl.SenderDrive {
		mode = SenderDrive
	}
	return &Transfer{
		f: f,
		// :258: the sender drives exactly when the accept says senderDrive.
		isDriver: accept.TransferControl.SenderDrive,
		res:      Result{Length: f.dataLength, Mode: mode, BlockSize: uint16(f.blockSize), StartOffset: startOffset}, //nolint:gosec // bounded by the accept's uint16
	}, nil
}

// negotiate records the length the transfer must meet (Flow.ts:30-31).
func (f *outboundFlow) negotiate(length uint64) {
	l := length
	f.negotiatedLength = &l
	f.dataLength = &l
	left := int64(l) //nolint:gosec // bounded by maxSafeInteger or the transfer limit
	f.bytesLeft = &left
}

// Run is the second half of [Send]: the blocks until the peer's
// BlockAckEof. Run once.
func (t *Transfer) Run(ctx context.Context) (Result, error) {
	res := t.res
	if err := t.f.process(ctx, t.isDriver); err != nil {
		return res, t.f.fail(ctx, err)
	}
	res.Transferred = t.f.transferred
	return res, nil
}
