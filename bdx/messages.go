// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"encoding/binary"
	"fmt"
)

// ProtocolID is the BDX protocol id on the wire (Common vendor).
//
// Mirrors matter.js packages/types/src/protocol/definitions/bdx.ts:7
// (`BDX_PROTOCOL_ID = 2`).
const ProtocolID uint16 = 2

// MessageType is a BDX message type, the protocol opcode of a BDX message.
//
// Mirrors matter.js packages/types/src/protocol/definitions/bdx.ts:9-20.
type MessageType uint8

// The BDX message types.
const (
	MessageSendInit           MessageType = 0x01
	MessageSendAccept         MessageType = 0x02
	MessageReceiveInit        MessageType = 0x04
	MessageReceiveAccept      MessageType = 0x05
	MessageBlockQuery         MessageType = 0x10
	MessageBlock              MessageType = 0x11
	MessageBlockEOF           MessageType = 0x12
	MessageBlockAck           MessageType = 0x13
	MessageBlockAckEOF        MessageType = 0x14
	MessageBlockQueryWithSkip MessageType = 0x15
)

// String names the message type as matter.js does.
func (t MessageType) String() string {
	switch t {
	case MessageSendInit:
		return "SendInit"
	case MessageSendAccept:
		return "SendAccept"
	case MessageReceiveInit:
		return "ReceiveInit"
	case MessageReceiveAccept:
		return "ReceiveAccept"
	case MessageBlockQuery:
		return "BlockQuery"
	case MessageBlock:
		return "Block"
	case MessageBlockEOF:
		return "BlockEof"
	case MessageBlockAck:
		return "BlockAck"
	case MessageBlockAckEOF:
		return "BlockAckEof"
	case MessageBlockQueryWithSkip:
		return "BlockQueryWithSkip"
	}
	return fmt.Sprintf("unknown(0x%02x)", uint8(t))
}

// StatusCode is a BDX protocol status code, carried in a StatusReport.
//
// Mirrors matter.js packages/types/src/protocol/definitions/bdx.ts:22-104.
type StatusCode uint16

// The BDX status codes.
const (
	StatusSuccess                    StatusCode = 0x00
	StatusLengthTooLarge             StatusCode = 0x12
	StatusLengthTooShort             StatusCode = 0x13
	StatusLengthMismatch             StatusCode = 0x14
	StatusLengthRequired             StatusCode = 0x15
	StatusBadMessageContent          StatusCode = 0x16
	StatusBadBlockCounter            StatusCode = 0x17
	StatusUnexpectedMessage          StatusCode = 0x18
	StatusResponderBusy              StatusCode = 0x19
	StatusTransferFailedUnknownError StatusCode = 0x1F
	StatusTransferMethodNotSupported StatusCode = 0x50
	StatusFileDesignatorUnknown      StatusCode = 0x51
	StatusStartOffsetNotSupported    StatusCode = 0x52
	StatusVersionNotSupported        StatusCode = 0x53
	StatusUnknown                    StatusCode = 0x5F
	StatusGeneralError               StatusCode = 0xFFFF
)

// Version is the only BDX protocol version (BdxInitMessagesSchema.ts:11,
// `BDX_VERSION = 0`).
const Version = 0

// TransferControl is the transfer-control octet (PTC in an *Init, TC in an
// *Accept).
//
// Mirrors matter.js BdxInitMessagesSchema.ts:19-34
// (BdxTransferControlBitmap): version in bits 0-3, senderDrive bit 4,
// receiverDrive bit 5, asynchronousTransfer bit 6; bit 7 reserved.
type TransferControl struct {
	Version              uint8
	SenderDrive          bool
	ReceiverDrive        bool
	AsynchronousTransfer bool
}

func (c TransferControl) encode() byte {
	b := c.Version & 0x0F
	if c.SenderDrive {
		b |= 1 << 4
	}
	if c.ReceiverDrive {
		b |= 1 << 5
	}
	if c.AsynchronousTransfer {
		b |= 1 << 6
	}
	return b
}

func decodeTransferControl(b byte) TransferControl {
	return TransferControl{
		Version:              b & 0x0F,
		SenderDrive:          b&(1<<4) != 0,
		ReceiverDrive:        b&(1<<5) != 0,
		AsynchronousTransfer: b&(1<<6) != 0,
	}
}

