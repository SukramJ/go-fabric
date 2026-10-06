// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The CHIP test harness container.
//
// matter.js tests itself against chip-tool and the connectedhomeip
// certification cases out of one image it publishes, ghcr.io/matter-js/chip
// (source: ../matter.js/support/chip). The image is multi-arch (amd64 and
// arm64), and it carries everything a run needs at one CHIP commit:
// chip-tool, the YAML suites and their cluster XML, the YAML runner, the
// Python `TC_*.py` harness with its installed wheels, and the CHIP reference
// apps. That makes it this suite's default harness too — on any Linux host
// with Docker, no snap, no emulation, no local CHIP checkout.
//
// The container runs with host networking, as matter.js runs it
// (packages/testing/src/chip/state.ts configureContainer), so chip-tool
// inside reaches the daemon on [::1] and sees the host's multicast. mDNS
// goes through the host's avahi over the host's system D-Bus, bind-mounted
// in. Docker's default AppArmor profile refuses a containerised client on
// the host bus ("Access denied" from avahi-client), hence apparmor=unconfined
// — a property of this one container, not a change to the host.
//
// One scratch directory (under bin/, see launchHarness) is bind-mounted at the SAME absolute path inside the
// container. Everything the two sides share lives there — chip-tool's
// key-value stores, the daemon's --app-pipe FIFO, the Python harness's
// restart flag — so a path is valid on both sides without translation.
//
// Inside the suite, chip-tool is reached through a two-line wrapper script
// (`docker exec -i <container> /bin/chip-tool "$@"`), so every helper that
// runs a chip-tool binary — one-shot commands, the interactive websocket
// server, capability detection — works unchanged against the image.

const (
	// chipHarnessEnv selects the harness: "image" (the default when Docker
	// can run the image) or "host" (a chip-tool from
	// $GOFABRIC_CHIPTOOL_BIN, ./bin/chip-tool or PATH, as before).
	chipHarnessEnv = "GOFABRIC_CHIP_HARNESS"
	// chipImageEnv overrides the image the Makefile pins.
	chipImageEnv = "GOFABRIC_CHIP_IMAGE"
)

// harness is the running CHIP container.
type harness struct {
	image     string
	container string
	// shared is the scratch directory mounted at the same path inside.
	shared string
	// chipTool is the wrapper script that runs the image's chip-tool.
	chipTool string
	// describe names the image digest and its CHIP commit.
	describe string
	// accessory answers the YAML runner's SystemCommands (accessory_test.go).
	accessory *accessoryServer
}

var (
	harnessOnce sync.Once
	harnessVal  *harness
	harnessErr  error
)

// reMakefileChipImage reads the pinned harness image out of the Makefile,
// the single source the CI workflow reads too.
var reMakefileChipImage = regexp.MustCompile(`(?m)^CHIP_TEST_IMAGE\s*\?=\s*(\S+)`)

// pinnedChipImage returns the harness image: $GOFABRIC_CHIP_IMAGE or the
// Makefile's CHIP_TEST_IMAGE.
func pinnedChipImage() (string, error) {
	if img := os.Getenv(chipImageEnv); img != "" {
		return img, nil
	}
	data, err := os.ReadFile(filepath.Join(moduleRoot(), "Makefile")) //nolint:gosec // a fixed path inside this module
	if err != nil {
		return "", err
	}
	m := reMakefileChipImage.FindSubmatch(data)
	if m == nil {
		return "", errors.New("no CHIP_TEST_IMAGE pin in the Makefile")
	}
	return string(m[1]), nil
}

// imageHarnessWanted reports whether the suite should use the container.
func imageHarnessWanted() bool {
	switch os.Getenv(chipHarnessEnv) {
	case "host":
		return false
	case "image":
		return true
	}
	// Default: the image, unless a chip-tool was pinned explicitly.
	return os.Getenv(chipToolBinEnv) == ""
}

// startHarness starts the container once per test binary.
func startHarness() (*harness, error) {
	harnessOnce.Do(func() { harnessVal, harnessErr = launchHarness() })
	return harnessVal, harnessErr
}

