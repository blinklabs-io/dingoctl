// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"

	"golang.org/x/net/http2"
)

// buildTransport returns an http.RoundTripper appropriate for cfg.
//
//   - TLS=false → plain-text HTTP/2 (h2c) so ConnectRPC streams work without
//     certificates.
//   - TLS=true, Insecure=false → standard TLS; Go auto-negotiates HTTP/2 via
//     ALPN.  Optional custom CA and client keypair for mTLS.
//   - TLS=true, Insecure=true → same as above but cert verification is
//     skipped; intended only for local development.
func buildTransport(cfg Config) (http.RoundTripper, error) {
	if !cfg.TLS {
		// Plain-text HTTP/2 (h2c).  DialTLSContext is used by the http2
		// package even for cleartext connections when AllowHTTP is set; we
		// return a plain TCP dial so no TLS handshake takes place.
		return &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		}, nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.Insecure, //nolint:gosec // controlled by --insecure flag
		MinVersion:         tls.VersionTLS12,
	}

	if cfg.CACert != "" {
		pool := x509.NewCertPool()
		pem, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("reading CA cert %q: %w", cfg.CACert, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no valid PEM certificates found in %q", cfg.CACert)
		}
		tlsCfg.RootCAs = pool
	}

	if cfg.ClientCert != "" || cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("loading mTLS keypair (%q, %q): %w", cfg.ClientCert, cfg.ClientKey, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// ForceAttemptHTTP2 ensures the transport proactively tries HTTP/2 even
	// on non-default TLS configs (Go disables it otherwise).
	return &http.Transport{
		TLSClientConfig:   tlsCfg,
		ForceAttemptHTTP2: true,
	}, nil
}

// serverBaseURL constructs the scheme+authority URL for cfg.
func serverBaseURL(cfg Config) string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + cfg.Address
}
