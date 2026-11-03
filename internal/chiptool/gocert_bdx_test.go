// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SukramJ/go-fabric/bdx"
)

// The BDX cases. matter.js's BDX-1.2 and 2.2 run the device as DUT, a
// requestor downloading an OTA image from the TH controller
// (support/chip-testing/test/cert/tc-bdx-support.ts:48-51); BDX-1.4 and 2.1
// have the controller as DUT, the sender, and a requestor app as TH. Read
// mirrored, the daemon is that requestor too, and its ReceiveInit,
// BlockQuery and BlockAckEOF are what every case checks. Where the TS reads
// a device's log for a message, the Go TH reads the message off its own
// end of the transfer (bdx.ServerSession.OnMessage), the same bytes.

func init() {
	for _, c := range []func() goCertCase{caseBDX12, caseBDX14, caseBDX21, caseBDX22} {
		registerGoCert(c)
	}
}

// bdxRun is a BDX case's one served transfer (served, TC-BDX-1.2.test.ts:24).
type bdxRun struct {
	r      suRun
	served *otaServed
}

// precondition is every BDX case's step 0: commission the requestor and
// serve it one update (serveOtaTransfer, tc-bdx-support.ts:117-224).
func (b *bdxRun) precondition(th *goTH) error {
	if err := b.r.precondition(th); err != nil {
		return err
	}
	s, err := b.r.o.serve(b.r.n, b.r.peer, serveOpts{})
	if err != nil {
		return err
	}
	b.served = s
	if acks := s.transfer.messages(false, bdx.MessageBlockAckEOF); len(acks) != 1 {
		return fmt.Errorf("the requestor sent %d BlockAckEOF", len(acks))
	}
	if !s.applied {
		th.note("the requestor had not asked to apply the image when the TH stopped waiting; the transfer was complete before that")
	}
	return nil
}

func (b *bdxRun) transfer() (*otaServed, error) {
	if b.served == nil {
		return nil, errors.New("the precondition step served no BDX transfer for this step to read")
	}
	return b.served, nil
}

// caseBDX12 is TC-BDX-1.2: the requestor's ReceiveInit.
func caseBDX12() goCertCase {
	b := &bdxRun{}
	return goCertCase{
		tc: "TC-BDX-1.2", line: 123, native: true,
		pics: []string{"MCORE.BDX.Receiver", "MCORE.BDX.Initiator", "MCORE.BDX.Sender", "MCORE.BDX.Responder"},
		steps: []goStep{
			{n: "0", line: 133, title: "the TH commissions the DUT, stages an image and announces itself", run: b.precondition},
			// recordProposalFields (:43-121).
			{n: "1", line: 156, title: "DUT sends a ReceiveInit message to TH", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				p := s.transfer.init
				a, ok := s.transfer.accept()
				if !ok {
					return errors.New("the TH sent no ReceiveAccept")
				}
				var errs []error
				if !p.TransferControl.ReceiverDrive || p.TransferControl.AsynchronousTransfer {
					errs = append(errs, fmt.Errorf("the DUT proposed %+v; an OTA requestor needs receiver drive, synchronous", p.TransferControl))
				}
				if a.MaxBlockSize > p.MaxBlockSize {
					errs = append(errs, fmt.Errorf("the DUT proposed %d bytes and the transfer ran at %d", p.MaxBlockSize, a.MaxBlockSize))
				}
				if (p.StartOffset != nil && *p.StartOffset != 0) || (p.MaxLength != nil && *p.MaxLength != 0) {
					errs = append(errs, fmt.Errorf("the DUT named start offset %v and length %v; a requestor asks for the whole file", deref(p.StartOffset), deref(p.MaxLength)))
				}
				fd := string(p.FileDesignator)
				if !strings.HasPrefix(fd, "ota/") || !strings.HasSuffix(fd, fmt.Sprintf(".%d", s.version)) {
					errs = append(errs, fmt.Errorf("the DUT named file designator %q for the version %d staged", fd, s.version))
				}
				if err := errors.Join(errs...); err != nil {
					return err
				}
				th.note("ReceiveInit: version %d, receiverDrive %t senderDrive %t async %t, max block %d, no offset or length, designator %q (%d bytes)",
					p.TransferControl.Version, p.TransferControl.ReceiverDrive, p.TransferControl.SenderDrive, p.TransferControl.AsynchronousTransfer, p.MaxBlockSize, fd, len(fd))
				return nil
			}},
		},
	}
}

