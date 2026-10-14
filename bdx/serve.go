// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"io"
	"io/fs"
)

// Blob is a stored file an outbound transfer reads: its size and its bytes
// at any offset. It is the Go form of the Blob matter.js's
// PersistedFileDesignator.openBlob returns (PersistedFileDesignator.ts:60-70);
// *bytes.Reader and *os.File (through a size wrapper) satisfy it.
type Blob interface {
	io.ReaderAt
	Size() int64
}

// ServeOptions configures [Serve].
type ServeOptions struct {
	// InitType is the message that opened the exchange: [MessageSendInit]
	// (the peer sends, this node receives) or [MessageReceiveInit] (the
	// peer receives, this node sends).
	InitType MessageType
	// Init is that message, decoded.
	Init Init
	// TransferFileDesignator is the designator this node serves under; the
	// Init must name it (bdx-session-initiator.ts:59-65).
	TransferFileDesignator FileDesignator
	// Open returns the file a ReceiveInit asks for; an error wrapping
	// fs.ErrNotExist is FileDesignatorUnknown (bdx-session-initiator.ts:
	// 67-74). Needed for a ReceiveInit.
	Open func(ctx context.Context) (Blob, error)
	// Sink receives the bytes a SendInit announces. Needed for a SendInit.
	Sink Sink
	// PreferredDriverModes are the modes accepted, in order of preference;
	// empty means matter.js's default [SenderDrive, ReceiverDrive]
	// (BdxSessionConfiguration.ts:104-111).
	PreferredDriverModes []DriverMode
	// MaxBlockSize, when set, lowers the accepted block size
	// (bdx-session-initiator.ts:200-203).
	MaxBlockSize uint16
	// Progress, Finished and Cancelled observe the transfer once it is
	// negotiated (flow/Flow.ts:21-23: progressInfo, progressFinished,
	// progressCancelled).
	Progress  func(transferred uint64, total *uint64)
	Finished  func(total uint64)
	Cancelled func()
	// Negotiated, when set, is told what the transfer settled on before
	// the first block (BdxSession.transferParameters, BdxSession.ts:141-143).
	Negotiated func(Result)
}

// Serve runs one transfer the peer initiated with ServeOptions.Init on the
// messenger's exchange: the *Accept, then the blocks. A ReceiveInit makes
// this node the sender (FollowingReceivingFlow when the peer drives,
// DrivenSendingFlow when this node does), a SendInit the receiver
// (DrivingReceivingFlow / FollowingSendingFlow).
//
// A BDX [Error] sends the peer a failure StatusReport with its code and is
// returned with the bytes transferred filled in; any other error is
// returned as it is. The exchange is left open for the caller to close.
//
// Mirrors matter.js BdxSession.fromMessage → processTransfer
// (BdxSession.ts:61-74, :154-200) with bdxSessionInitiator's two responder
// branches (bdx-session-initiator.ts:52-111, determineAcceptParameters
// :182-218, collectAcceptParameters :224-268).
func Serve(ctx context.Context, m *Messenger, opts ServeOptions) (Result, error) {
	modes := opts.PreferredDriverModes
	if len(modes) == 0 {
		modes = []DriverMode{SenderDrive, ReceiverDrive}
	}
	if opts.InitType == MessageReceiveInit {
		return serveSender(ctx, m, modes, opts)
	}
	if opts.InitType == MessageSendInit {
		return serveReceiver(ctx, m, modes, opts)
	}
	// BdxSession.fromMessage (BdxSession.ts:63-68).
	err := newError(StatusUnexpectedMessage, "Invalid message type for BDX session initialization: %s (%d)", opts.InitType, uint8(opts.InitType))
	_ = m.SendError(ctx, err.Code)
	return Result{}, err
}

// checkDesignator is bdx-session-initiator.ts:59-65.
func checkDesignator(opts ServeOptions) error {
	incoming, want := FileDesignator(opts.Init.FileDesignator).Text(), opts.TransferFileDesignator.Text()
	if incoming != want {
		return newError(StatusFileDesignatorUnknown, "File designator in Init message (%s) does not match the configured transfer file designator (%s)", incoming, want)
	}
	return nil
}

// determineAccept is determineAcceptParameters (bdx-session-initiator.ts:
// 182-218): the first preferred mode the Init offers, its block size
// capped to the transport and the configured maximum, its length when
// definite.
func determineAccept(m *Messenger, modes []DriverMode, opts ServeOptions) (tc TransferControl, blockSize uint16, length *uint64, err error) {
	init := opts.Init
	found := false
	for _, mode := range modes {
		if (mode == SenderDrive && init.TransferControl.SenderDrive) || (mode == ReceiverDrive && init.TransferControl.ReceiverDrive) {
			tc = TransferControl{Version: Version, SenderDrive: mode == SenderDrive, ReceiverDrive: mode == ReceiverDrive}
			found = true
			break
		}
	}
	if !found {
		return tc, 0, nil, newError(StatusTransferMethodNotSupported, "Can not determine a valid transfer mode")
	}
	size := capBlockSizeToTransport(m, int(init.MaxBlockSize))
	if opts.MaxBlockSize != 0 && size > int(opts.MaxBlockSize) {
		size = int(opts.MaxBlockSize)
	}
	if size < 1 {
		return tc, 0, nil, newError(StatusBadMessageContent, "Max Block Size must be between 1 and 65535bytes")
	}
	if init.MaxLength != nil {
		if *init.MaxLength > maxSafeInteger {
			return tc, 0, nil, newError(StatusLengthTooLarge, "Max length %d exceeds maximum safe integer value", *init.MaxLength)
		}
		if *init.MaxLength > 0 {
			l := *init.MaxLength
			length = &l
		}
	}
	return tc, uint16(size), length, nil //nolint:gosec // capped by the Init's uint16
}

