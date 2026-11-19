// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"errors"

	"github.com/SukramJ/go-fabric/internal/exchangeseam"
)

// The one-way seam for a test outside this package that stands in for
// traffic it did not send; see the exchangeseam package doc for why it is
// not a method on Session.
func init() {
	exchangeseam.NotifyActivity = func(sess any, received bool) {
		if s, ok := sess.(*Session); ok {
			s.notifyActivity(received)
		}
	}
	exchangeseam.NewEstablishingSecureSession = func(mgr, cfg any) (any, error) {
		m, ok := mgr.(*Manager)
		if !ok {
			return nil, errors.New("exchange: establishing session on a foreign manager")
		}
		c, ok := cfg.(SecureSessionConfig)
		if !ok {
			return nil, errors.New("exchange: establishing session with a foreign config")
		}
		s, err := m.newSecureSession(c, true)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	exchangeseam.ActivateSecureSession = func(sess any) {
		if s, ok := sess.(*Session); ok {
			s.activate()
		}
	}
}
