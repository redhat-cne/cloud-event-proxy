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

package auth

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"

	restapi "github.com/redhat-cne/rest-api/v2"
	log "github.com/sirupsen/logrus"
)

// ClientAuthConfig extends the base AuthConfig with client-specific certificate paths
type ClientAuthConfig struct {
	*restapi.AuthConfig
	// Client-specific certificate paths (different from server paths in base AuthConfig)
	ClientCertPath string `json:"clientCertPath"`
	ClientKeyPath  string `json:"clientKeyPath"`
}

// AuthConfig is an alias for ClientAuthConfig for backward compatibility
type AuthConfig = ClientAuthConfig

// LoadAuthConfig loads authentication configuration from a JSON file
func LoadAuthConfig(configPath string) (*ClientAuthConfig, error) {
	if configPath == "" {
		log.Info("No authentication config path provided, using default (no auth)")
		return &ClientAuthConfig{
			AuthConfig: &restapi.AuthConfig{},
		}, nil
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("authentication config file not found: %s", configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read authentication config file %s: %v", configPath, err)
	}

	var config ClientAuthConfig
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal authentication config: %v", err)
	}

	// Initialize embedded AuthConfig if it's nil
	if config.AuthConfig == nil {
		config.AuthConfig = &restapi.AuthConfig{}
	}

	log.Infof("Loaded authentication config from %s", configPath)
	return &config, nil
}

// Validate validates the authentication configuration
func (c *ClientAuthConfig) Validate() error {
	if c.EnableMTLS {
		if c.ClientCertPath == "" {
			return fmt.Errorf("client certificate path is required when mTLS is enabled")
		}
		if c.ClientKeyPath == "" {
			return fmt.Errorf("client key path is required when mTLS is enabled")
		}
		if c.CACertPath == "" {
			return fmt.Errorf("CA certificate path is required when mTLS is enabled")
		}

		// Check if certificate files exist
		if _, err := os.Stat(c.ClientCertPath); os.IsNotExist(err) {
			return fmt.Errorf("client certificate file not found: %s", c.ClientCertPath)
		}
		if _, err := os.Stat(c.ClientKeyPath); os.IsNotExist(err) {
			return fmt.Errorf("client key file not found: %s", c.ClientKeyPath)
		}
		if _, err := os.Stat(c.CACertPath); os.IsNotExist(err) {
			return fmt.Errorf("CA certificate file not found: %s", c.CACertPath)
		}
	}

	if c.EnableOAuth {
		if c.OAuthIssuer == "" {
			return fmt.Errorf("OAuth issuer is required when OAuth is enabled")
		}
		if c.OAuthJWKSURL == "" {
			return fmt.Errorf("OAuth JWKS URL is required when OAuth is enabled")
		}
		if c.ServiceAccountToken == "" {
			return fmt.Errorf("service account token path is required when OAuth is enabled")
		}

		// Check if service account token file exists
		if _, err := os.Stat(c.ServiceAccountToken); os.IsNotExist(err) {
			return fmt.Errorf("service account token file not found: %s", c.ServiceAccountToken)
		}
	}

	return nil
}

// CreateTLSConfig creates a TLS configuration for mTLS
func (c *ClientAuthConfig) CreateTLSConfig() (*tls.Config, error) {
	if !c.EnableMTLS {
		return nil, nil
	}

	// Load client certificate
	cert, err := tls.LoadX509KeyPair(c.ClientCertPath, c.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %v", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(c.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %v", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // Skip hostname verification for localhost connections
		// Note: Server certificates from Service CA may not support client authentication
		// This is acceptable for internal localhost connections
	}

	log.Info("Created TLS configuration for mTLS")
	return tlsConfig, nil
}

// GetOAuthToken reads the OAuth token from the service account token file
func (c *ClientAuthConfig) GetOAuthToken() (string, error) {
	if !c.EnableOAuth {
		return "", nil
	}

	token, err := os.ReadFile(c.ServiceAccountToken)
	if err != nil {
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}

	return string(token), nil
}

// IsAuthenticationEnabled returns true if either mTLS or OAuth is enabled
func (c *ClientAuthConfig) IsAuthenticationEnabled() bool {
	return c.EnableMTLS || c.EnableOAuth
}

// GetConfigSummary returns a summary of the authentication configuration
func (c *ClientAuthConfig) GetConfigSummary() string {
	summary := "Authentication Configuration:\n"
	summary += fmt.Sprintf("  mTLS: %t\n", c.EnableMTLS)
	if c.EnableMTLS {
		summary += fmt.Sprintf("    Client Cert: %s\n", c.ClientCertPath)
		summary += fmt.Sprintf("    Client Key: %s\n", c.ClientKeyPath)
		summary += fmt.Sprintf("    CA Cert: %s\n", c.CACertPath)
		summary += fmt.Sprintf("    Use Service CA: %t\n", c.UseServiceCA)
	}
	summary += fmt.Sprintf("  OAuth: %t\n", c.EnableOAuth)
	if c.EnableOAuth {
		summary += fmt.Sprintf("    Issuer: %s\n", c.OAuthIssuer)
		summary += fmt.Sprintf("    JWKS URL: %s\n", c.OAuthJWKSURL)
		summary += fmt.Sprintf("    Service Account: %s\n", c.ServiceAccountName)
		summary += fmt.Sprintf("    Token Path: %s\n", c.ServiceAccountToken)
		summary += fmt.Sprintf("    Use OpenShift OAuth: %t\n", c.UseOpenShiftOAuth)
	}
	return summary
}
