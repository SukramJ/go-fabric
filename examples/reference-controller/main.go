// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command reference-controller is the module's controller role as a
// command-line tool: it pairs Matter nodes over IP, reads, writes, invokes
// and subscribes, creates groups and bindings and serves an OTA image. Its
// fabric — the CA, the fabric record, the client registry and the DCL trust
// store — lives in one SQLite database (--db), so every run is the same
// fabric and every node it paired still trusts it.
//
// It is an example, not API (README.md §API stability): copy from it, do
// not build against it. README.md beside this file lists every command.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "reference-controller:", err)
		}
		os.Exit(1)
	}
}

// stderr is where the log and the usage go.
var stderr io.Writer = os.Stderr

// globalOptions are the flags every command takes.
type globalOptions struct {
	db          string
	dcl         string
	attestation string
	logLevel    string
	listen      string
	nodeListen  string
	timeout     durationFlag
	caKey       string
	trust       string
	trustDir    string
}

// defaultOptions are the flags' defaults.
func defaultOptions() *globalOptions {
	return &globalOptions{db: "reference-controller.db", dcl: "off", logLevel: "warn", listen: "[::]:0", nodeListen: "[::]:0", trust: "development"}
}

// register defines the global flags on fs with o's current values as
// their defaults, so a command's own flag set — which defines them again,
// for flags after the command — keeps what was parsed before it.
func (o *globalOptions) register(fs *flag.FlagSet) {
	fs.StringVar(&o.db, "db", o.db, "the SQLite database holding the CA, the fabric, the paired nodes and the DCL trust store")
	fs.StringVar(&o.dcl, "dcl", o.dcl, "the online Distributed Compliance Ledger on top of the trust set: prod, testnet or off")
	fs.StringVar(&o.trust, "trust", o.trust, "the attestation trust set: development (DCL snapshot and CSA test PAAs), production (DCL snapshot) or none (no chain check)")
	fs.StringVar(&o.trustDir, "trust-dir", o.trustDir, "a DCL snapshot directory (paa/, cd-signers/, SOURCE) in place of the embedded one")
	fs.StringVar(&o.attestation, "attestation", o.attestation, "on an attestation finding: accept, reject or ask (default ask on a terminal, reject otherwise)")
	fs.StringVar(&o.logLevel, "log-level", o.logLevel, "log level: debug, info, warn or error")
	fs.StringVar(&o.listen, "listen", o.listen, "the controller's UDP address")
	fs.StringVar(&o.nodeListen, "node-listen", o.nodeListen, "the UDP address of the controller's own node (group sender, OTA provider)")
	fs.Var(&o.timeout, "timeout", "how long a command waits for the network (default 60s)")
	fs.StringVar(&o.caKey, "ca-key", o.caKey, "the CA root key outside the database: file:<path> (passphrase from "+passphraseEnv+")")
}

// command is one subcommand: its usage line and its function.
type command struct {
	usage string
	run   func(e *env, args []string) error
}

// commands is every subcommand. "group", "ota" and "window" dispatch on
// their second word. It is filled in init: the commands' usage errors
// refer to it.
var commands map[string]command

func init() {
	commands = map[string]command{
		"pair":      {"pair --code <MT:… | 11 or 21 digits> | --passcode N --discriminator N [--node-id N] [--label L] [--wifi-ssid S --wifi-credentials C | --thread-dataset HEX]", cmdPair},
		"unpair":    {"unpair <node>", cmdUnpair},
		"nodes":     {"nodes", cmdNodes},
		"node":      {"node <node>", cmdNode},
		"read":      {"read <node> <ep>/<Cluster>/<Attribute>…", cmdRead},
		"write":     {"write <node> <ep>/<Cluster>/<Attribute> <json> [--timed ms]", cmdWrite},
		"invoke":    {"invoke <node> <ep>/<Cluster>/<Command> [<json>] [--timed ms]", cmdInvoke},
		"subscribe": {"subscribe <node> <path>… [--min s] [--max s] [--count n]", cmdSubscribe},
		"events":    {"events <node> <ep>/<Cluster>/<Event>…", cmdEvents},
		"group":     {"group create <id> --key <hex|random> --nodes <n,n…> --endpoints <e,e…> | group send <id> <Cluster>/<Command> [<json>]", cmdGroup},
		"bind":      {"bind <node> <ep> --to <node>/<ep>[/<Cluster>] | --group <id>", cmdBind},
		"ota":       {"ota serve --image <file> [--version N] [--vendor N] [--product N] [--announce <node>] [--once] [--yes]", cmdOTA},
		"window":    {"window open <node> [--timeout s] [--basic] | window revoke <node>", cmdWindow},
	}
}

