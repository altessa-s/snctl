// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package domain holds the local profile model without I/O.
package domain

import (
	"errors"
	"net/url"
	"strings"
)

// OfficialEndpoint selects the official Supernote Cloud.
const OfficialEndpoint = "official"

// ErrEndpoint means an endpoint is neither official nor a private HTTP(S) URL.
var ErrEndpoint = errors.New("endpoint must be official or an HTTP(S) URL without credentials, query or fragment")

// Endpoint validates and normalizes a profile's endpoint. Plain HTTP is
// allowed because Supernote Private Cloud may be hosted on a local network.
func Endpoint(value string) (string, error) {
	if value == "" || value == OfficialEndpoint {
		return OfficialEndpoint, nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", ErrEndpoint
	}
	return strings.TrimRight(u.String(), "/"), nil
}
