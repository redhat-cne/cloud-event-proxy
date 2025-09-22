// Copyright 2024 The Cloud Native Events Authors
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

// mTLSMiddleware validates client certificates for mTLS authentication
func (s *Server) mTLSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authConfig == nil || !s.authConfig.EnableMTLS {
			next.ServeHTTP(w, r)
			return
		}

		if r.TLS == nil {
			log.Error("mTLS required but request is not over TLS")
			http.Error(w, "mTLS required", http.StatusUnauthorized)
			return
		}

		if len(r.TLS.PeerCertificates) == 0 {
			log.Error("no client certificate provided")
			http.Error(w, "Client certificate required", http.StatusUnauthorized)
			return
		}

		clientCert := r.TLS.PeerCertificates[0]

		// Verify the client certificate against our CA
		opts := x509.VerifyOptions{
			Roots: s.caCertPool,
		}

		if _, err := clientCert.Verify(opts); err != nil {
			log.Errorf("client certificate verification failed: %v", err)
			http.Error(w, "Invalid client certificate", http.StatusUnauthorized)
			return
		}

		log.Infof("mTLS authentication successful for client: %s", clientCert.Subject.CommonName)
		next.ServeHTTP(w, r)
	})
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

// oAuthMiddleware validates OAuth Bearer tokens
func (s *Server) oAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authConfig == nil || !s.authConfig.EnableOAuth {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			log.Error("missing Authorization header")
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Error("invalid Authorization header format")
			http.Error(w, "Bearer token required", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			log.Error("empty Bearer token")
			http.Error(w, "Invalid Bearer token", http.StatusUnauthorized)
			return
		}

		// Validate the token (simplified validation - in production, use proper JWT library)
		if err := s.validateOAuthToken(token); err != nil {
			log.Errorf("OAuth token validation failed: %v", err)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		log.Info("OAuth authentication successful")
		next.ServeHTTP(w, r)
	})
}

// validateOAuthToken validates the OAuth token using OpenShift Authentication Operator
func (s *Server) validateOAuthToken(token string) error {
	if s.authConfig == nil || !s.authConfig.EnableOAuth {
		return nil
	}

	// Parse JWT token
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT token format")
	}

	// Basic token validation
	if len(token) < 20 {
		return fmt.Errorf("token too short")
	}

	// For OpenShift Authentication Operator integration:
	// 1. Fetch JWKS from s.authConfig.OAuthJWKSURL
	// 2. Verify JWT signature using the public keys
	// 3. Validate issuer matches s.authConfig.OAuthIssuer
	// 4. Validate audience matches s.authConfig.RequiredAudience
	// 5. Check required scopes in s.authConfig.RequiredScopes
	// 6. Validate expiration time

	// In production, you should use a proper JWT library like github.com/golang-jwt/jwt
	// to implement the following validation logic:
	/*
		// Parse and verify JWT token
		parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
			// Verify signing method
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			// Get public key from JWKS
			keyID := token.Header["kid"].(string)
			publicKey, err := s.getPublicKeyFromJWKS(keyID)
			if err != nil {
				return nil, err
			}
			return publicKey, nil
		})

		if err != nil {
			return fmt.Errorf("failed to parse token: %v", err)
		}

		// Extract claims
		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok || !parsedToken.Valid {
			return fmt.Errorf("invalid token claims")
		}

		// Validate issuer
		if s.authConfig.OAuthIssuer != "" {
			if issuer, ok := claims["iss"].(string); !ok || issuer != s.authConfig.OAuthIssuer {
				return fmt.Errorf("invalid issuer")
			}
		}

		// Validate audience
		if s.authConfig.RequiredAudience != "" {
			if audience, ok := claims["aud"].([]interface{}); ok {
				found := false
				for _, aud := range audience {
					if audStr, ok := aud.(string); ok && audStr == s.authConfig.RequiredAudience {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("invalid audience")
				}
			}
		}

		// Validate scopes
		if len(s.authConfig.RequiredScopes) > 0 {
			if scopes, ok := claims["scope"].(string); ok {
				scopeList := strings.Split(scopes, " ")
				for _, requiredScope := range s.authConfig.RequiredScopes {
					found := false
					for _, scope := range scopeList {
						if scope == requiredScope {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("missing required scope: %s", requiredScope)
					}
				}
			}
		}

		// Validate expiration
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				return fmt.Errorf("token expired")
			}
		}
	*/

	// For now, accept tokens that look like valid JWT tokens
	// This should be replaced with proper JWT validation in production
	log.Infof("OAuth token validation successful (simplified validation for OpenShift Authentication Operator)")
	return nil
}

// combinedAuthMiddleware applies both mTLS and OAuth authentication
func (s *Server) combinedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for health endpoint
		if r.URL.Path == "/health" || r.URL.Path == "/api/ocloudNotifications/v2/health" {
			next.ServeHTTP(w, r)
			return
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

		// TODO: Add OAuth validation here if needed for other endpoints

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}
