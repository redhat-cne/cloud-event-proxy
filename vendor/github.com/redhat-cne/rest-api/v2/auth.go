// Copyright 2025 The Cloud Native Events Authors
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

package restapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

// initMTLSCACertPool initializes the CA certificate pool for mTLS
func (s *Server) initMTLSCACertPool() error {
	if s.authConfig == nil || !s.authConfig.EnableMTLS || s.authConfig.CACertPath == "" {
		return nil
	}

	caCert, err := os.ReadFile(s.authConfig.CACertPath)
	if err != nil {
		log.Errorf("failed to read CA certificate: %v", err)
		return err
	}

	s.caCertPool = x509.NewCertPool()
	if !s.caCertPool.AppendCertsFromPEM(caCert) {
		log.Error("failed to parse CA certificate")
		return fmt.Errorf("failed to parse CA certificate")
	}

	log.Info("mTLS CA certificate pool initialized")
	return nil
}

// TokenInfo carries the authenticated identity returned by a TokenValidator.
type TokenInfo struct {
	Username  string
	UID       string
	Groups    []string
	Audiences []string
}

// TokenValidator validates an OAuth 2.0 / OIDC bearer token and returns the
// authenticated identity. Implementations are supplied by the embedding
// application (e.g. cloud-event-proxy uses the Kubernetes TokenReview API) so
// that this library remains free of any Kubernetes client dependency.
//
// A validator MUST cryptographically verify the token (signature and, when
// audiences are supplied, audience binding). Returning a non-nil error MUST
// cause the request to be rejected with 401.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string, audiences []string) (*TokenInfo, error)
}

// SetTokenValidator installs the bearer-token validator used when OAuth is
// enabled. When OAuth is enabled and no validator is installed, all
// non-localhost requests fail closed (401).
func (s *Server) SetTokenValidator(v TokenValidator) {
	s.tokenValidator = v
}

// isLoopbackRemoteAddr reports whether the request originates from the local
// loopback interface (same pod). Such requests never leave the pod's network
// namespace and are treated as a trusted fast-path.
func isLoopbackRemoteAddr(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// validateEndpointURI performs SSRF hardening on a caller-supplied callback /
// endpoint URI (subscriber EndpointUri or publisher endpoint). Per O-RAN
// RHT-0003 the host may be localhost, an IP, or an FQDN, so those are allowed;
// link-local, cloud-metadata, multicast and unspecified addresses are rejected.
func validateEndpointURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid EndpointUri %q: %v", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("EndpointUri scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("EndpointUri host is empty")
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(), ip.IsUnspecified():
			return fmt.Errorf("EndpointUri host %q is not an allowed address", host)
		case ip.Equal(net.ParseIP("169.254.169.254")):
			return fmt.Errorf("EndpointUri host %q (cloud metadata) is not allowed", host)
		}
	}
	return nil
}

// tlsVersionFromString maps a TLS version name (as used by the OpenShift
// TLSSecurityProfile / crypto/tls) to its constant. Returns 0 when unknown.
func tlsVersionFromString(v string) uint16 {
	switch strings.TrimSpace(v) {
	case "VersionTLS10":
		return tls.VersionTLS10
	case "VersionTLS11":
		return tls.VersionTLS11
	case "VersionTLS12":
		return tls.VersionTLS12
	case "VersionTLS13":
		return tls.VersionTLS13
	default:
		return 0
	}
}

// cipherSuitesFromNames maps IANA cipher suite names to their crypto/tls IDs.
// Unknown names are ignored. TLS 1.3 cipher suites are not configurable in Go
// and are silently dropped, which is expected.
func cipherSuitesFromNames(names []string) []uint16 {
	if len(names) == 0 {
		return nil
	}
	lookup := make(map[string]uint16)
	for _, cs := range tls.CipherSuites() {
		lookup[cs.Name] = cs.ID
	}
	for _, cs := range tls.InsecureCipherSuites() {
		lookup[cs.Name] = cs.ID
	}
	var out []uint16
	for _, n := range names {
		if id, ok := lookup[strings.TrimSpace(n)]; ok {
			out = append(out, id)
		}
	}
	return out
}

// ApplyTLSProfile applies the centrally-managed TLS profile (min version and
// cipher suites, sourced from the cluster's TLSSecurityProfile via the
// operator) onto a tls.Config. Nothing is hardcoded here: values come from the
// AuthConfig. Only when no min version is supplied at all do we fall back to
// TLS 1.2 to avoid negotiating an insecure protocol by default.
func (c *AuthConfig) ApplyTLSProfile(cfg *tls.Config) {
	if c == nil {
		return
	}
	if mv := tlsVersionFromString(c.TLSMinVersion); mv != 0 {
		cfg.MinVersion = mv
	} else if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}
	if cs := cipherSuitesFromNames(c.TLSCipherSuites); len(cs) > 0 {
		cfg.CipherSuites = cs
	}
}

// combinedAuthMiddleware enforces mTLS and/or OAuth on protected endpoints.
//
// Requests from the local loopback interface (same pod) are treated as a
// trusted fast-path and skip authentication - they never leave the pod. All
// other (FQDN / service-DNS / external) requests must satisfy every enabled
// mechanism: a verified client certificate when mTLS is enabled, and a valid
// bearer token when OAuth is enabled.
func (s *Server) combinedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Trusted loopback fast-path (same pod, e.g. in-process producer self-call).
		if isLoopbackRemoteAddr(r.RemoteAddr) {
			log.Debugf("allowing loopback connection from %s for %s", r.RemoteAddr, r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}

		// mTLS: the TLS handshake (ClientAuth: VerifyClientCertIfGiven) has
		// already verified any presented certificate chain against the CA pool;
		// here we require that a client certificate was in fact presented.
		if s.authConfig != nil && s.authConfig.EnableMTLS {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				log.Warnf("mTLS required but no client certificate provided for %s", r.URL.Path)
				http.Error(w, "Client certificate required", http.StatusUnauthorized)
				return
			}
			log.Debugf("client certificate present and verified for %s", r.URL.Path)
		}

		// OAuth: require and validate a bearer token.
		if s.authConfig != nil && s.authConfig.EnableOAuth {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Warnf("OAuth required but no Authorization header provided for %s", r.URL.Path)
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}
			if !strings.HasPrefix(authHeader, "Bearer ") {
				log.Warnf("invalid Authorization header format for %s", r.URL.Path)
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

			// Fail closed: OAuth is enabled but no validator was installed.
			if s.tokenValidator == nil {
				log.Error("OAuth enabled but no TokenValidator configured; rejecting request")
				http.Error(w, "token validation unavailable", http.StatusUnauthorized)
				return
			}

			info, err := s.tokenValidator.ValidateToken(r.Context(), token, s.authConfig.RequiredAudiences)
			if err != nil {
				log.Warnf("OAuth token validation failed for %s: %v", r.URL.Path, err)
				http.Error(w, "Invalid OAuth token", http.StatusUnauthorized)
				return
			}
			log.Debugf("OAuth token validated for %s (user=%s)", r.URL.Path, info.Username)
		}

		next.ServeHTTP(w, r)
	})
}
