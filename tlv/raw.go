// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"errors"
	"fmt"
	"io"
)

// ErrNotArray reports that [SplitArrayMembers] was handed an element that is
// not an Array or List container.
var ErrNotArray = errors.New("tlv: element is not an array or list")

// SplitArrayMembers splits one anonymously tagged Array or List element into
// the encodings of its direct members, each a complete element including its
// own control byte and (for containers) its end marker. It is the building
// block for splitting a list attribute across ReportData chunks the way
// matter.js chunkAttributePayload does (packages/protocol/src/interaction/
// AttributeDataEncoder.ts): a REPLACE-ALL with the leading members, then one
// ListIndex=null append per remaining member.
func SplitArrayMembers(encoded []byte) ([][]byte, error) {
	dec := NewDecoder(encoded)
	open, err := dec.Next()
	if err != nil {
		return nil, err
	}
	if open.Type != TypeArray && open.Type != TypeList {
		return nil, ErrNotArray
	}
	var out [][]byte
	for {
		start := dec.Pos()
		el, err := dec.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("%w: array not closed", ErrUnbalancedContainer)
			}
			return nil, err
		}
		if el.IsEndContainer {
			if dec.Remaining() != 0 {
				return nil, fmt.Errorf("tlv: %d trailing bytes after array", dec.Remaining())
			}
			return out, nil
		}
		if el.IsContainer {
			if err := skipContainer(dec); err != nil {
				return nil, err
			}
		}
		out = append(out, encoded[start:dec.Pos()])
	}
}

// skipContainer consumes the members of the container whose opener was just
// read, through its end marker.
func skipContainer(dec *Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("%w: container not closed", ErrUnbalancedContainer)
			}
			return err
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		}
	}
	return nil
}

// PutRawElement writes element — one complete, anonymously tagged element
// as [SplitArrayMembers] returns it — under tag. The element's control
// byte keeps its type; its tag is replaced.
func (e *Encoder) PutRawElement(tag Tag, element []byte) error {
	if len(element) == 0 {
		return errors.New("tlv: empty raw element")
	}
	if TagKind(element[0]>>5) != TagKindAnonymous {
		return fmt.Errorf("tlv: raw element must be anonymously tagged (control=0x%02X)", element[0])
	}
	e.writeControlAndTag(ElementType(element[0]&0x1F), tag)
	e.buf = append(e.buf, element[1:]...)
	return nil
}