// rangeControl is the range-control octet (RC).
//
// Mirrors matter.js BdxInitMessagesSchema.ts:43-55 (BdxRangeControlBitmap):
// definiteLength bit 0, startOffset bit 1, wideRange bit 4.
type rangeControl struct {
	definiteLength bool
	startOffset    bool
	wideRange      bool
}

func (r rangeControl) encode() byte {
	var b byte
	if r.definiteLength {
		b |= 1 << 0
	}
	if r.startOffset {
		b |= 1 << 1
	}
	if r.wideRange {
		b |= 1 << 4
	}
	return b
}

func decodeRangeControl(b byte) rangeControl {
	return rangeControl{definiteLength: b&(1<<0) != 0, startOffset: b&(1<<1) != 0, wideRange: b&(1<<4) != 0}
}

// Init is a SendInit or ReceiveInit message.
//
// Mirrors matter.js BdxInitMessagesSchema.ts:59-104 (BdxInit). matter.js
// decides between 32- and 64-bit range fields by the JavaScript type of the
// value (`typeof startOffset === "bigint"`); the Go form carries that choice
// as WideRange. A MaxLength of 0 means an indefinite length, as in
// matter.js, and is not put on the wire.
type Init struct {
	TransferControl TransferControl
	MaxBlockSize    uint16
	// StartOffset is the optional start offset (STARTOFS).
	StartOffset *uint64
	// MaxLength is the optional definite length (DEFLEN); nil or 0 is
	// indefinite.
	MaxLength *uint64
	// WideRange writes StartOffset and MaxLength as 64-bit values.
	WideRange      bool
	FileDesignator []byte
	// MetaData is the optional trailing TLV metadata.
	MetaData []byte
}

// Validate applies matter.js BdxInitMessageSchema.validate
// (BdxInitMessagesSchema.ts:185-210).
func (m Init) Validate() error {
	if m.TransferControl.Version != Version {
		return newError(StatusVersionNotSupported, "Unsupported BDX version: %d", m.TransferControl.Version)
	}
	if !m.TransferControl.SenderDrive && !m.TransferControl.ReceiverDrive {
		return newError(StatusBadMessageContent, "BDX transfer protocol needs to allow either sender or receiver drive")
	}
	if len(m.FileDesignator) == 0 {
		return newError(StatusBadMessageContent, "File Designator cannot be empty")
	}
	if len(m.FileDesignator) > 0xFFFF {
		return newError(StatusBadMessageContent, "File Designator length exceeds maximum allowed size of 65535bytes")
	}
	if m.MaxBlockSize < 1 {
		return newError(StatusBadMessageContent, "Max Block Size must be between 1 and 65535bytes")
	}
	return nil
}

// Encode encodes the message. Mirrors BdxInitMessagesSchema.ts:108-146.
func (m Init) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	definite := m.MaxLength != nil && *m.MaxLength != 0
	rc := rangeControl{startOffset: m.StartOffset != nil, definiteLength: definite, wideRange: m.WideRange}
	out := []byte{m.TransferControl.encode(), rc.encode()}
	out = binary.LittleEndian.AppendUint16(out, m.MaxBlockSize)
	if m.StartOffset != nil {
		out = appendRange(out, *m.StartOffset, m.WideRange)
	}
	if definite {
		out = appendRange(out, *m.MaxLength, m.WideRange)
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(m.FileDesignator))) //nolint:gosec // Validate bounds it to 0xFFFF
	out = append(out, m.FileDesignator...)
	if len(m.MetaData) > 0 {
		out = append(out, m.MetaData...)
	}
	return out, nil
}

// DecodeInit decodes a SendInit or ReceiveInit payload. Mirrors
// BdxInitMessagesSchema.ts:148-183, the validation included.
func DecodeInit(b []byte) (Init, error) {
	r := reader{b: b}
	m := Init{TransferControl: decodeTransferControl(r.u8())}
	rc := decodeRangeControl(r.u8())
	m.MaxBlockSize = r.u16()
	m.WideRange = rc.wideRange
	if rc.startOffset {
		v := r.rangeValue(rc.wideRange)
		m.StartOffset = &v
	}
	if rc.definiteLength {
		// 0 also means indefinite length (:164-166).
		if v := r.rangeValue(rc.wideRange); v != 0 {
			m.MaxLength = &v
		}
	}
	fdLen := int(r.u16())
	m.FileDesignator = r.bytes(fdLen)
	if r.err != nil {
		return Init{}, r.err
	}
	if rest := r.rest(); len(rest) > 0 {
		m.MetaData = rest
	}
	if err := m.Validate(); err != nil {
		return Init{}, err
	}
	return m, nil
}

