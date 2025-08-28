# Authentication Configuration for Cloud Event Consumer

This guide explains how to use mTLS and OAuth authentication in the example cloud event consumer deployment.

## Overview

The example consumer is configured to authenticate with the cloud-event-proxy server using:
- mTLS (Mutual TLS) for transport security
- OAuth with Service Account tokens for client authentication

## Components

### Authentication Configuration

The authentication settings are stored in a ConfigMap (`consumer-auth-config`):
```yaml
data:
  config.json: |
    {
      "enableMTLS": true,
      "caCertPath": "/etc/cloud-event-consumer/service-ca/ca.crt",
      "enableOAuth": true,
      "oauthIssuer": "https://kubernetes.default.svc",
      "oauthJWKSURL": "https://kubernetes.default.svc/.well-known/openid-configuration",
      "requiredScopes": ["cloud-event-proxy"],
      "requiredAudience": "cloud-event-proxy"
    }
```

### Service Account

The consumer uses a dedicated service account (`consumer-sa`) with:
- RBAC permissions to access the cloud-event-proxy service
- Automatically managed OAuth tokens
- Token mounted in the pod for authentication

### Certificate Management

The consumer accesses certificates through:
- Service CA bundle mounted from the cloud-event-proxy service
- Automatic certificate rotation handled by OpenShift
- Secure volume mounts in the pod

## Deployment

The authentication components are automatically deployed when you apply the example manifests:

```bash
# Create namespace and resources
oc apply -k examples/manifests/

# Verify the deployment
oc get deployment cloud-consumer-deployment -n cloud-events
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
# View CA bundle
oc get configmap ptp-event-publisher-ca-bundle -n openshift-ptp -o yaml

# Check certificate mounting in pod
oc describe pod -l app=consumer -n cloud-events
```

### Authentication Errors

Check the consumer logs:
```bash
# View pod logs
oc logs deployment/cloud-consumer-deployment -n cloud-events

# Check service account token
oc describe sa consumer-sa -n cloud-events
```

### RBAC Issues

Verify RBAC configuration:
```bash
# Check role binding
oc get rolebinding cloud-event-consumer -n openshift-ptp -o yaml

# Test permissions
oc auth can-i get services -n openshift-ptp --as system:serviceaccount:cloud-events:consumer-sa
```

## Security Considerations

1. **Certificate Management**
   - Certificates are automatically rotated by OpenShift
   - CA bundle is kept up to date
   - Private keys never leave the pod

2. **Token Security**
   - Service account tokens are automatically rotated
   - Tokens are mounted securely in the pod
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
    // Get service account token
    token := os.Getenv("SERVICE_ACCOUNT_TOKEN")

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