// env is what a command runs with.
type env struct {
	ctx    context.Context
	opts   *globalOptions
	stdin  io.Reader
	stdout io.Writer
	// outMu serialises stdout: the OTA provider's offer (asked on the
	// bridge's receive goroutine) and the serving command's own lines
	// write concurrently.
	outMu sync.Mutex
	log   *slog.Logger
	// attests is set by a command that commissions a device: only it
	// opens the DCL trust store.
	attests bool
}

// run is the tool: args without the program name. A command that fails
// returns its error; main prints it and exits 1.
func run(args []string, stdin io.Reader, stdout io.Writer) error {
	opts := defaultOptions()
	fs := flag.NewFlagSet("reference-controller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts.register(fs)
	fs.Usage = func() { usage(fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		usage(fs)
		return errors.New("no command")
	}
	name := fs.Arg(0)
	cmd, ok := commands[name]
	if !ok {
		usage(fs)
		return fmt.Errorf("unknown command %q", name)
	}
	log, err := newLogger(opts.logLevel)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(baseContext(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cmd.run(&env{ctx: ctx, opts: opts, stdin: stdin, stdout: stdout, log: log}, fs.Args()[1:])
}

// baseContext is the context every run derives from; a test replaces it.
var baseContext = context.Background

func newLogger(levelName string) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelName)); err != nil {
		return nil, fmt.Errorf("--log-level %q: %w", levelName, err)
	}
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})), nil
}

func usage(fs *flag.FlagSet) {
	_, _ = fmt.Fprintln(stderr, "usage: reference-controller [flags] <command> [arguments]")
	_, _ = fmt.Fprintln(stderr, "\ncommands:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_, _ = fmt.Fprintln(stderr, "  "+commands[n].usage)
	}
	_, _ = fmt.Fprintln(stderr, "\nflags (before or after the command):")
	fs.PrintDefaults()
}

// parseArgs parses a command's flags wherever they appear among its
// positional arguments ("--" ends the flags) and returns the positionals.
func parseArgs(e *env, name string, args []string, define func(fs *flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	e.opts.register(fs)
	if define != nil {
		define(fs)
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			// A --log-level after the command applies too.
			log, err := newLogger(e.opts.logLevel)
			if err != nil {
				return nil, err
			}
			e.log = log
			return pos, nil
		}
		// flag stops at "--" too, having consumed it: everything after is
		// positional.
		if len(args) > len(rest) && args[len(args)-len(rest)-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// wantArgs checks the number of positional arguments.
func wantArgs(cmd string, pos []string, minN, maxN int) error {
	if len(pos) < minN || (maxN >= 0 && len(pos) > maxN) {
		return fmt.Errorf("usage: %s", commands[cmd].usage)
	}
	return nil
}

// isTerminal reports whether r is a character device (an interactive
// stdin).
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// printf writes to the command's output; a failed write to stdout is
// not an error a command can do anything about.
func (e *env) printf(format string, args ...any) {
	e.outMu.Lock()
	defer e.outMu.Unlock()
	_, _ = fmt.Fprintf(e.stdout, format, args...)
}

func (e *env) println(args ...any) {
	e.outMu.Lock()
	defer e.outMu.Unlock()
	_, _ = fmt.Fprintln(e.stdout, args...)
}

// writeJSON encodes v as one line on stdout under the same lock.
func (e *env) writeJSON(v any) error {
	e.outMu.Lock()
	defer e.outMu.Unlock()
	return json.NewEncoder(e.stdout).Encode(v)
}

// confirm asks a yes/no question on the console.
func confirm(e *env, question string) bool {
	e.printf("%s [y/N] ", question)
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := e.stdin.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	ans := strings.ToLower(strings.TrimSpace(b.String()))
	return ans == "y" || ans == "yes"
}
