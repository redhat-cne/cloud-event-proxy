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
	"bytes"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// AuthenticatedClient wraps an HTTP client with authentication capabilities
type AuthenticatedClient struct {
	client     *http.Client
	authConfig *AuthConfig
	oauthToken string
}

// NewAuthenticatedClient creates a new authenticated HTTP client
func NewAuthenticatedClient(authConfig *AuthConfig) (*AuthenticatedClient, error) {
	if authConfig == nil {
		// Return a basic HTTP client if no auth config is provided
		return &AuthenticatedClient{
			client: &http.Client{
				Timeout: 30 * time.Second,
			},
			authConfig: &AuthConfig{},
		}, nil
	}

	// Validate the configuration
	if err := authConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid authentication configuration: %v", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Configure TLS for mTLS if enabled
	if authConfig.EnableMTLS {
		tlsConfig, err := authConfig.CreateTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS configuration: %v", err)
		}

		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		log.Info("Configured HTTP client with mTLS")
	}

	// Get OAuth token if OAuth is enabled
	var oauthToken string
	if authConfig.EnableOAuth {
		token, err := authConfig.GetOAuthToken()
		if err != nil {
			return nil, fmt.Errorf("failed to get OAuth token: %v", err)
		}
		oauthToken = token
		log.Info("Configured HTTP client with OAuth token")
	}

	ac := &AuthenticatedClient{
		client:     client,
		authConfig: authConfig,
		oauthToken: oauthToken,
	}

	log.Info("Created authenticated HTTP client")
	return ac, nil
}

// Do performs an HTTP request with authentication
func (ac *AuthenticatedClient) Do(req *http.Request) (*http.Response, error) {
	// Add OAuth token if available
	if ac.authConfig.EnableOAuth && ac.oauthToken != "" {
		req.Header.Set("Authorization", "Bearer "+ac.oauthToken)
	}

	// Add content-type if not already set
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return ac.client.Do(req)
}

// Get performs a GET request with authentication
func (ac *AuthenticatedClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %v", err)
	}

	return ac.Do(req)
}

// Post performs a POST request with authentication
func (ac *AuthenticatedClient) Post(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %v", err)
	}

	return ac.Do(req)
}

// Put performs a PUT request with authentication
func (ac *AuthenticatedClient) Put(url string) (*http.Response, error) {
	req, err := http.NewRequest("PUT", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create PUT request: %v", err)
	}

	return ac.Do(req)
}

// Delete performs a DELETE request with authentication
func (ac *AuthenticatedClient) Delete(url string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DELETE request: %v", err)
	}

	return ac.Do(req)
}

// GetClient returns the underlying HTTP client
func (ac *AuthenticatedClient) GetClient() *http.Client {
	return ac.client
}

// GetAuthConfig returns the authentication configuration
func (ac *AuthenticatedClient) GetAuthConfig() *AuthConfig {
	return ac.authConfig
}

// RefreshOAuthToken refreshes the OAuth token from the service account token file
func (ac *AuthenticatedClient) RefreshOAuthToken() error {
	if !ac.authConfig.EnableOAuth {
		return nil
	}

	token, err := ac.authConfig.GetOAuthToken()
	if err != nil {
		return fmt.Errorf("failed to refresh OAuth token: %v", err)
	}

	ac.oauthToken = token
	log.Info("Refreshed OAuth token")
	return nil
}

// IsAuthenticated returns true if the client is configured with authentication
func (ac *AuthenticatedClient) IsAuthenticated() bool {
	return ac.authConfig.IsAuthenticationEnabled()
}
