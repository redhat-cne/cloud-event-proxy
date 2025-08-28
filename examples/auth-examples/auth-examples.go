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

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redhat-cne/cloud-event-proxy/pkg/auth"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	log "github.com/sirupsen/logrus"
)

// AuthenticatedConsumerExample demonstrates how to use the authentication features
// in the cloud-event-proxy consumer
func runAuthenticatedConsumerExample() {
	// Initialize logger
	log.SetLevel(log.InfoLevel)

	// Example 1: Basic authentication setup
	fmt.Println("=== Example 1: Basic Authentication Setup ===")
	basicAuthExample()

	// Example 2: mTLS only authentication
	fmt.Println("\n=== Example 2: mTLS Only Authentication ===")
	mtlsOnlyExample()

	// Example 3: OAuth only authentication
	fmt.Println("\n=== Example 3: OAuth Only Authentication ===")
	oauthOnlyExample()

	// Example 4: Combined mTLS + OAuth authentication
	fmt.Println("\n=== Example 4: Combined mTLS + OAuth Authentication ===")
	combinedAuthExample()

	// Example 5: Making authenticated requests
	fmt.Println("\n=== Example 5: Making Authenticated Requests ===")
	makeAuthenticatedRequestsExample()
}

// basicAuthExample shows how to create a basic authenticated client
func basicAuthExample() {
	// Create a basic authentication configuration
	authConfig := &auth.AuthConfig{
		EnableMTLS:  false,
		EnableOAuth: false,
	}

	// Create authenticated REST client
	client, err := restclient.NewAuthenticated(authConfig)
	if err != nil {
		log.Errorf("Failed to create authenticated client: %v", err)
		return
	}

	fmt.Printf("Created basic authenticated client: %+v\n", client)
}

// mtlsOnlyExample shows how to configure mTLS authentication
func mtlsOnlyExample() {
	// Create mTLS authentication configuration
	authConfig := &auth.AuthConfig{
		EnableMTLS:     true,
		UseServiceCA:   true,
		ClientCertPath: "/etc/cloud-event-consumer/client-certs/tls.crt",
		ClientKeyPath:  "/etc/cloud-event-consumer/client-certs/tls.key",
		CACertPath:     "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
		EnableOAuth:    false,
	}

	// Validate configuration
	if err := authConfig.Validate(); err != nil {
		log.Errorf("Invalid mTLS configuration: %v", err)
		return
	}

	// Create TLS configuration
	tlsConfig, err := authConfig.CreateTLSConfig()
	if err != nil {
		log.Errorf("Failed to create TLS configuration: %v", err)
		return
	}

	fmt.Printf("Created mTLS configuration: %+v\n", tlsConfig != nil)
	fmt.Printf("Authentication summary:\n%s", authConfig.GetConfigSummary())
}

// oauthOnlyExample shows how to configure OAuth authentication
func oauthOnlyExample() {
	// Create OAuth authentication configuration
	authConfig := &auth.AuthConfig{
		EnableMTLS:          false,
		EnableOAuth:         true,
		UseOpenShiftOAuth:   true,
		OAuthIssuer:         "https://oauth-openshift.apps.your-cluster.com",
		OAuthJWKSURL:        "https://oauth-openshift.apps.your-cluster.com/oauth/jwks",
		RequiredScopes:      []string{"user:info"},
		RequiredAudience:    "openshift",
		ServiceAccountName:  "consumer-sa",
		ServiceAccountToken: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}

	// Validate configuration
	if err := authConfig.Validate(); err != nil {
		log.Errorf("Invalid OAuth configuration: %v", err)
		return
	}

	fmt.Printf("Created OAuth configuration\n")
	fmt.Printf("Authentication summary:\n%s", authConfig.GetConfigSummary())
}

// combinedAuthExample shows how to configure both mTLS and OAuth
func combinedAuthExample() {
	// Create combined authentication configuration
	authConfig := &auth.AuthConfig{
		// mTLS configuration
		EnableMTLS:     true,
		UseServiceCA:   true,
		ClientCertPath: "/etc/cloud-event-consumer/client-certs/tls.crt",
		ClientKeyPath:  "/etc/cloud-event-consumer/client-certs/tls.key",
		CACertPath:     "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",

		// OAuth configuration
		EnableOAuth:         true,
		UseOpenShiftOAuth:   true,
		OAuthIssuer:         "https://oauth-openshift.apps.your-cluster.com",
		OAuthJWKSURL:        "https://oauth-openshift.apps.your-cluster.com/oauth/jwks",
		RequiredScopes:      []string{"user:info"},
		RequiredAudience:    "openshift",
		ServiceAccountName:  "consumer-sa",
		ServiceAccountToken: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}

	// Validate configuration
	if err := authConfig.Validate(); err != nil {
		log.Errorf("Invalid combined configuration: %v", err)
		return
	}

	// Create authenticated client
	client, err := restclient.NewAuthenticated(authConfig)
	if err != nil {
		log.Errorf("Failed to create authenticated client: %v", err)
		return
	}

	fmt.Printf("Created combined authentication client\n")
	fmt.Printf("Authentication summary:\n%s", authConfig.GetConfigSummary())
	fmt.Printf("Client is authenticated: %t\n", client != nil)
}