func launchHarness() (*harness, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not on PATH: %w", err)
	}
	image, err := pinnedChipImage()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// A local image (pulled before, or by `make chiptool-image`) is used as
	// is; otherwise pull it. The pin is a digest, so "as is" is exact.
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err != nil { //nolint:gosec // image is the pinned harness reference
		if out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); err != nil { //nolint:gosec // image is the pinned harness reference
			return nil, fmt.Errorf("docker pull %s: %w\n%s", image, err, out)
		}
	}

	// Under the module's bin/ rather than os.TempDir: a snap-installed
	// Docker daemon has a private /tmp, so a bind mount of a host /tmp path
	// shows the container an empty directory. bin/ is gitignored and inside
	// $HOME, which the snap's home interface does expose.
	base := filepath.Join(moduleRoot(), "bin", "chip-harness")
	if err := os.MkdirAll(base, 0o755); err != nil { //nolint:gosec // shared with the container's root
		return nil, err
	}
	shared, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(shared, 0o777); err != nil { //nolint:gosec // shared with the container's root
		return nil, err
	}
	name := fmt.Sprintf("gofabric-chip-%d", os.Getpid())
	args := []string{
		"run", "-d", "--rm", "--name", name,
		"--network", "host",
		"--security-opt", "apparmor=unconfined",
		"-v", shared + ":" + shared,
		"--entrypoint", "sleep",
	}
	if _, err := os.Stat("/run/dbus/system_bus_socket"); err == nil {
		args = append(args, "-v", "/run/dbus:/run/dbus")
	}
	args = append(args, image, "infinity")
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		_ = os.RemoveAll(shared)
		return nil, fmt.Errorf("docker run %s: %w\n%s", image, err, out)
	}
	h := &harness{image: image, container: name, shared: shared}

	h.chipTool = filepath.Join(shared, "chip-tool")
	wrapper := "#!/bin/sh\nexec docker exec -i " + name + " /bin/chip-tool \"$@\"\n"
	if err := os.WriteFile(h.chipTool, []byte(wrapper), 0o755); err != nil { //nolint:gosec // an executable wrapper is the point
		h.close()
		return nil, err
	}

	commit, _ := h.exec(ctx, "cat", "/etc/chip-version")
	digest, _ := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", image).Output() //nolint:gosec // pinned image
	h.describe = fmt.Sprintf("container %s from %s (CHIP %s)", name, strings.TrimSpace(string(digest)), strings.TrimSpace(commit))

	if h.accessory, err = startAccessoryServer(); err != nil {
		h.close()
		return nil, err
	}
	if err := h.pointAccessoryBridge(ctx, h.accessory.port); err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}

// exec runs a command inside the container and returns its merged output.
func (h *harness) exec(ctx context.Context, argv ...string) (string, error) {
	return h.execIn(ctx, "", argv...)
}

// execIn runs a command inside the container from dir.
func (h *harness) execIn(ctx context.Context, dir string, argv ...string) (string, error) {
	args := []string{"exec"}
	if dir != "" {
		args = append(args, "-w", dir)
	}
	args = append(args, h.container)
	args = append(args, argv...)
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // argv is the suite's own
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return stripANSI(buf.String()), err
}

// close removes the container and the scratch directory.
func (h *harness) close() {
	if h.accessory != nil {
		h.accessory.close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", h.container).Run() //nolint:gosec // our own container
	// Files the container created as root cannot be removed from the host;
	// remove them from inside first.
	_ = exec.CommandContext(ctx, "docker", "run", "--rm", "-v", h.shared+":/x", "--entrypoint", "sh", h.image, "-c", "rm -rf /x/*").Run() //nolint:gosec // our own scratch dir
	_ = os.RemoveAll(h.shared)
}

// TestMain tears the harness container down after the run.
func TestMain(m *testing.M) {
	code := m.Run()
	if harnessVal != nil {
		harnessVal.close()
	}
	os.Exit(code)
}

// moduleRoot is the go-fabric checkout this test file lives in.
func moduleRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}