// SendAccept is a SendAccept message (BdxAcceptMessagesSchema.ts:18-38).
type SendAccept struct {
	TransferControl TransferControl
	MaxBlockSize    uint16
	MetaData        []byte
}

// validateAccept applies BdxSendAcceptMessageSchema.validate
// (BdxAcceptMessagesSchema.ts:82-98).
func validateAccept(tc TransferControl, maxBlockSize uint16) error {
	if tc.Version != Version {
		return newError(StatusVersionNotSupported, "Unsupported BDX version: %d", tc.Version)
	}
	if !tc.SenderDrive && !tc.ReceiverDrive && !tc.AsynchronousTransfer {
		return newError(StatusBadMessageContent, "BDX transfer control need to specify at least one of sender drive, receiver drive or asynchronousTransfer")
	}
	if maxBlockSize < 1 {
		return newError(StatusBadMessageContent, "Max Block Size must be between 1 and 65535bytes")
	}
	return nil
}

// Encode encodes the message. Mirrors BdxAcceptMessagesSchema.ts:51-64.
func (m SendAccept) Encode() ([]byte, error) {
	if err := validateAccept(m.TransferControl, m.MaxBlockSize); err != nil {
		return nil, err
	}
	out := []byte{m.TransferControl.encode()}
	out = binary.LittleEndian.AppendUint16(out, m.MaxBlockSize)
	return append(out, m.MetaData...), nil
}

// DecodeSendAccept decodes a SendAccept payload
// (BdxAcceptMessagesSchema.ts:66-80).
func DecodeSendAccept(b []byte) (SendAccept, error) {
	r := reader{b: b}
	m := SendAccept{TransferControl: decodeTransferControl(r.u8()), MaxBlockSize: r.u16()}
	if r.err != nil {
		return SendAccept{}, r.err
	}
	if rest := r.rest(); len(rest) > 0 {
		m.MetaData = rest
	}
	if err := validateAccept(m.TransferControl, m.MaxBlockSize); err != nil {
		return SendAccept{}, err
	}
	return m, nil
}

// ReceiveAccept is a ReceiveAccept message (BdxAcceptMessagesSchema.ts:41-47).
type ReceiveAccept struct {
	TransferControl TransferControl
	MaxBlockSize    uint16
	// Length is the optional definite length; nil or 0 is indefinite.
	Length *uint64
	// WideRange writes Length as a 64-bit value.
	WideRange bool
	MetaData  []byte
}

// Encode encodes the message. Mirrors BdxAcceptMessagesSchema.ts:104-131.
func (m ReceiveAccept) Encode() ([]byte, error) {
	if err := validateAccept(m.TransferControl, m.MaxBlockSize); err != nil {
		return nil, err
	}
	definite := m.Length != nil && *m.Length != 0
	rc := rangeControl{definiteLength: definite, wideRange: m.WideRange}
	out := []byte{m.TransferControl.encode(), rc.encode()}
	out = binary.LittleEndian.AppendUint16(out, m.MaxBlockSize)
	if definite {
		out = appendRange(out, *m.Length, m.WideRange)
	}
	return append(out, m.MetaData...), nil
}

// DecodeReceiveAccept decodes a ReceiveAccept payload
// (BdxAcceptMessagesSchema.ts:133-157).
func DecodeReceiveAccept(b []byte) (ReceiveAccept, error) {
	r := reader{b: b}
	m := ReceiveAccept{TransferControl: decodeTransferControl(r.u8())}
	rc := decodeRangeControl(r.u8())
	m.MaxBlockSize = r.u16()
	m.WideRange = rc.wideRange
	if rc.definiteLength {
		if v := r.rangeValue(rc.wideRange); v != 0 {
			m.Length = &v
		}
	}
	if r.err != nil {
		return ReceiveAccept{}, r.err
	}
	if rest := r.rest(); len(rest) > 0 {
		m.MetaData = rest
	}
	if err := validateAccept(m.TransferControl, m.MaxBlockSize); err != nil {
		return ReceiveAccept{}, err
	}
	return m, nil
}

// Block is a Block or BlockEof message (BdxBlockMessagesSchema.ts:29-34,
// 80-99): a 32-bit little-endian block counter followed by the data.
type Block struct {
	BlockCounter uint32
	Data         []byte
}

