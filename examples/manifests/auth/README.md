# Authentication Configuration for Cloud Event Consumer

This guide explains how to use mTLS and OAuth authentication in the example cloud event consumer deployment using OpenShift's built-in components. This unified approach works seamlessly for both single node and multi-node OpenShift clusters.

## Overview

The example consumer is configured to authenticate with the cloud-event-proxy server using:
- mTLS (Mutual TLS) using OpenShift Service CA for transport security
- OAuth with JWT tokens using OpenShift's built-in OAuth server for client authentication

## Components

### Authentication Configuration

The authentication configuration uses OpenShift's built-in components:

The authentication settings are stored in a ConfigMap (`consumer-auth-config`):
```yaml
data:
  config.json: |
    {
      "enableMTLS": true,
      "useServiceCA": true,
      "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
      "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
      "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
      "enableOAuth": true,
      "useOpenShiftOAuth": true,
      "oauthIssuer": "https://oauth-openshift.apps.your-cluster.com",
      "oauthJWKSURL": "https://oauth-openshift.apps.your-cluster.com/.well-known/jwks.json",
      "requiredScopes": ["user:info"],
      "requiredAudience": "openshift",
      "serviceAccountName": "consumer-sa",
      "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
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

Authentication settings are automatically configured by the `setup-secrets.sh` script based on your `CLUSTER_NAME` environment variable:
- mTLS is enabled by default using OpenShift Service CA
- OAuth is enabled by default using OpenShift OAuth server
- OAuth issuer URLs are automatically set based on `CLUSTER_NAME`
- Required scopes and audience are set for OpenShift integration

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
    "net/http"
    "os"
)

func createAuthenticatedClient(authConfig *AuthConfig) (*http.Client, error) {
    // Load CA certificate
    caCert, err := os.ReadFile(authConfig.CACertPath)
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
