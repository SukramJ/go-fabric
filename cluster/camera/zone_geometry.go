// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

// The zone polygon rules of matter.js's TwoDCartesianGeometry
// (packages/node/src/behaviors/zone-management/TwoDCartesianGeometry.ts
// at b25ed960): a zone is a simple polygon, so a repeated vertex, three
// collinear vertices of a triangle, or two non-adjacent edges that share
// a point make it self-intersecting. connectedhomeip's ZoneGeometry
// (zone-geometry.h at 6170af84) states the same rules.

// orientation is the sign of the cross product of pq and pr: 1 or -1 for
// the two sides of the line through p and q, 0 when r is on that line.
// Mirrors matter.js TwoDCartesianGeometry.ts:63-69 at b25ed960.
func orientation(p, q, r Vertex) int {
	v := (int64(q.X)-int64(p.X))*(int64(r.Y)-int64(p.Y)) - (int64(q.Y)-int64(p.Y))*(int64(r.X)-int64(p.X))
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// onSegment reports whether r, collinear with pq, lies within the
// bounding box of pq and therefore on the segment. Mirrors matter.js
// TwoDCartesianGeometry.ts:71-78 at b25ed960.
func onSegment(p, q, r Vertex) bool {
	return r.X >= min(p.X, q.X) && r.X <= max(p.X, q.X) && r.Y >= min(p.Y, q.Y) && r.Y <= max(p.Y, q.Y)
}

// segmentsIntersect reports whether p1q1 and p2q2 share a point. Mirrors
// matter.js TwoDCartesianGeometry.ts:80-96 at b25ed960.
func segmentsIntersect(p1, q1, p2, q2 Vertex) bool {
	o1, o2 := orientation(p1, q1, p2), orientation(p1, q1, q2)
	o3, o4 := orientation(p2, q2, p1), orientation(p2, q2, q1)
	if o1 != o2 && o3 != o4 {
		return true
	}
	return o1 == 0 && onSegment(p1, q1, p2) || o2 == 0 && onSegment(p1, q1, q2) ||
		o3 == 0 && onSegment(p2, q2, p1) || o4 == 0 && onSegment(p2, q2, q1)
}

// ZoneSelfIntersecting reports whether the closed polygon of vertices is
// not simple: a repeated vertex, a triangle of collinear vertices, or,
// with more vertices, two non-adjacent edges that share a point — every
// self-intersection includes such a pair. Mirrors matter.js
// TwoDCartesianGeometry.ts:24-60 at b25ed960 (isSelfIntersecting).
func ZoneSelfIntersecting(vertices []Vertex) bool {
	seen := make(map[Vertex]bool, len(vertices))
	for _, v := range vertices {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	n := len(vertices)
	if n == 3 {
		return orientation(vertices[0], vertices[1], vertices[2]) == 0
	}
	for first := 0; first+1 < n; first++ {
		// The edge that closes the loop is adjacent to the first edge.
		end := n
		if first == 0 {
			end = n - 1
		}
		for second := first + 2; second < end; second++ {
			if segmentsIntersect(vertices[first], vertices[first+1], vertices[second], vertices[(second+1)%n]) {
				return true
			}
		}
	}
	return false
}
