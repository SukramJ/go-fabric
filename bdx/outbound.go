// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"io"
	"sync"
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

	// endMu serialises the flow's end, which two goroutines may reach: the
	// flow's own (the final progress, the read of the final ack, a
	// failure) and the receive goroutine, where a [ReceiveNotifier]
	// reports the final BlockAckEof at its arrival. It guards the two
	// fields below and is held while the end's hooks run, so finished and
	// cancelled are exclusive and each runs at most once.
	endMu sync.Mutex
	// finalEmitted is set once the final progress went out (Flow.ts:135).
	finalEmitted bool
	// end is how the flow ended: endRunning until finished or cancelled.
	end flowEnd
}

// flowEnd is how a flow ended.
type flowEnd uint8

const (
	endRunning flowEnd = iota
	endFinished
	endCancelled
)

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
		if err := f.sendBlockEOF(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
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
		if err := f.sendBlockEOF(ctx, Block{BlockCounter: counter, Data: data}); err != nil {
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

// sendBlockEOF sends the last block, watching for its BlockAckEof first:
// the receiver piggybacks the block's MRP acknowledgement on it, so it
// arrives while the send still waits. When it arrived with the final
// counter the transfer is finished, and a send that returns an error
// after that — its wait ended by the context in the same instant — is
// complete all the same.
func (f *outboundFlow) sendBlockEOF(ctx context.Context, b Block) error {
	f.watchBlockAckEOF(b.BlockCounter, f.transferred+uint64(len(b.Data)))
	err := f.m.SendBlockEOF(ctx, b)
	if err != nil && f.finished() {
		return nil
	}
	return err
}

// watchBlockAckEOF installs the exchange's [ReceiveNotifier], when it has
// one, for the final BlockAckEof: one for counter, the last block's,
// finishes the transfer with total bytes at its arrival, on the receive
// goroutine — the BDX state step on receive of chip's TransferFacilitator
// (src/protocols/bdx/TransferFacilitator.cpp:61 at 6170af84, where
// HandleMessageReceived runs in OnMessageReceived) and the end matter.js's
// processTransfer reaches once finalizeTransfer read that ack
// (flow/Flow.ts:139-141, flow/OutboundFlow.ts:115-118 at db021da0). The
// transition needs no I/O, so it is complete before the receive path
// decodes the peer's next datagram: an OTA requestor's ApplyUpdateRequest,
// sent after its BlockAckEof, finds the update waiting for its apply. Any
// other message, and one with another counter, is left to the read, which
// refuses a wrong counter as it always has.
func (f *outboundFlow) watchBlockAckEOF(counter uint32, total uint64) {
	rn, ok := f.m.Exchange().(ReceiveNotifier)
	if !ok {
		return
	}
	rn.OnReceive(func(msg Message) {
		if msg.ProtocolID != ProtocolID || MessageType(msg.Opcode) != MessageBlockAckEOF {
			return
		}
		ack, err := DecodeCounterOnly(msg.Payload)
		if err != nil || ack.BlockCounter != counter {
			return
		}
		f.finish(total)
	})
}

// emitFinal is the final progress (Flow.ts:135), once: finish emits it
// when the BlockAckEof arrives first.
func (f *outboundFlow) emitFinal() {
	f.endMu.Lock()
	defer f.endMu.Unlock()
	if f.finalEmitted || f.end != endRunning {
		return
	}
	f.finalEmitted = true
	f.emit()
}

// finish ends the flow as finished with total bytes (Flow.ts:139-141):
// the final progress if it has not gone out, then progressFinished. It
// does nothing once the flow ended.
func (f *outboundFlow) finish(total uint64) {
	f.endMu.Lock()
	defer f.endMu.Unlock()
	if f.end != endRunning {
		return
	}
	f.end = endFinished
	if !f.finalEmitted {
		f.finalEmitted = true
		if f.hooks.progress != nil {
			f.hooks.progress(total, f.dataLength)
		}
	}
	if f.hooks.finished != nil {
		f.hooks.finished(total)
	}
}

// finished reports whether the flow ended as finished.
func (f *outboundFlow) finished() bool {
	f.endMu.Lock()
	defer f.endMu.Unlock()
	return f.end == endFinished
}

// cancel ends the flow as cancelled (Flow.ts:137-138, :143-145). It does
// nothing once the flow ended: a transfer finished at its BlockAckEof's
// arrival is never reported cancelled after.
func (f *outboundFlow) cancel() {
	f.endMu.Lock()
	defer f.endMu.Unlock()
	if f.end != endRunning {
		return
	}
	f.end = endCancelled
	if f.hooks.cancelled != nil {
		f.hooks.cancelled()
	}
}

// process is Flow.processTransfer (flow/Flow.ts:121-147) for an outbound
// flow, finalized as OutboundFlow.finalizeTransfer (OutboundFlow.ts:115-119):
// the peer's BlockAckEof, for the last block's counter. On an exchange
// with a [ReceiveNotifier] the ack finishes the transfer at its arrival
// (watchBlockAckEOF), and process only takes the queued ack off the
// exchange. A context that ends while the flow still waits — a scope
// disabled under it included — fails the flow, which reports cancelled
// (Flow.ts:137-138), never finished.
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
	f.emitFinal()
	if f.finished() {
		// The BlockAckEof arrived and finished the transfer; its counter
		// was checked there. Take it off the exchange — it is queued, and
		// a context ended since leaves it to the exchange's close.
		_, _ = f.m.ReadBlockAckEOF(ctx)
		return nil
	}
	ack, err := f.m.ReadBlockAckEOF(ctx)
	if err != nil {
		if f.finished() {
			// It arrived while the read waited and the context ended in
			// the same instant.
			return nil
		}
		return err
	}
	if err := validateCounter(ack.BlockCounter, f.finalCounter); err != nil {
		return err
	}
	f.finish(f.transferred)
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
	f.cancel()
	return err
}
