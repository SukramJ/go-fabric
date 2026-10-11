// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// TransferIdleTimeout is how long a BDX party waits for the peer's next
// message.
//
// Mirrors matter.js BdxMessenger.ts:37 (`BDX_TRANSFER_IDLE_TIMEOUT =
// Minutes(5)`, "Minimum time according to Matter spec").
const TransferIdleTimeout = 5 * time.Minute

// Message is one message the peer sent on an [Exchange].
type Message struct {
	ProtocolID uint16
	Opcode     uint8
	Payload    []byte
}

// Exchange is one Matter exchange on a secure session, the way BDX uses it.
// The transport underneath — MRP over UDP today, TCP once it lands — is the
// implementation's: an implementation sends reliably, acknowledges what it
// receives and does not hand acknowledgements up as messages.
//
// matter.js runs BDX only on a secure session (BdxMessenger.ts:56-58,
// "Bdx Protocol requires a secure session"); an implementation offers
// nothing else.
type Exchange interface {
	// Send sends payload with protocolID and opcode on the exchange.
	Send(ctx context.Context, protocolID uint16, opcode uint8, payload []byte) error
	// Receive returns the next message the peer sent on the exchange,
	// waiting until ctx ends.
	Receive(ctx context.Context) (Message, error)
	// MaxPayloadSize is the largest message payload the exchange carries
	// (matter.js MessageExchange.maxPayloadSize: the channel's maximum
	// minus the message overhead).
	MaxPayloadSize() int
	// Close ends the exchange.
	Close() error
}

// ErrUnexpectedMessage is a well-formed message of a type the transfer did
// not expect at this point. It is not a BDX [Error]: matter.js throws an
// UnexpectedDataError here (BdxMessenger.ts:95-98), which sends no
// StatusReport.
var ErrUnexpectedMessage = errors.New("bdx: unexpected message")

// Messenger exchanges BDX messages over one [Exchange].
//
// Mirrors matter.js packages/protocol/src/bdx/BdxMessenger.ts.
type Messenger struct {
	ex      Exchange
	timeout time.Duration
}

// NewMessenger returns a messenger over ex waiting timeout for each message
// of the peer; zero means [TransferIdleTimeout] (BdxMessenger.ts:55).
func NewMessenger(ex Exchange, timeout time.Duration) *Messenger {
	if timeout <= 0 {
		timeout = TransferIdleTimeout
	}
	return &Messenger{ex: ex, timeout: timeout}
}

// Exchange is the exchange the messenger runs on.
func (m *Messenger) Exchange() Exchange { return m.ex }

// MaxPayloadSize is the exchange's maximum payload.
func (m *Messenger) MaxPayloadSize() int { return m.ex.MaxPayloadSize() }

// Next waits for the next BDX message of one of the expected types and
// returns it decoded. A failure StatusReport is returned as a
// *[StatusResponseError]; a message of another type as
// [ErrUnexpectedMessage].
//
// Mirrors BdxMessenger.ts:80-101 (nextMessage). A message of another
// protocol is dropped as matter.js's exchange drops it
// (MessageExchange.ts:462-466).
func (m *Messenger) Next(ctx context.Context, expected ...MessageType) (MessageType, any, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	for {
		msg, err := m.ex.Receive(ctx)
		if err != nil {
			return 0, nil, err
		}
		if msg.ProtocolID == SecureChannelProtocolID && msg.Opcode == OpcodeStatusReport {
			if err := statusReportError(msg.Payload, expected); err != nil {
				return 0, nil, err
			}
			// A success StatusReport is not a BDX message type either.
			return 0, nil, fmt.Errorf("%w: StatusReport, expected: %s", ErrUnexpectedMessage, expectedInfo(expected))
		}
		if msg.ProtocolID != ProtocolID {
			continue
		}
		t := MessageType(msg.Opcode)
		if !slices.Contains(expected, t) {
			return 0, nil, fmt.Errorf("%w: received %s#%d, expected: %s", ErrUnexpectedMessage, t, uint8(t), expectedInfo(expected))
		}
		v, err := Decode(t, msg.Payload)
		if err != nil {
			return 0, nil, err
		}
		return t, v, nil
	}
}

