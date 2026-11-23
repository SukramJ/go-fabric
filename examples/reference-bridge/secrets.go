// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"thingfabric.dev/go-fabric/node"
)

// passcodeEnv is the environment variable the setup passcode comes from
// when --passcode is not given, so it need not be on the command line,
// where ps and the shell history show it — as the reference controller
// takes its network secrets (R2-P6-sec-2615).
const passcodeEnv = "GOFABRIC_PASSCODE" //nolint:gosec // G101: the variable's name, not a credential

// secretValue is a secret flag's value: read from the file it names when
// it starts with "@" (a trailing newline dropped), else the value itself.
func secretValue(flagName, value string) (string, error) {
	path, ok := strings.CutPrefix(value, "@")
	if !ok {
		return value, nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // the operator names the file
	if err != nil {
		return "", fmt.Errorf("%s: %w", flagName, err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// passcodeValue is --passcode: a decimal passcode, or @file.
//
// Set does not fail on a value that is not a decimal number: package flag
// reports a failing Set as `invalid value "<value>" for flag -passcode`,
// the passcode itself in the text that reaches a log (R2-C2-sec-2601). It
// records the value-free errPasscodeNotDecimal in err instead, which
// checkFlags reports after the parse.
type passcodeValue struct {
	v   *uint
	err *error
}

// errPasscodeNotDecimal is a --passcode that is not a decimal number. The
// value is not in it: it is the passcode, and the text reaches a log.
var errPasscodeNotDecimal = errors.New("--passcode is not a decimal number")

func (p passcodeValue) String() string {
	if p.v == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*p.v), 10)
}

func (p passcodeValue) Set(s string) error {
	// A file that cannot be read is an error flag may print: its text
	// names the file, never the passcode.
	s, err := secretValue("--passcode", s)
	if err != nil {
		return err
	}
	if n, perr := strconv.ParseUint(strings.TrimSpace(s), 10, 32); perr == nil {
		*p.v, *p.err = uint(n), nil
	} else {
		*p.err = errPasscodeNotDecimal
	}
	return nil
}

// passcodeFromEnv sets --passcode from passcodeEnv when the flag was not
// given on fs.
func passcodeFromEnv(fs *flag.FlagSet) error {
	v := os.Getenv(passcodeEnv)
	if v == "" {
		return nil
	}
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == "passcode" })
	if given {
		return nil
	}
	if err := fs.Set("passcode", v); err != nil {
		return fmt.Errorf("$%s: %w", passcodeEnv, err)
	}
	if p, ok := fs.Lookup("passcode").Value.(passcodeValue); ok && *p.err != nil {
		return fmt.Errorf("$%s: %w", passcodeEnv, *p.err)
	}
	return nil
}

// enableKeyBytes is --enable-key: 16 bytes in hex, or @file holding them.
func enableKeyBytes(arg string) ([]byte, error) {
	v, err := secretValue("--enable-key", arg)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("--enable-key: %w", err)
	}
	return key, nil
}

// pbkdfSalt is --pbkdf-salt, or, when it is empty, a salt of
// node.PBKDFSaltMax bytes drawn at random at each start rather than a
// constant every copy of the daemon shares: matter.js draws its salt at
// random (packages/protocol/src/protocol/DeviceCommissioner.ts:134 at
// db021da0), and the salt reaches a commissioner inside PASE only, so
// nothing needs it kept (R2-P6-sec-2615).
func pbkdfSalt(arg string) []byte {
	if arg != "" {
		return []byte(arg)
	}
	salt := make([]byte, node.PBKDFSaltMax)
	_, _ = rand.Read(salt) // crypto/rand.Read does not fail
	return salt
}
