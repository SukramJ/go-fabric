// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"net"
	"net/url"
	"strings"
)

// ValidPushURL is connectedhomeip's ValidateUrl
// (PushAVStreamTransportLogic.cpp:293-356) over net/url in place of
// uriparser: a well-formed URI of the https scheme (any case) with a host,
// a path that ends in "/", and neither a query nor a fragment. uriparser's
// strictness is matched by refusing a character RFC 3986 does not admit, a
// host that is neither a reg-name nor a bracketed IP literal, and a
// non-numeric port — the cases of tests/TestURLParsing.cpp.
func ValidPushURL(raw string) bool {
	for _, r := range raw {
		if !uriChar(r) {
			return false
		}
	}
	if strings.ContainsAny(raw, "?#") { // :344-353
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") { // :330-337
		return false
	}
	if u.User != nil && strings.Contains(u.User.String(), "%40") { // RFC 3986 userinfo admits no "@"
		return false
	}
	if !validHost(u.Host) { // :338-341
		return false
	}
	return strings.HasSuffix(u.EscapedPath(), "/") // :342-343
}

// uriChar is an RFC 3986 character: unreserved, reserved or '%'.
func uriChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", r)
}

// validHost is a non-empty reg-name or bracketed IP literal with an
// optional numeric port.
func validHost(host string) bool {
	if host == "" {
		return false
	}
	name, port := host, ""
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end < 0 {
			return false
		}
		name, port = host[:end+1], host[end+1:]
		inner := name[1 : len(name)-1]
		if ip := net.ParseIP(inner); ip == nil || !strings.Contains(inner, ":") {
			return false
		}
	} else if i := strings.IndexByte(host, ':'); i >= 0 {
		name, port = host[:i], host[i:]
		if name == "" || strings.ContainsAny(name, "[]") {
			return false
		}
	}
	if port != "" {
		digits := strings.TrimPrefix(port, ":")
		if port[0] != ':' || digits == "" || strings.Trim(digits, "0123456789") != "" {
			return false
		}
	}
	return name != ""
}
