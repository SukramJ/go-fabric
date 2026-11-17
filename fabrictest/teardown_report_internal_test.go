// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// loggingTB records what the site logs.
type loggingTB struct {
	*testing.T
	mu     sync.Mutex
	logs   []string
	logged chan struct{}
}

func (l *loggingTB) Logf(format string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, fmt.Sprintf(format, args...))
	l.mu.Unlock()
	select {
	case l.logged <- struct{}{}:
	default:
	}
	l.T.Logf(format, args...)
}

func (l *loggingTB) has(part string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.logs {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// TestTeardownAndServeErrorsAreReported (R2-P6-test-2519 a, b): an error
// of what the site closes, and a controller socket's serve that ends
// before the stack stopped it, reach the test's log instead of being
// dropped.
func TestTeardownAndServeErrorsAreReported(t *testing.T) {
	t.Parallel()
	tb := &loggingTB{T: t, logged: make(chan struct{}, 1)}
	s := NewSite(tb, Options{})
	s.logTeardown("probe", errors.New("refused"))
	if !tb.has("probe: refused") {
		t.Error("a teardown error was not logged")
	}
	c := s.AddController(ControllerOptions{})
	c.mu.RLock()
	l := c.listener
	c.mu.RUnlock()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(10 * time.Second)
	for !tb.has("the socket's serve ended") {
		select {
		case <-tb.logged:
		case <-timeout:
			t.Fatal("a serve that ended under the controller was not reported")
		}
	}
}