// caseBDX14 mirrors TC-BDX-1.4: the ReceiveAccept the sender answers the
// requestor's ReceiveInit with.
func caseBDX14() goCertCase {
	b := &bdxRun{}
	return goCertCase{
		tc: "TC-BDX-1.4", line: 166, pics: []string{"MCORE.BDX.Sender", "MCORE.BDX.Responder"},
		steps: []goStep{
			{n: "0", line: 172, title: "the DUT commissions the TH, stages an image and announces itself", run: b.precondition},
			// recordAcceptFields (:106-164).
			{n: "1", line: 195, title: "TH sends a ReceiveInit message to DUT + DUT sends a ReceiveAccept message back", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				p := s.transfer.init
				a, ok := s.transfer.accept()
				if !ok {
					return errors.New("no ReceiveAccept answered the ReceiveInit")
				}
				var errs []error
				chose := (a.TransferControl.SenderDrive && p.TransferControl.SenderDrive) || (a.TransferControl.ReceiverDrive && p.TransferControl.ReceiverDrive)
				if !chose || a.TransferControl.SenderDrive == a.TransferControl.ReceiverDrive || a.TransferControl.Version > p.TransferControl.Version {
					errs = append(errs, fmt.Errorf("accept %+v against the proposal %+v", a.TransferControl, p.TransferControl))
				}
				if a.MaxBlockSize > p.MaxBlockSize {
					errs = append(errs, fmt.Errorf("accept granted %d bytes against the proposed %d", a.MaxBlockSize, p.MaxBlockSize))
				}
				if p.MaxLength != nil && *p.MaxLength != 0 && a.Length != nil && *a.Length > *p.MaxLength {
					errs = append(errs, fmt.Errorf("accept names %d bytes against the proposed %d", *a.Length, *p.MaxLength))
				}
				if err := errors.Join(errs...); err != nil {
					return err
				}
				th.note("the requestor proposed %+v, %d bytes; the accept chose %+v, %d bytes, length %v; the requestor then queried %d blocks",
					p.TransferControl, p.MaxBlockSize, a.TransferControl, a.MaxBlockSize, deref(a.Length), len(s.transfer.messages(false, bdx.MessageBlockQuery)))
				return nil
			}},
		},
	}
}

// caseBDX21 mirrors TC-BDX-2.1: the synchronous sender's Blocks and
// BlockEOF, and the requestor's acknowledgements of them.
func caseBDX21() goCertCase {
	b := &bdxRun{}
	return goCertCase{
		tc: "TC-BDX-2.1", line: 207, pics: []string{"MCORE.BDX.SynchronousSender"},
		steps: []goStep{
			{n: "0", line: 213, title: "the DUT commissions the TH, stages an image and announces itself", run: b.precondition},
			{n: "1", line: 236, title: "DUT sends the first Block + TH sends a BlockAck", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				return firstBlockAcknowledged(th, s)
			}},
			{n: "2", line: 248, title: "DUT sends further Blocks + TH sends BlockAcks", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				blocks := s.transfer.messages(true, bdx.MessageBlock)
				if len(blocks) < 2 {
					return fmt.Errorf("%d Blocks, so none followed the first", len(blocks))
				}
				for i, m := range blocks {
					if m.counter != uint32(i) { //nolint:gosec // a block index
						return fmt.Errorf("Block %d of %d carries counter %d", i, len(blocks), m.counter)
					}
				}
				a, _ := s.transfer.accept()
				if _, _, moved := s.transfer.state(); moved <= uint64(a.MaxBlockSize) {
					return fmt.Errorf("%d bytes moved, no more than one block", moved)
				}
				th.note("%d Blocks with counters 0..%d", len(blocks), len(blocks)-1)
				return nil
			}},
			{n: "3", line: 254, title: "DUT sends a BlockEOF + TH sends a BlockAckEOF", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				return blockEOFAcknowledged(th, s)
			}},
		},
	}
}

