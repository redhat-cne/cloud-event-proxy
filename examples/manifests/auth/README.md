# Authentication Configuration for Cloud Event Consumer

This guide explains how to use mTLS and OAuth authentication in the example cloud event consumer deployment. This example is designed to be generic and work with any Kubernetes cluster, not specific to OpenShift.

## Overview

The example consumer is configured to authenticate with the cloud-event-proxy server using:
- mTLS (Mutual TLS) for transport security
- OAuth with JWT tokens for client authentication

## Components

### Authentication Configuration

The authentication settings are stored in a ConfigMap (`consumer-auth-config`):
```yaml
data:
  config.json: |
    {
      "enableMTLS": true,
      "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
      "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
      "caCertPath": "/etc/cloud-event-consumer/ca-bundle/ca.crt",
      "enableOAuth": true,
      "oauthIssuer": "https://your-oauth-provider.com",
      "oauthJWKSURL": "https://your-oauth-provider.com/.well-known/jwks.json",
      "requiredScopes": ["subscription:create", "events:read"],
      "requiredAudience": "cloud-event-proxy"
    }
```

### Service Account

The consumer uses a dedicated service account (`consumer-sa`) with:
- RBAC permissions to access the cloud-event-proxy service
- Basic Kubernetes service account functionality
- No OpenShift-specific dependencies

### Certificate Management

The consumer accesses certificates through:
- Client certificates mounted from Kubernetes secrets
- CA bundle mounted from Kubernetes secrets
- Manual certificate management (no automatic rotation)
- Secure volume mounts in the pod

## Deployment

The authentication components are automatically deployed when you apply the example manifests:

```bash
# Create namespace and resources
kubectl apply -k examples/manifests/

# Verify the deployment
kubectl get deployment cloud-consumer-deployment -n cloud-events
```

## Configuration Options

### Authentication Settings

You can modify the authentication settings in `auth/configmap.yaml`:
- Enable/disable mTLS
- Enable/disable OAuth
- Configure required scopes and audience
- Adjust certificate paths

### Service Account Permissions

Adjust RBAC permissions in `auth/service-account.yaml`:
- Modify role permissions
- Add additional role bindings
- Configure access to other services

## Troubleshooting

### Certificate Issues

Check the mounted certificates:
```bash
# View CA bundle secret
kubectl get secret server-ca-bundle -n cloud-events -o yaml

# Check client certificates
kubectl get secret consumer-client-certs -n cloud-events -o yaml

# Check certificate mounting in pod
kubectl describe pod -l app=consumer -n cloud-events
```

### Authentication Errors

Check the consumer logs:
```bash
# View pod logs
kubectl logs deployment/cloud-consumer-deployment -n cloud-events

# Check service account
kubectl describe sa consumer-sa -n cloud-events
```

### RBAC Issues

Verify RBAC configuration:
```bash
# Check role binding
kubectl get rolebinding cloud-event-consumer -n openshift-ptp -o yaml

# Test permissions
kubectl auth can-i get services -n openshift-ptp --as system:serviceaccount:cloud-events:consumer-sa
```

## Security Considerations

1. **Certificate Management**
   - Certificates must be manually managed and rotated
   - CA bundle should be kept up to date
   - Private keys never leave the pod
   - Consider using a certificate management solution in production

2. **Token Security**
   - OAuth tokens should be obtained from your OAuth provider
   - Tokens should be rotated regularly
   - Access is controlled via RBAC

3. **Network Security**
   - All traffic is encrypted with TLS
   - Authentication required for all requests
   - Network policies can be added for additional security

## Development Notes

When developing a custom consumer, implement authentication using the provided configuration:

```go
import (
    "crypto/tls"
    "crypto/x509"
    "io/ioutil"
    "net/http"
)

func createAuthenticatedClient(authConfig *AuthConfig) (*http.Client, error) {
    // Load CA certificate
    caCert, err := ioutil.ReadFile(authConfig.CACertPath)
    if err != nil {
        return nil, err
    }
    caCertPool := x509.NewCertPool()
    caCertPool.AppendCertsFromPEM(caCert)

    // Create TLS config
    tlsConfig := &tls.Config{
        RootCAs: caCertPool,
    }

    // Create HTTP client
    client := &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: tlsConfig,
        },
    }

    return client, nil
}

func makeAuthenticatedRequest(client *http.Client, url string) error {
    // Get OAuth token from your OAuth provider
    token := getOAuthToken() // Implement this function to get token from your OAuth provider

    // Create request
    req, err := http.NewRequest("POST", url, nil)
    if err != nil {
        return err
    }

    // Add OAuth token
    req.Header.Set("Authorization", "Bearer "+token)

    // Make request
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return nil
}
```
