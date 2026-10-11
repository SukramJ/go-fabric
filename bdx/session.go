// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// DriverMode is who drives a transfer: the sender pushes blocks, or the
// receiver queries them.
//
// Mirrors matter.js flow/Flow.ts:170-173 (Flow.DriverMode).
type DriverMode uint8

// The driver modes.
const (
	SenderDrive DriverMode = iota
	ReceiverDrive
)

func (d DriverMode) String() string {
	if d == SenderDrive {
		return "senderDrive"
	}
	return "receiverDrive"
}

// blockCounterOverhead is the 4-byte block counter that precedes a block's
// data (bdx-session-initiator.ts:19-27, 155).
const blockCounterOverhead = 4

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER, the ceiling
// matter.js holds a length or offset to (BdxMessenger.ts:306-323,
// asSafeNumber).
const maxSafeInteger = 1<<53 - 1

// ReceiveOptions configures [Receive].
type ReceiveOptions struct {
	// TransferFileDesignator names the file in the ReceiveInit — the
	// designator the peer offered (matter.js `transferFileDesignator`).
	TransferFileDesignator FileDesignator
	// Sink receives the bytes.
	Sink Sink
	// PreferredDriverModes are the modes offered, in order of preference;
	// empty means matter.js's default [SenderDrive, ReceiverDrive]
	// (BdxSessionConfiguration.ts:104-111).
	PreferredDriverModes []DriverMode
	// MaxBlockSize, when set, lowers the offered block size below what the
	// exchange carries (BdxSessionConfiguration.Config.maxBlockSize).
	MaxBlockSize uint16
	// Progress, when set, is told the bytes transferred and the transfer's
	// size (nil while unknown): once when the transfer is negotiated, after
	// every block but the last, and once more when the last block arrived
	// (flow/Flow.ts:121-147, progressInfo).
	Progress func(transferred uint64, total *uint64)
}

// Result is what a completed transfer moved.
type Result struct {
	// Transferred is the number of bytes received.
	Transferred uint64
	// Length is the definite length the peer announced, nil when it
	// announced none.
	Length *uint64
	// Mode and BlockSize are what the transfer settled on.
	Mode      DriverMode
	BlockSize uint16
}

// Receive runs one inbound transfer this node initiates: ReceiveInit,
// the peer's ReceiveAccept, then the blocks — queried one by one when this
// node drives, acknowledged one by one when the peer does — until
// BlockEof, which is answered with BlockAckEof once the sink committed.
//
// A BDX [Error] sends the peer a failure StatusReport with its code and is
// returned with the bytes transferred filled in; any other error is
// returned as it is. Either way the sink is closed with the error and the
// exchange is left open for the caller to close.
//
// Mirrors matter.js BdxClient.asReceiver → BdxSession.processTransfer
// (BdxSession.ts:154-200) with bdxSessionInitiator's initiator/receiver
// branch (bdx-session-initiator.ts:47-49, buildInitMessage :114-179,
// collectAcceptParameters :224-268) and the two inbound flows,
// DrivingReceivingFlow (receiver drive) and FollowingSendingFlow (sender
// drive). Asynchronous transfer is refused as matter.js refuses it.
func Receive(ctx context.Context, m *Messenger, opts ReceiveOptions) (Result, error) {
	if opts.Sink == nil {
		return Result{}, errors.New("bdx: receive needs a sink")
	}
	modes := opts.PreferredDriverModes
	if len(modes) == 0 {
		modes = []DriverMode{SenderDrive, ReceiverDrive}
	}

	f := &inboundFlow{m: m, sink: opts.Sink, progress: opts.Progress, counter: -1}
	res, err := f.run(ctx, modes, opts)
	if err != nil {
		var be *Error
		if errors.As(err, &be) {
			// BdxSession.ts:188-195: report the code to the peer, best
			// effort, and record how far the transfer came.
			_ = m.SendError(ctx, be.Code)
			be.BytesTransferred = f.transferred
			be.TotalBytesLength = f.dataLength
		}
		_ = opts.Sink.Close(err)
		return res, err
	}
	return res, nil
}

// inboundFlow is the state of one inbound transfer (flow/Flow.ts,
// flow/InboundFlow.ts).
type inboundFlow struct {
	m        *Messenger
	sink     Sink
	progress func(uint64, *uint64)

	counter     int64 // last block counter used; -1 before the first (Flow.ts:18)
	transferred uint64
	dataLength  *uint64
	bytesLeft   *int64
}