// serveSender is the sender-and-responder branch (bdx-session-initiator.ts:
// 67-102).
func serveSender(ctx context.Context, m *Messenger, modes []DriverMode, opts ServeOptions) (Result, error) {
	f := &outboundFlow{m: m, counter: -1}
	res, err := negotiateSender(ctx, f, modes, opts)
	if err != nil {
		return res, f.fail(ctx, err)
	}
	if opts.Negotiated != nil {
		opts.Negotiated(res)
	}
	f.hooks = flowHooks{progress: opts.Progress, finished: opts.Finished, cancelled: opts.Cancelled}
	if err := f.process(ctx, res.Mode == SenderDrive); err != nil {
		return res, f.fail(ctx, err)
	}
	res.Transferred = f.transferred
	return res, nil
}

func negotiateSender(ctx context.Context, f *outboundFlow, modes []DriverMode, opts ServeOptions) (Result, error) {
	if err := checkDesignator(opts); err != nil {
		return Result{}, err
	}
	if opts.Open == nil {
		return Result{}, newError(StatusFileDesignatorUnknown, "File designator %s does not point to an existing file in the storage to send data", opts.TransferFileDesignator.Text())
	}
	blob, err := opts.Open(ctx)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}, newError(StatusFileDesignatorUnknown, "File designator %s does not point to an existing file in the storage to send data", opts.TransferFileDesignator.Text())
		}
		return Result{}, err
	}
	var startOffset uint64
	if opts.Init.StartOffset != nil {
		if *opts.Init.StartOffset > maxSafeInteger {
			return Result{}, newError(StatusStartOffsetNotSupported, "Start offset %d exceeds maximum safe integer value", *opts.Init.StartOffset)
		}
		startOffset = *opts.Init.StartOffset
	}
	tc, blockSize, length, err := determineAccept(f.m, modes, opts)
	if err != nil {
		return Result{}, err
	}
	size := uint64(max(blob.Size(), 0))
	if length != nil {
		// :86-98: a definite length must fit what the blob holds past
		// the offset.
		available := int64(size) - int64(startOffset) //nolint:gosec // both bounded by maxSafeInteger
		if int64(*length) > available {               //nolint:gosec // bounded by maxSafeInteger
			return Result{}, newError(StatusLengthTooLarge, "Requested maxLength %dbytes exceeds available size %d for blob %s", *length, size, opts.TransferFileDesignator.Text())
		}
	}
	if err := f.m.SendReceiveAccept(ctx, ReceiveAccept{TransferControl: tc, MaxBlockSize: blockSize, Length: length}); err != nil {
		return Result{}, err
	}
	// collectAcceptParameters (:224-268), then OutboundFlow.initTransfer
	// (OutboundFlow.ts:30-65): the blob from the offset, cut to the
	// negotiated length or its full size.
	f.blockSize = capBlockSizeToTransport(f.m, int(blockSize))
	dataLength := size
	if length != nil {
		dataLength = *length
		f.negotiate(*length)
	} else {
		dl := dataLength
		f.dataLength = &dl
	}
	f.data = io.NewSectionReader(blob, int64(startOffset), int64(dataLength)) //nolint:gosec // bounded by maxSafeInteger
	mode := ReceiverDrive
	if tc.SenderDrive {
		mode = SenderDrive
	}
	return Result{Length: f.dataLength, Mode: mode, BlockSize: uint16(f.blockSize)}, nil //nolint:gosec // capped above
}

// serveReceiver is the receiver-and-responder branch
// (bdx-session-initiator.ts:105-111): a SendAccept, which carries no
// length, then the inbound flow.
func serveReceiver(ctx context.Context, m *Messenger, modes []DriverMode, opts ServeOptions) (Result, error) {
	if opts.Sink == nil {
		return Result{}, errors.New("bdx: serving a SendInit needs a sink")
	}
	f := &inboundFlow{m: m, sink: opts.Sink, counter: -1}
	res, err := negotiateReceiver(ctx, f, modes, opts)
	if err != nil {
		return res, f.fail(ctx, err)
	}
	if opts.Negotiated != nil {
		opts.Negotiated(res)
	}
	f.hooks = flowHooks{progress: opts.Progress, finished: opts.Finished, cancelled: opts.Cancelled}
	// :258: the receiver drives exactly when the sender does not.
	if err := f.process(ctx, res.Mode == ReceiverDrive); err != nil {
		return res, f.fail(ctx, err)
	}
	res.Transferred = f.transferred
	return res, nil
}

func negotiateReceiver(ctx context.Context, f *inboundFlow, modes []DriverMode, opts ServeOptions) (Result, error) {
	if err := checkDesignator(opts); err != nil {
		return Result{}, err
	}
	tc, blockSize, _, err := determineAccept(f.m, modes, opts)
	if err != nil {
		return Result{}, err
	}
	if err := f.m.SendSendAccept(ctx, SendAccept{TransferControl: tc, MaxBlockSize: blockSize}); err != nil {
		return Result{}, err
	}
	mode := ReceiverDrive
	if tc.SenderDrive {
		mode = SenderDrive
	}
	return Result{Mode: mode, BlockSize: uint16(capBlockSizeToTransport(f.m, int(blockSize)))}, nil //nolint:gosec // capped above
}
