// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

// The zone polygon rules of connectedhomeip's ZoneGeometry
// (src/app/clusters/zone-management-server/zone-geometry.h at 6170af84):
// a zone is a simple polygon, so a repeated vertex, three collinear
// vertices of a triangle, or two intersecting non-adjacent edges make it
// self-intersecting. The rules are ported, not the file.

// orientation is the side of ray pq that r lies on: 0 collinear, 1 left
// (counter-clockwise), 2 right, from the cross product of pq and pr
// (zone-geometry.h:237-249).
func orientation(p, q, r Vertex) int {
	v := (int64(q.X)-int64(p.X))*(int64(r.Y)-int64(p.Y)) - (int64(q.Y)-int64(p.Y))*(int64(r.X)-int64(p.X))
	switch {
	case v == 0:
		return 0
	case v > 0:
		return 1
	}
	return 2
}

// onSegment reports whether q lies within the bounding box of p and r,
// for collinear p, q, r (zone-geometry.h:216-220).
func onSegment(p, q, r Vertex) bool {
	return q.X <= max(p.X, r.X) && q.X >= min(p.X, r.X) && q.Y <= max(p.Y, r.Y) && q.Y >= min(p.Y, r.Y)
}

// segmentsIntersect reports whether p1q1 and p2q2 share a point
// (zone-geometry.h:252-290).
func segmentsIntersect(p1, q1, p2, q2 Vertex) bool {
	o1, o2 := orientation(p1, q1, p2), orientation(p1, q1, q2)
	o3, o4 := orientation(p2, q2, p1), orientation(p2, q2, q1)
	if o1 != 0 && o2 != 0 && o3 != 0 && o4 != 0 && o1 != o2 && o3 != o4 {
		return true
	}
	return o1 == 0 && onSegment(p1, p2, q1) || o2 == 0 && onSegment(p1, q2, q1) ||
		o3 == 0 && onSegment(p2, p1, q2) || o4 == 0 && onSegment(p2, q1, q2)
}

// ZoneSelfIntersecting reports whether the closed polygon of vertices is
// not simple: a repeated vertex, a triangle of collinear vertices, or two
// intersecting non-adjacent edges. Mirrors chip
// ZoneGeometry::IsZoneSelfIntersecting (zone-geometry.h:158-209) and
// ZoneHasDuplicates (:304-317).
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
	for i := range n {
		p1, q1 := vertices[i], vertices[(i+1)%n]
		for j := i + 2; j < n; j++ {
			if (j+1)%n == i { // the edge adjacent to edge i
				continue
			}
			if segmentsIntersect(p1, q1, vertices[j], vertices[(j+1)%n]) {
				return true
			}
		}
	}
	return false
}
