# Cloud Event Consumer Example

This directory contains example Kubernetes manifests for deploying a cloud event consumer with mTLS and OAuth authentication using OpenShift's built-in components.

## Overview

The example consumer is designed to work with **OpenShift clusters of any size** (single node or multi-node). It demonstrates how to:

- Deploy a cloud event consumer
- Configure mTLS authentication using OpenShift Service CA
- Set up OAuth authentication using OpenShift's built-in OAuth server
- Use OpenShift's native authentication components

## Prerequisites

- OpenShift cluster (single node or multi-node)
- oc or kubectl configured to access your cluster
- No additional operators required (uses OpenShift's built-in components)
- OpenShift Service CA will automatically generate certificates

## Quick Start

1. **Deploy the Consumer**:
   ```bash
   # Apply the manifests
   oc apply -k .
   ```

2. **Verify Deployment**:
   ```bash
   kubectl get deployment cloud-consumer-deployment -n cloud-events
   kubectl get pods -n cloud-events
   ```

## Components

### Core Resources

- **`namespace.yaml`**: Creates the `cloud-events` namespace
- **`service.yaml`**: Creates a service for the consumer
- **`consumer.yaml`**: Main deployment for the cloud event consumer

### Authentication Resources

- **`auth/configmap.yaml`**: Authentication configuration
- **`auth/service-account.yaml`**: Service account and RBAC
- **`auth/certificate-example.md`**: Certificate generation guide

## Configuration

### Authentication Settings

The consumer supports both mTLS and OAuth authentication. Configure these in `auth/configmap.yaml`:

```json
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

### Required Secrets

You need to create these secrets manually:

1. **`consumer-client-certs`**: TLS secret containing client certificate and key
2. **`server-ca-bundle`**: Generic secret containing the CA certificate

See [Certificate Generation](auth/certificate-example.md) for detailed instructions.

## Customization

### Namespace

Change the namespace by updating the `namespace.yaml` file and all references to `cloud-events` in other files.

### Service Discovery

The service discovery configuration in `consumer.yaml` points to the server in the `openshift-ptp` namespace:

```yaml
args:
  - "--http-event-publishers=ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043"
```

This assumes the cloud-event-proxy server is running in the `openshift-ptp` namespace. If your server is in a different namespace, update this configuration accordingly.

### OAuth Provider

Configure your OAuth provider settings in `auth/configmap.yaml`:

- Update `oauthIssuer` to your OAuth server URL
- Update `oauthJWKSURL` to your JWKS endpoint
- Adjust `requiredScopes` and `requiredAudience` as needed

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n cloud-events
kubectl describe pod -l app=consumer -n cloud-events
```

### View Logs

```bash
kubectl logs deployment/cloud-consumer-deployment -n cloud-events
```

### Verify Secrets

```bash
kubectl get secrets -n cloud-events
kubectl describe secret consumer-client-certs -n cloud-events
kubectl describe secret server-ca-bundle -n cloud-events
```

### Test Authentication

```bash
# Test RBAC permissions
kubectl auth can-i get services -n openshift-ptp --as system:serviceaccount:cloud-events:consumer-sa
```

## Security Considerations

1. **Certificate Management**: Certificates must be manually managed and rotated
2. **OAuth Tokens**: Obtain tokens from your OAuth provider and rotate regularly
3. **RBAC**: Review and adjust RBAC permissions based on your security requirements
4. **Network Policies**: Consider implementing network policies for additional security

## Production Deployment

For production environments:

1. Use a proper certificate management solution (e.g., cert-manager)
2. Implement automated certificate rotation
3. Use a production-grade OAuth provider
4. Set up monitoring and alerting
5. Implement proper logging and audit trails
6. Review and harden RBAC permissions

## Support

For issues and questions:

1. Check the [Authentication Guide](auth/README.md) for detailed authentication setup
2. Review the [Certificate Generation Guide](auth/certificate-example.md) for certificate management
3. Check the main project documentation
4. Open an issue in the project repository
