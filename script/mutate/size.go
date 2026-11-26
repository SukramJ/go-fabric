// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// byteSize is a flag value in bytes, written as a plain number or with a
// KiB, MiB or GiB suffix.
type byteSize int64

var sizeUnits = []struct {
	suffix string
	shift  uint
}{{"GiB", 30}, {"MiB", 20}, {"KiB", 10}}

func (b *byteSize) String() string {
	for _, u := range sizeUnits {
		if *b != 0 && int64(*b)%(1<<u.shift) == 0 {
			return strconv.FormatInt(int64(*b)>>u.shift, 10) + u.suffix
		}
	}
	return strconv.FormatInt(int64(*b), 10)
}

func (b *byteSize) Set(s string) error {
	num, shift := s, uint(0)
	for _, u := range sizeUnits {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			num, shift = n, u.shift
			break
		}
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 || n > (1<<62)>>shift {
		return fmt.Errorf("%q is not a size in bytes, KiB, MiB or GiB", s)
	}
	*b = byteSize(n << shift)
	return nil
}