// makeAuthenticatedRequestsExample shows how to make authenticated HTTP requests
func makeAuthenticatedRequestsExample() {
	// Load authentication configuration from file
	configPath := "auth-config-example.json"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("Configuration file %s not found, using basic client\n", configPath)
		client := restclient.New()
		makeBasicRequest(client)
		return
	}

	authConfig, err := auth.LoadAuthConfig(configPath)
	if err != nil {
		log.Errorf("Failed to load authentication configuration: %v", err)
		return
	}

	// Create authenticated client
	client, err := restclient.NewAuthenticated(authConfig)
	if err != nil {
		log.Errorf("Failed to create authenticated client: %v", err)
		return
	}

	// Make authenticated requests
	makeAuthenticatedRequest(client, "https://example.com/api/health")
	makeAuthenticatedRequest(client, "https://example.com/api/subscriptions")
}

// makeBasicRequest makes a basic HTTP request without authentication
func makeBasicRequest(client *restclient.Rest) {
	fmt.Println("Making basic HTTP request (no authentication)")
	// This would make a request to a public endpoint
	// For demonstration purposes, we'll just show the concept
	fmt.Println("Basic request completed")
}

// makeAuthenticatedRequest makes an authenticated HTTP request
func makeAuthenticatedRequest(client *restclient.Rest, url string) {
	fmt.Printf("Making authenticated request to: %s\n", url)

	// Create a mock request (in real usage, you would use the actual URL)
	// For demonstration, we'll show how the authenticated client would be used

	// The authenticated client automatically handles:
	// 1. mTLS certificate validation
	// 2. OAuth token injection
	// 3. Proper headers

	fmt.Printf("Authenticated request to %s completed\n", url)
}

// loadConfigFromFile demonstrates how to load configuration from a JSON file
func loadConfigFromFile(filename string) (*auth.AuthConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var config auth.AuthConfig
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &config, nil
}

// saveConfigToFile demonstrates how to save configuration to a JSON file
func saveConfigToFile(config *auth.AuthConfig, filename string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err = os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// Example of how to use the authentication in a real application
func realWorldExample() {
	fmt.Println("Demonstrating real-world authentication configuration management:")

	// 1. Create a sample configuration
	sampleConfig := &auth.AuthConfig{
		EnableMTLS:     true,
		ClientCertPath: "/etc/ssl/certs/client.crt",
		ClientKeyPath:  "/etc/ssl/private/client.key",
		CACertPath:     "/etc/ssl/certs/ca.crt",
		UseServiceCA:   true,
	}

	// 2. Save configuration to a temporary file (demonstrates saveConfigToFile)
	tempConfigFile := "/tmp/sample-auth-config.json"
	fmt.Printf("Saving sample configuration to %s\n", tempConfigFile)
	if err := saveConfigToFile(sampleConfig, tempConfigFile); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		return
	}

	// 3. Load configuration from file (demonstrates loadConfigFromFile)
	fmt.Printf("Loading configuration from %s\n", tempConfigFile)
	loadedConfig, err := loadConfigFromFile(tempConfigFile)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	// 4. Verify the loaded configuration
	fmt.Printf("Loaded configuration: mTLS=%t, ServiceCA=%t\n", loadedConfig.EnableMTLS, loadedConfig.UseServiceCA)

	// 5. Create authenticated REST client with loaded config
	_, err = restclient.NewAuthenticated(loadedConfig)
	if err != nil {
		fmt.Printf("Note: Failed to create authenticated client (expected in demo): %v\n", err)
	} else {
		fmt.Println("Successfully created authenticated client")
	}

	// 6. Clean up temporary file
	os.Remove(tempConfigFile)
	fmt.Println("Real-world example completed - demonstrated config save/load functionality")
}

// main function to run the authentication examples
func main() {
	fmt.Println("=== Running Authentication Examples ===")

	fmt.Println("\n1. Basic Auth Example:")
	basicAuthExample()

	fmt.Println("\n2. mTLS Only Example:")
	mtlsOnlyExample()

	fmt.Println("\n3. OAuth Only Example:")
	oauthOnlyExample()

	fmt.Println("\n4. Combined Auth Example:")
	combinedAuthExample()

	fmt.Println("\n5. Authenticated Requests Example:")
	makeAuthenticatedRequestsExample()

	fmt.Println("\n6. Real World Example:")
	realWorldExample()

	fmt.Println("\n7. Full Consumer Example:")
	runAuthenticatedConsumerExample()
}
