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
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// OAuthClaims represents the claims in an OAuth JWT token
type OAuthClaims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  []string `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	Scopes    []string `json:"scope"`
}

// validateOAuthToken validates the OAuth JWT token
func (s *Server) validateOAuthToken(tokenString string) (*OAuthClaims, error) {
	if s.authConfig == nil || !s.authConfig.EnableOAuth {
		return nil, fmt.Errorf("OAuth not enabled")
	}

	// Parse the token without verification first to get the issuer
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %v", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate issuer
	issuer, ok := claims["iss"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid issuer in token")
	}

	// Accept both OpenShift OAuth tokens and Kubernetes ServiceAccount tokens
	validIssuers := []string{
		s.authConfig.OAuthIssuer,                       // OpenShift OAuth server
		"https://kubernetes.default.svc.cluster.local", // Kubernetes ServiceAccount tokens (full)
		"https://kubernetes.default.svc",               // Kubernetes ServiceAccount tokens (short)
		"kubernetes.default.svc.cluster.local",         // Kubernetes ServiceAccount tokens (no https)
		"kubernetes.default.svc",                       // Kubernetes ServiceAccount tokens (minimal)
	}

	issuerValid := false
	for _, validIssuer := range validIssuers {
		if issuer == validIssuer {
			issuerValid = true
			break
		}
	}

	if !issuerValid {
		return nil, fmt.Errorf("token issuer not accepted: got %s, expected one of: %v", issuer, validIssuers)
	}

	// Validate expiration
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("token expired")
		}
	} else {
		return nil, fmt.Errorf("missing or invalid expiration in token")
	}

	// Validate audience if required
	if len(s.authConfig.RequiredAudience) > 0 {
		var audiences []string
		if aud, ok := claims["aud"].([]interface{}); ok {
			for _, a := range aud {
				if audStr, ok := a.(string); ok {
					audiences = append(audiences, audStr)
				}
			}
		} else if audStr, ok := claims["aud"].(string); ok {
			audiences = []string{audStr}
		}

		audienceValid := false
		for _, aud := range audiences {
			if aud == s.authConfig.RequiredAudience {
				audienceValid = true
				break
			}
		}
		if !audienceValid {
			return nil, fmt.Errorf("token audience validation failed")
		}
	}

	// Convert to OAuthClaims struct
	oauthClaims := &OAuthClaims{
		Issuer:    issuer,
		ExpiresAt: int64(claims["exp"].(float64)),
	}

	if sub, ok := claims["sub"].(string); ok {
		oauthClaims.Subject = sub
	}

	if iat, ok := claims["iat"].(float64); ok {
		oauthClaims.IssuedAt = int64(iat)
	}

	return oauthClaims, nil
}

// combinedAuthMiddleware applies both mTLS and OAuth authentication
func (s *Server) combinedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for localhost connections (same pod)
		if r.RemoteAddr != "" {
			host := r.RemoteAddr
			if idx := strings.LastIndex(host, ":"); idx != -1 {
				host = host[:idx] // Remove port
			}
			if host == "127.0.0.1" || host == "::1" || host == "[::1]" {
				log.Debugf("Allowing localhost connection from %s for %s", r.RemoteAddr, r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check for client certificate when mTLS is enabled
		if s.authConfig != nil && s.authConfig.EnableMTLS {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				log.Warnf("mTLS required but no client certificate provided for %s", r.URL.Path)
				http.Error(w, "Client certificate required", http.StatusUnauthorized)
				return
			}

			// Verify the client certificate against our CA
			cert := r.TLS.PeerCertificates[0]
			opts := x509.VerifyOptions{Roots: s.caCertPool}
			if _, err := cert.Verify(opts); err != nil {
				log.Warnf("Client certificate verification failed for %s: %v", r.URL.Path, err)
				http.Error(w, "Invalid client certificate", http.StatusUnauthorized)
				return
			}
			log.Debugf("Client certificate verified successfully for %s", r.URL.Path)
		}

		// Validate OAuth token if OAuth is enabled
		if s.authConfig != nil && s.authConfig.EnableOAuth {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Warnf("OAuth required but no Authorization header provided for %s", r.URL.Path)
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			// Extract Bearer token
			if !strings.HasPrefix(authHeader, "Bearer ") {
				log.Warnf("OAuth required but invalid Authorization header format for %s", r.URL.Path)
				http.Error(w, "Bearer token required", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			_, err := s.validateOAuthToken(token)
			if err != nil {
				log.Warnf("OAuth token validation failed for %s: %v", r.URL.Path, err)
				http.Error(w, "Invalid OAuth token", http.StatusUnauthorized)
				return
			}
			log.Debugf("OAuth token validated successfully for %s", r.URL.Path)
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}