func expectedInfo(expected []MessageType) string {
	parts := make([]string, len(expected))
	for i, t := range expected {
		parts[i] = fmt.Sprintf("%s#%d", t, uint8(t))
	}
	return strings.Join(parts, ",")
}

// statusReportError mirrors BdxMessenger.ts:274-296
// (throwIfErrorStatusReport).
func statusReportError(payload []byte, expected []MessageType) error {
	sr, err := DecodeStatusReport(payload)
	if err != nil {
		return err
	}
	hint := expectedInfo(expected)
	if sr.GeneralStatus != GeneralSuccess {
		return &StatusResponseError{
			GeneralStatus: sr.GeneralStatus, ProtocolStatus: sr.ProtocolStatus,
			Msg: fmt.Sprintf("Received general error status for protocol %d (%s)", sr.ProtocolID, hint),
		}
	}
	if sr.ProtocolStatus != StatusSuccess {
		return &StatusResponseError{
			GeneralStatus: sr.GeneralStatus, ProtocolStatus: sr.ProtocolStatus,
			Msg: fmt.Sprintf("Received general success status, but protocol status is not Success (%s)", hint),
		}
	}
	return nil
}

func (m *Messenger) send(ctx context.Context, t MessageType, payload []byte) error {
	return m.ex.Send(ctx, ProtocolID, uint8(t), payload)
}

// SendSendInit sends a SendInit and returns the peer's SendAccept
// (BdxMessenger.ts:111-117).
func (m *Messenger) SendSendInit(ctx context.Context, msg Init) (SendAccept, error) {
	payload, err := msg.Encode()
	if err != nil {
		return SendAccept{}, err
	}
	if err := m.send(ctx, MessageSendInit, payload); err != nil {
		return SendAccept{}, err
	}
	_, v, err := m.Next(ctx, MessageSendAccept)
	if err != nil {
		return SendAccept{}, err
	}
	return v.(SendAccept), nil //nolint:forcetypeassert // Decode returns SendAccept for MessageSendAccept
}

// SendReceiveInit sends a ReceiveInit and returns the peer's ReceiveAccept
// (BdxMessenger.ts:120-126).
func (m *Messenger) SendReceiveInit(ctx context.Context, msg Init) (ReceiveAccept, error) {
	payload, err := msg.Encode()
	if err != nil {
		return ReceiveAccept{}, err
	}
	if err := m.send(ctx, MessageReceiveInit, payload); err != nil {
		return ReceiveAccept{}, err
	}
	_, v, err := m.Next(ctx, MessageReceiveAccept)
	if err != nil {
		return ReceiveAccept{}, err
	}
	return v.(ReceiveAccept), nil //nolint:forcetypeassert // Decode returns ReceiveAccept for MessageReceiveAccept
}

// SendSendAccept sends a SendAccept (BdxMessenger.ts:129-131).
func (m *Messenger) SendSendAccept(ctx context.Context, msg SendAccept) error {
	payload, err := msg.Encode()
	if err != nil {
		return err
	}
	return m.send(ctx, MessageSendAccept, payload)
}

// SendReceiveAccept sends a ReceiveAccept (BdxMessenger.ts:134-136).
func (m *Messenger) SendReceiveAccept(ctx context.Context, msg ReceiveAccept) error {
	payload, err := msg.Encode()
	if err != nil {
		return err
	}
	return m.send(ctx, MessageReceiveAccept, payload)
}

// SendBlock sends a Block (BdxMessenger.ts:139-144).
func (m *Messenger) SendBlock(ctx context.Context, msg Block) error {
	return m.send(ctx, MessageBlock, msg.Encode())
}

// SendBlockEOF sends a BlockEof (BdxMessenger.ts:169-174).
func (m *Messenger) SendBlockEOF(ctx context.Context, msg Block) error {
	return m.send(ctx, MessageBlockEOF, msg.Encode())
}

// SendBlockQuery sends a BlockQuery (BdxMessenger.ts:153-158).
func (m *Messenger) SendBlockQuery(ctx context.Context, blockCounter uint32) error {
	return m.send(ctx, MessageBlockQuery, CounterOnly{BlockCounter: blockCounter}.Encode())
}

// SendBlockQueryWithSkip sends a BlockQueryWithSkip (BdxMessenger.ts:161-166).
func (m *Messenger) SendBlockQueryWithSkip(ctx context.Context, msg BlockQueryWithSkip) error {
	return m.send(ctx, MessageBlockQueryWithSkip, msg.Encode())
}