// Encode encodes the message.
func (m Block) Encode() []byte {
	out := binary.LittleEndian.AppendUint32(nil, m.BlockCounter)
	return append(out, m.Data...)
}

// DecodeBlock decodes a Block or BlockEof payload.
func DecodeBlock(b []byte) (Block, error) {
	r := reader{b: b}
	m := Block{BlockCounter: r.u32()}
	if r.err != nil {
		return Block{}, r.err
	}
	m.Data = r.rest()
	return m, nil
}

// CounterOnly is a BlockQuery, BlockAck or BlockAckEof message
// (BdxBlockMessagesSchema.ts:10-56): the 32-bit block counter alone.
type CounterOnly struct {
	BlockCounter uint32
}

// Encode encodes the message.
func (m CounterOnly) Encode() []byte {
	return binary.LittleEndian.AppendUint32(nil, m.BlockCounter)
}

// DecodeCounterOnly decodes a BlockQuery, BlockAck or BlockAckEof payload.
func DecodeCounterOnly(b []byte) (CounterOnly, error) {
	r := reader{b: b}
	m := CounterOnly{BlockCounter: r.u32()}
	return m, r.err
}

// BlockQueryWithSkip is a BlockQueryWithSkip message
// (BdxBlockMessagesSchema.ts:24-26, 58-77): block counter, then a 64-bit
// skip.
type BlockQueryWithSkip struct {
	BlockCounter uint32
	BytesToSkip  uint64
}

// Encode encodes the message.
func (m BlockQueryWithSkip) Encode() []byte {
	out := binary.LittleEndian.AppendUint32(nil, m.BlockCounter)
	return binary.LittleEndian.AppendUint64(out, m.BytesToSkip)
}

// DecodeBlockQueryWithSkip decodes a BlockQueryWithSkip payload.
func DecodeBlockQueryWithSkip(b []byte) (BlockQueryWithSkip, error) {
	r := reader{b: b}
	m := BlockQueryWithSkip{BlockCounter: r.u32(), BytesToSkip: r.u64()}
	return m, r.err
}

// Decode decodes payload as a message of type t into its Go form: [Init],
// [SendAccept], [ReceiveAccept], [Block], [CounterOnly] or
// [BlockQueryWithSkip]. Mirrors matter.js BdxMessage.decode
// (schema/BdxMessage.ts:34-39, the Kinds table :56-67).
func Decode(t MessageType, payload []byte) (any, error) {
	switch t {
	case MessageSendInit, MessageReceiveInit:
		return DecodeInit(payload)
	case MessageSendAccept:
		return DecodeSendAccept(payload)
	case MessageReceiveAccept:
		return DecodeReceiveAccept(payload)
	case MessageBlock, MessageBlockEOF:
		return DecodeBlock(payload)
	case MessageBlockQuery, MessageBlockAck, MessageBlockAckEOF:
		return DecodeCounterOnly(payload)
	case MessageBlockQueryWithSkip:
		return DecodeBlockQueryWithSkip(payload)
	}
	return nil, fmt.Errorf("bdx: can not decode unknown BDX message type: %d", uint8(t))
}

func appendRange(out []byte, v uint64, wide bool) []byte {
	if wide {
		return binary.LittleEndian.AppendUint64(out, v)
	}
	return binary.LittleEndian.AppendUint32(out, uint32(v)) //nolint:gosec // a narrow range field is 32-bit by definition
}

// reader is a little-endian cursor that records the first short read.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = fmt.Errorf("bdx: message truncated: need %d bytes at offset %d of %d", n, r.off, len(r.b))
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u8() byte {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *reader) u16() uint16 {
	if s := r.take(2); s != nil {
		return binary.LittleEndian.Uint16(s)
	}
	return 0
}

func (r *reader) u32() uint32 {
	if s := r.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if s := r.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}

func (r *reader) rangeValue(wide bool) uint64 {
	if wide {
		return r.u64()
	}
	return uint64(r.u32())
}

func (r *reader) bytes(n int) []byte {
	s := r.take(n)
	if s == nil {
		return nil
	}
	return append([]byte(nil), s...)
}

func (r *reader) rest() []byte {
	if r.err != nil || r.off >= len(r.b) {
		return nil
	}
	return append([]byte(nil), r.b[r.off:]...)
}