// caseBDX22 is TC-BDX-2.2: the synchronous receiver's BlockQuery messages
// and its acknowledgements.
func caseBDX22() goCertCase {
	b := &bdxRun{}
	return goCertCase{
		tc: "TC-BDX-2.2", line: 183, native: true,
		pics: []string{"MCORE.BDX.SynchronousReceiver", "MCORE.BDX.SynchronousSender", "MCORE.BDX.Sender"},
		steps: []goStep{
			{n: "0", line: 193, title: "the TH commissions the DUT, stages an image and announces itself", run: b.precondition},
			{n: "1", line: 216, title: "TH sends the first Block + DUT sends a BlockAck", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				return firstBlockAcknowledged(th, s)
			}},
			{n: "2", line: 226, pics: "MCORE.BDX.Driver", title: "DUT sends the first BlockQuery + TH sends a Block", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				q := s.transfer.messages(false, bdx.MessageBlockQuery)
				if len(q) == 0 || q[0].counter != 0 {
					return fmt.Errorf("the DUT's first BlockQuery: %d sent, first %v", len(q), q)
				}
				th.note("the first BlockQuery names block 0")
				return nil
			}},
			{n: "3", line: 237, pics: "MCORE.BDX.Driver", title: "DUT sends further BlockQuery messages + TH sends further Blocks", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				q := s.transfer.messages(false, bdx.MessageBlockQuery)
				if len(q) < 2 {
					return fmt.Errorf("%d BlockQuery messages, so none followed", len(q))
				}
				for i, m := range q {
					if m.counter != uint32(i) { //nolint:gosec // a block index
						return fmt.Errorf("BlockQuery %d of %d carries counter %d", i, len(q), m.counter)
					}
				}
				th.note("%d BlockQuery messages with counters 0..%d", len(q), len(q)-1)
				return nil
			}},
			{
				n: "4", line: 246, title: "DUT sends a BlockQueryWithSkip + TH sends a Block",
				na: "the single uninterrupted OTA download this case reads never asks the sender to skip bytes (TC-BDX-2.2.test.ts:256-258)",
			},
			{n: "5", line: 264, title: "TH sends a BlockEOF + DUT sends a BlockAckEOF", run: func(th *goTH) error {
				s, err := b.transfer()
				if err != nil {
					return err
				}
				return blockEOFAcknowledged(th, s)
			}},
		},
	}
}

// firstBlockAcknowledged is firstBlockAcknowledged and firstBlock
// (TC-BDX-2.2.test.ts:27-80, TC-BDX-2.1.test.ts:26-89): the first Block
// counts from 0 and carries 1..MaxBlockSize bytes; under receiver drive
// the requestor's next BlockQuery, naming block 1, is its acknowledgement.
func firstBlockAcknowledged(th *goTH, s *otaServed) error {
	a, ok := s.transfer.accept()
	if !ok {
		return errors.New("no ReceiveAccept")
	}
	blocks := s.transfer.messages(true, bdx.MessageBlock)
	if len(blocks) == 0 || blocks[0].counter != 0 || blocks[0].length == 0 || blocks[0].length > int(a.MaxBlockSize) {
		return fmt.Errorf("the first Block: %d sent, first %+v, negotiated %d bytes", len(blocks), blocks, a.MaxBlockSize)
	}
	q := s.transfer.messages(false, bdx.MessageBlockQuery)
	if len(q) < 2 || q[1].counter != 1 {
		return fmt.Errorf("the requestor sent %d BlockQuery messages, so none answered the first Block with block 1", len(q))
	}
	th.note("first Block counter 0, %d bytes (negotiated %d); under receiver drive the requestor answered with a BlockQuery naming block 1",
		blocks[0].length, a.MaxBlockSize)
	return nil
}

// blockEOFAcknowledged is blockEofAcknowledged and blockEof
// (TC-BDX-2.2.test.ts:142-181, TC-BDX-2.1.test.ts:137-205): one BlockEOF
// with the counter the staged size gives, one BlockAckEOF naming it, and
// the whole file moved.
func blockEOFAcknowledged(th *goTH, s *otaServed) error {
	a, ok := s.transfer.accept()
	if !ok {
		return errors.New("no ReceiveAccept")
	}
	expected := uint32(s.fileSize / int(a.MaxBlockSize)) //nolint:gosec // a block count
	eofs := s.transfer.messages(true, bdx.MessageBlockEOF)
	acks := s.transfer.messages(false, bdx.MessageBlockAckEOF)
	if len(eofs) != 1 || eofs[0].counter != expected || eofs[0].length > int(a.MaxBlockSize) {
		return fmt.Errorf("BlockEOF: %d sent %+v, expected counter %d from a %d-byte image at %d bytes a block", len(eofs), eofs, expected, s.fileSize, a.MaxBlockSize)
	}
	if len(acks) != 1 || acks[0].counter != expected {
		return fmt.Errorf("BlockAckEOF: %d sent %+v, expected counter %d", len(acks), acks, expected)
	}
	if _, _, moved := s.transfer.state(); moved != uint64(s.fileSize) {
		return fmt.Errorf("%d of the %d staged bytes moved", moved, s.fileSize)
	}
	th.note("BlockEOF counter %d (%d bytes), BlockAckEOF counter %d, all %d bytes moved", expected, eofs[0].length, acks[0].counter, s.fileSize)
	return nil
}