// SendBlockAck sends a BlockAck (BdxMessenger.ts:182-187).
func (m *Messenger) SendBlockAck(ctx context.Context, blockCounter uint32) error {
	return m.send(ctx, MessageBlockAck, CounterOnly{BlockCounter: blockCounter}.Encode())
}

// SendBlockAckEOF sends a BlockAckEof (BdxMessenger.ts:190-195).
func (m *Messenger) SendBlockAckEOF(ctx context.Context, blockCounter uint32) error {
	return m.send(ctx, MessageBlockAckEOF, CounterOnly{BlockCounter: blockCounter}.Encode())
}

// ReadBlock reads the next Block or BlockEof. A Block without data is a
// BadMessageContent error (BdxMessenger.ts:198-205).
func (m *Messenger) ReadBlock(ctx context.Context) (MessageType, Block, error) {
	t, v, err := m.Next(ctx, MessageBlock, MessageBlockEOF)
	if err != nil {
		return 0, Block{}, err
	}
	b := v.(Block) //nolint:forcetypeassert // Decode returns Block for Block and BlockEof
	if t == MessageBlock && len(b.Data) == 0 {
		return 0, Block{}, newError(StatusBadMessageContent, "Received empty data in Block message")
	}
	return t, b, nil
}

// ReadBlockQuery reads the next BlockQuery or BlockQueryWithSkip, consuming
// a BlockAck before it and holding the query's counter to the one after the
// acknowledged block (BdxMessenger.ts:212-236). The result is a
// [CounterOnly] or a [BlockQueryWithSkip].
func (m *Messenger) ReadBlockQuery(ctx context.Context) (MessageType, any, error) {
	t, v, err := m.Next(ctx, MessageBlockQuery, MessageBlockQueryWithSkip, MessageBlockAck)
	if err != nil {
		return 0, nil, err
	}
	var expectedCounter *uint32
	if t == MessageBlockAck {
		next := v.(CounterOnly).BlockCounter + 1 //nolint:forcetypeassert // Decode returns CounterOnly for BlockAck; wraps at 2^32
		expectedCounter = &next
		if t, v, err = m.Next(ctx, MessageBlockQuery, MessageBlockQueryWithSkip); err != nil {
			return 0, nil, err
		}
	}
	if expectedCounter != nil {
		got := counterOf(v)
		if got != *expectedCounter {
			return 0, nil, newError(StatusBadBlockCounter, "Received BlockQuery with unexpected block counter: %d, expected: %d", got, *expectedCounter)
		}
	}
	return t, v, nil
}

func counterOf(v any) uint32 {
	switch q := v.(type) {
	case CounterOnly:
		return q.BlockCounter
	case BlockQueryWithSkip:
		return q.BlockCounter
	}
	return 0
}

// ReadBlockAckEOF reads the next BlockAckEof (BdxMessenger.ts:239-243).
func (m *Messenger) ReadBlockAckEOF(ctx context.Context) (CounterOnly, error) {
	_, v, err := m.Next(ctx, MessageBlockAckEOF)
	if err != nil {
		return CounterOnly{}, err
	}
	return v.(CounterOnly), nil //nolint:forcetypeassert // Decode returns CounterOnly for BlockAckEof
}

// ReadBlockAck reads the next BlockAck (BdxMessenger.ts:246-250).
func (m *Messenger) ReadBlockAck(ctx context.Context) (CounterOnly, error) {
	_, v, err := m.Next(ctx, MessageBlockAck)
	if err != nil {
		return CounterOnly{}, err
	}
	return v.(CounterOnly), nil //nolint:forcetypeassert // Decode returns CounterOnly for BlockAck
}

// SendError sends a failure StatusReport with code (BdxMessenger.ts:253-272:
// general status Failure, on the Secure Channel protocol).
func (m *Messenger) SendError(ctx context.Context, code StatusCode) error {
	return m.ex.Send(ctx, SecureChannelProtocolID, OpcodeStatusReport, EncodeStatusReport(GeneralFailure, code))
}

// Close closes the exchange (BdxMessenger.ts:298-300).
func (m *Messenger) Close() error { return m.ex.Close() }