func (f *inboundFlow) run(ctx context.Context, modes []DriverMode, opts ReceiveOptions) (Result, error) {
	// buildInitMessage, receiver branch (bdx-session-initiator.ts:155-178).
	maxBlockSize := f.m.MaxPayloadSize() - blockCounterOverhead
	if opts.MaxBlockSize != 0 && maxBlockSize > int(opts.MaxBlockSize) {
		maxBlockSize = int(opts.MaxBlockSize)
	}
	if maxBlockSize < 1 || maxBlockSize > 0xFFFF {
		return Result{}, fmt.Errorf("bdx: block size %d outside 1..65535", maxBlockSize)
	}
	init := Init{
		TransferControl: TransferControl{
			Version:       Version,
			SenderDrive:   slices.Contains(modes, SenderDrive),
			ReceiverDrive: slices.Contains(modes, ReceiverDrive),
		},
		MaxBlockSize:   uint16(maxBlockSize), //nolint:gosec // bounded above
		FileDesignator: opts.TransferFileDesignator,
	}
	accept, err := f.m.SendReceiveInit(ctx, init)
	if err != nil {
		return Result{}, err
	}

	// collectAcceptParameters (bdx-session-initiator.ts:224-268).
	if accept.TransferControl.AsynchronousTransfer {
		return Result{}, newError(StatusTransferMethodNotSupported, "Asynchronous transfer is not supported in this implementation")
	}
	blockSize := int(accept.MaxBlockSize)
	if transport := f.m.MaxPayloadSize() - blockCounterOverhead; blockSize > transport {
		blockSize = transport
	}
	if accept.Length != nil {
		if *accept.Length > maxSafeInteger {
			return Result{}, newError(StatusLengthTooLarge, "Accept message length field %d exceeds maximum safe integer value", *accept.Length)
		}
		l := *accept.Length
		f.dataLength = &l
		left := int64(l) //nolint:gosec // bounded by maxSafeInteger
		f.bytesLeft = &left
	}
	mode := ReceiverDrive
	if accept.TransferControl.SenderDrive {
		mode = SenderDrive
	}
	// As the receiver this node drives exactly when the sender does not.
	isDriver := !accept.TransferControl.SenderDrive
	res := Result{Length: f.dataLength, Mode: mode, BlockSize: uint16(blockSize)} //nolint:gosec // bounded by the accept's uint16

	// Flow.processTransfer (flow/Flow.ts:121-147).
	f.emit()
	for {
		var done bool
		if isDriver {
			done, err = f.drivingNext(ctx)
		} else {
			done, err = f.followingNext(ctx)
		}
		if err != nil {
			return res, err
		}
		if done {
			break
		}
		f.emit()
	}
	f.emit()
	// InboundFlow.finalizeTransfer (InboundFlow.ts:65-70): the write
	// completes, then BlockAckEof.
	if err := f.sink.Close(nil); err != nil {
		return res, err
	}
	if err := f.m.SendBlockAckEOF(ctx, uint32(f.counter)); err != nil { //nolint:gosec // a block counter is 32-bit
		return res, err
	}
	res.Transferred = f.transferred
	return res, nil
}

func (f *inboundFlow) emit() {
	if f.progress != nil {
		f.progress(f.transferred, f.dataLength)
	}
}

// nextCounter advances the block counter, wrapping at 2^32 (Flow.ts:97-100).
func (f *inboundFlow) nextCounter() uint32 {
	f.counter = (f.counter + 1) % 0x1_0000_0000
	return uint32(f.counter) //nolint:gosec // reduced modulo 2^32
}

// drivingNext is one step of DrivingReceivingFlow.transferNextChunk
// (flow/DrivingReceivingFlow.ts:17-42): BlockQuery, then the Block it
// asked for.
func (f *inboundFlow) drivingNext(ctx context.Context) (bool, error) {
	counter := f.nextCounter()
	if err := f.m.SendBlockQuery(ctx, counter); err != nil {
		return false, err
	}
	t, block, err := f.m.ReadBlock(ctx)
	if err != nil {
		return false, err
	}
	if block.BlockCounter != counter {
		return false, newError(StatusBadBlockCounter, "Received Block with unexpected block counter: %d, expected: %d", block.BlockCounter, counter)
	}
	f.transferred += uint64(len(block.Data))
	return f.write(block.Data, t)
}

// followingNext is one step of FollowingSendingFlow.transferNextChunk
// (flow/FollowingSendingFlow.ts:15-39): the peer's next Block, then its
// BlockAck.
func (f *inboundFlow) followingNext(ctx context.Context) (bool, error) {
	t, block, err := f.m.ReadBlock(ctx)
	if err != nil {
		return false, err
	}
	if want := f.nextCounter(); block.BlockCounter != want {
		return false, newError(StatusBadBlockCounter, "Received Block with unexpected block counter: %d, expected: %d", block.BlockCounter, want)
	}
	f.transferred += uint64(len(block.Data))
	done, err := f.write(block.Data, t)
	if err != nil || done {
		return done, err
	}
	return false, f.m.SendBlockAck(ctx, block.BlockCounter)
}

// write is InboundFlow.writeDataChunk (InboundFlow.ts:72-100): the data
// goes to the sink, and BlockEof ends the transfer — refused when a definite
// length was not met exactly.
func (f *inboundFlow) write(data []byte, t MessageType) (bool, error) {
	if _, err := f.sink.Write(data); err != nil {
		return false, err
	}
	if f.bytesLeft != nil {
		*f.bytesLeft -= int64(len(data))
	}
	if t != MessageBlockEOF {
		return false, nil
	}
	if f.bytesLeft != nil && *f.bytesLeft != 0 {
		code := StatusLengthTooLarge
		if *f.bytesLeft > 0 {
			code = StatusLengthTooShort
		}
		return false, newError(code, "Received BlockEof with %dbytes left, but no more data available", *f.bytesLeft)
	}
	return true, nil
}
