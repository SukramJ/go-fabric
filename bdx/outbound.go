// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"io"
)

// flowHooks are a flow's observables (flow/Flow.ts:21-23): progressInfo,
// progressFinished and progressCancelled.
type flowHooks struct {
	// progress is told the bytes transferred and the transfer's size (nil
	// while unknown) once the transfer is negotiated, after every chunk but
	// the last, and once more after the last (Flow.ts:125, :133, :135).
	progress func(transferred uint64, total *uint64)
	// finished is told the bytes transferred once the transfer is
	// finalized (Flow.ts:140-141).
	finished func(total uint64)
	// cancelled runs when the transfer fails (Flow.ts:143-145).
	cancelled func()
}

// capBlockSizeToTransport caps a negotiated block size to what one message
// of the exchange carries, the payload less the 4-byte block counter.
// Mirrors matter.js bdx-session-initiator.ts:20-27 (capBlockSizeToTransport).
func capBlockSizeToTransport(m *Messenger, maxBlockSize int) int {
	if transport := m.MaxPayloadSize() - blockCounterOverhead; maxBlockSize > transport {
		return transport
	}
	return maxBlockSize
}

// outboundFlow is one outbound transfer: data read from this node and sent
// to the peer — matter.js flow/OutboundFlow.ts, with its two concrete flows
// as the two step functions: DrivenSendingFlow (this node drives:
// [outboundFlow.drivenNext]) and FollowingReceivingFlow (the receiver
// drives: [outboundFlow.followingNext]).
type outboundFlow struct {
	m     *Messenger
	hooks flowHooks
	// data is the stream the blocks are read from, already positioned at
	// the start offset and cut to the data length when one is known
	// (OutboundFlow.initTransfer, OutboundFlow.ts:30-65).
	data      io.Reader
	blockSize int

	counter      int64 // last block counter used; -1 before the first (Flow.ts:18)
	finalCounter uint32
	transferred  uint64
	// dataLength is the transfer's size for progress (Flow.ts:24, :31,
	// OutboundFlow.ts:37-38: the blob size when none was negotiated).
	dataLength *uint64
	// bytesLeft is set only when a length was negotiated (Flow.ts:30,
	// transferParameters.dataLength); readDataChunk then holds the data
	// to it (OutboundFlow.ts:93-110).
	bytesLeft *int64
	// negotiatedLength is that negotiated length (transferParameters.dataLength).
	negotiatedLength *uint64
}

// nextCounter advances the block counter, wrapping at 2^32 (Flow.ts:97-100).
func (f *outboundFlow) nextCounter() uint32 {
	f.counter = (f.counter + 1) % 0x1_0000_0000
	return uint32(f.counter) //nolint:gosec // reduced modulo 2^32
}

// validateCounter is Flow.validateCounter (Flow.ts:103-111).
func validateCounter(got, want uint32) error {
	if got != want {
		return newError(StatusBadBlockCounter, "Received Block with unexpected block counter: %d, expected: %d", got, want)
	}
	return nil
}

func (f *outboundFlow) emit() {
	if f.hooks.progress != nil {
		f.hooks.progress(f.transferred, f.dataLength)
	}
}

// readDataChunk reads the next block of the stream and does the basic
// checks. Mirrors OutboundFlow.readDataChunk (OutboundFlow.ts:70-113): the
// stream yields full blocks (BytesStreamReader.read(blockSize)); a short
// one must be the last, the end of the stream is an empty last block, and
// a negotiated length is met exactly — LengthTooShort / LengthTooLarge
// otherwise — and ends the transfer when it is used up.
func (f *outboundFlow) readDataChunk() (data []byte, done bool, err error) {
	buf := make([]byte, f.blockSize)
	n, err := io.ReadFull(f.data, buf)
	switch {
	case err == nil:
		// A full block; matter.js does not look past it (:82 only
		// checks a short one).
	case errors.Is(err, io.EOF):
		// :73-81: no value and done — an empty last block.
		done = true
	case errors.Is(err, io.ErrUnexpectedEOF):
		// :82-91: less than a block, and the stream ended after it.
		done = true
	default:
		return nil, false, err
	}
	data = buf[:n]
	if f.bytesLeft != nil && f.negotiatedLength != nil {
		*f.bytesLeft -= int64(n)
		if *f.bytesLeft < 0 {
			return nil, false, newError(StatusLengthTooLarge, "Data length exceeded, expected %dbytes, but got %dbytes more", *f.negotiatedLength, -*f.bytesLeft)
		}
		if done && *f.bytesLeft > 0 {
			return nil, false, newError(StatusLengthTooShort, "Data length too short, expected %dbytes, but got %dbytes less", *f.negotiatedLength, *f.bytesLeft)
		}
		if *f.bytesLeft == 0 {
			done = true
		}
	}
	return data, done, nil
}

