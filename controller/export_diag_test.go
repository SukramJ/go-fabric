// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import "github.com/SukramJ/go-fabric/controller/client"

// WatchingICD reports whether c watches n's ICD Check-Ins.
func WatchingICD(c *Controller, n *client.Node) bool { return c.watchingICD(n) }
