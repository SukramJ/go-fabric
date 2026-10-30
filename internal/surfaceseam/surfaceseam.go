// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package surfaceseam carries the one-way seam through which the
// surface/surfacetest support package gives a surface.Policies value a
// controller profile the register (notes/parity/controller_profiles.md)
// does not list.
//
// Direction of the edge: package surface installs [WithProfiles] from its
// init; surfacetest reads it. The generated profile table is data that
// only the register's evidence changes; a test that needs a Reproduced
// remove rule runs it through this seam instead of a register row, and no
// exported field of surface.Policies exists only to serve tests.
package surfaceseam

// WithProfiles returns policies (a surface.Policies, as any: surface
// imports this package to install the hook) with profiles (a
// []surface.Profile) known and selected. Reports false when either
// argument has another type.
var WithProfiles func(policies, profiles any) (any, bool)