// drivenNext is one step of DrivenSendingFlow.transferNextChunk
// (flow/DrivenSendingFlow.ts:15-42): the next Block — the last as BlockEof
// — and, the transfer being synchronous, the peer's BlockAck for it.
func (f *outboundFlow) drivenNext(ctx context.Context) (bool, error) {
	counter := f.nextCounter()
	data, done, err := f.readDataChunk()
	if err != nil {
		return false, err
	}
	if done {
		if err := f.m.SendBlockEOF(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
			return false, err
		}
		f.transferred += uint64(len(data))
		f.finalCounter = counter
		return true, nil
	}
	if err := f.m.SendBlock(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
		return false, err
	}
	f.transferred += uint64(len(data))
	ack, err := f.m.ReadBlockAck(ctx)
	if err != nil {
		return false, err
	}
	return false, validateCounter(ack.BlockCounter, counter)
}

// followingNext is one step of FollowingReceivingFlow.transferNextChunk
// (flow/FollowingReceivingFlow.ts:21-55): the receiver's BlockQuery or
// BlockQueryWithSkip (a BlockAck before it consumed), the skip honoured,
// then the Block it asked for — the last as BlockEof.
func (f *outboundFlow) followingNext(ctx context.Context) (bool, error) {
	t, v, err := f.m.ReadBlockQuery(ctx)
	if err != nil {
		return false, err
	}
	var skip uint64
	if t == MessageBlockQueryWithSkip {
		q, _ := v.(BlockQueryWithSkip)
		if q.BytesToSkip > maxSafeInteger {
			// BdxMessenger.asSafeNumber with TransferFailedUnknownError (:27-31).
			return false, newError(StatusTransferFailedUnknownError, "BytesToSkip %d exceeds maximum safe integer value", q.BytesToSkip)
		}
		skip = q.BytesToSkip
	}
	counter := counterOf(v)
	if err := validateCounter(counter, f.nextCounter()); err != nil {
		return false, err
	}
	if skip > 0 {
		// :37-41: the stream skips what it can; the requested amount
		// counts as transferred.
		if _, err := io.CopyN(io.Discard, f.data, int64(skip)); err != nil && !errors.Is(err, io.EOF) { //nolint:gosec // bounded by maxSafeInteger
			return false, err
		}
		f.transferred += skip
	}
	data, done, err := f.readDataChunk()
	if err != nil {
		return false, err
	}
	if done {
		if err := f.m.SendBlockEOF(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
			return false, err
		}
		f.transferred += uint64(len(data))
		f.finalCounter = counter
		return true, nil
	}
	if err := f.m.SendBlock(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
		return false, err
	}
	f.transferred += uint64(len(data))
	return false, nil
}

// process is Flow.processTransfer (flow/Flow.ts:121-147) for an outbound
// flow, finalized as OutboundFlow.finalizeTransfer (OutboundFlow.ts:115-119):
// the peer's BlockAckEof, for the last block's counter.
func (f *outboundFlow) process(ctx context.Context, isDriver bool) error {
	f.emit()
	for {
		var done bool
		var err error
		if isDriver {
			done, err = f.drivenNext(ctx)
		} else {
			done, err = f.followingNext(ctx)
		}
		if err != nil {
			return err
		}
		if done {
			break
		}
		f.emit()
	}
	f.emit()
	ack, err := f.m.ReadBlockAckEOF(ctx)
	if err != nil {
		return err
	}
	if err := validateCounter(ack.BlockCounter, f.finalCounter); err != nil {
		return err
	}
	if f.hooks.finished != nil {
		f.hooks.finished(f.transferred)
	}
	return nil
}

// fail is BdxSession.processTransfer's catch (BdxSession.ts:186-196) for
// an outbound flow.
func (f *outboundFlow) fail(ctx context.Context, err error) error {
	var be *Error
	if errors.As(err, &be) {
		_ = f.m.SendError(ctx, be.Code)
		be.BytesTransferred = f.transferred
		be.TotalBytesLength = f.dataLength
	}
	if f.hooks.cancelled != nil {
		f.hooks.cancelled()
	}
	return err
}
