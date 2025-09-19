# Cloud Event Consumer with Authentication

This directory contains examples of how to use the cloud-event-proxy consumer with authentication features including mTLS and OAuth.

## Files

- `main.go` - Main consumer application with authentication support
- `authenticated-consumer-example.go` - Comprehensive examples of authentication usage
- `auth-config-example.json` - Example authentication configuration file
- `README.md` - This documentation file

## Authentication Features

The consumer supports two types of authentication:

### 1. mTLS (Mutual TLS)
- Client certificate authentication
- CA certificate validation
- Transport layer security
- Works with OpenShift Service CA or cert-manager

### 2. OAuth
- JWT token-based authentication
- OpenShift OAuth server integration
- ServiceAccount token support
- Bearer token authentication

## Configuration

Authentication is configured using a JSON configuration file. The configuration supports:

```json
{
  "enableMTLS": true,
  "useServiceCA": true,
  "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
  "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
  "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
  "enableOAuth": true,
  "useOpenShiftOAuth": true,
  "oauthIssuer": "https://oauth-openshift.apps.your-cluster.com",
  "oauthJWKSURL": "https://oauth-openshift.apps.your-cluster.com/oauth/jwks",
  "requiredScopes": ["user:info"],
  "requiredAudience": "openshift",
  "serviceAccountName": "consumer-sa",
  "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
}
```

### Configuration Options

#### mTLS Configuration
- `enableMTLS`: Enable/disable mTLS authentication
- `useServiceCA`: Use OpenShift Service CA (recommended)
- `clientCertPath`: Path to client certificate file
- `clientKeyPath`: Path to client private key file
- `caCertPath`: Path to CA certificate file
- `certManagerIssuer`: cert-manager ClusterIssuer name (alternative to Service CA)
- `certManagerNamespace`: Namespace for cert-manager resources

#### OAuth Configuration
- `enableOAuth`: Enable/disable OAuth authentication
- `useOpenShiftOAuth`: Use OpenShift's built-in OAuth server (recommended)
- `oauthIssuer`: OAuth server issuer URL
- `oauthJWKSURL`: OAuth JWKS endpoint URL
- `requiredScopes`: Required OAuth scopes
- `requiredAudience`: Required OAuth audience
- `serviceAccountName`: ServiceAccount name for authentication
- `serviceAccountToken`: Path to ServiceAccount token file
- `authenticationOperator`: Use OpenShift Authentication Operator (alternative)

## Usage

### Basic Usage

```bash
# Run without authentication
./consumer --local-api-addr=localhost:8989 --http-event-publishers=localhost:9043

# Run with authentication
./consumer --local-api-addr=localhost:8989 --http-event-publishers=localhost:9043 --auth-config=/path/to/auth-config.json
```

### Command Line Options

- `--local-api-addr`: Local API address (default: localhost:8989)
- `--api-path`: REST API path (default: /api/ocloudNotifications/v2/)
- `--http-event-publishers`: Comma-separated list of publisher addresses
- `--auth-config`: Path to authentication configuration file (JSON format)

### Environment Variables

- `NODE_NAME`: Node name for resource addressing
- `NODE_IP`: Node IP address
- `CONSUMER_TYPE`: Consumer type (PTP, HW, MOCK)
- `ENABLE_STATUS_CHECK`: Enable periodic status checks

## Examples

### Example 1: mTLS Only

```json
{
  "enableMTLS": true,
  "useServiceCA": true,
  "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
  "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
  "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
  "enableOAuth": false
}
```

### Example 2: OAuth Only

```json
{
  "enableMTLS": false,
  "enableOAuth": true,
  "useOpenShiftOAuth": true,
  "oauthIssuer": "https://oauth-openshift.apps.your-cluster.com",
  "oauthJWKSURL": "https://oauth-openshift.apps.your-cluster.com/oauth/jwks",
  "requiredScopes": ["user:info"],
  "requiredAudience": "openshift",
  "serviceAccountName": "consumer-sa",
  "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
}
```

### Example 3: Combined mTLS + OAuth

```json
{
  "enableMTLS": true,
  "useServiceCA": true,
  "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
  "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
  "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
  "enableOAuth": true,
  "useOpenShiftOAuth": true,
  "oauthIssuer": "https://oauth-openshift.apps.your-cluster.com",
  "oauthJWKSURL": "https://oauth-openshift.apps.your-cluster.com/oauth/jwks",
  "requiredScopes": ["user:info"],
  "requiredAudience": "openshift",
  "serviceAccountName": "consumer-sa",
  "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
}
```

## Development

### Building the Consumer

```bash
# Build the consumer
go build -o consumer main.go

# Build with authentication examples
go build -o authenticated-consumer-example authenticated-consumer-example.go
```

### Running Examples

```bash
# Run the main consumer
./consumer --auth-config=auth-config-example.json

# Run the authentication examples
./authenticated-consumer-example
```

## Integration with Kubernetes/OpenShift

### Deployment

The consumer can be deployed in Kubernetes/OpenShift with authentication using the manifests in the `manifests/` directory:

```bash
# Deploy with authentication
kubectl apply -k manifests/

# Verify deployment
kubectl get pods -n cloud-events
kubectl logs deployment/cloud-consumer-deployment -n cloud-events
```

### Certificate Management

For mTLS authentication, certificates can be managed using:

1. **OpenShift Service CA** (recommended)
   - Automatic certificate generation
   - Automatic rotation
   - Built-in CA management

2. **cert-manager** (alternative)
   - External certificate management
   - Custom certificate policies
   - Integration with external CAs

3. **Manual certificates** (development only)
   - Self-signed certificates
   - Manual rotation
   - Not recommended for production

### ServiceAccount Configuration

For OAuth authentication, configure the ServiceAccount:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: consumer-sa
  namespace: cloud-events
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: cloud-event-consumer
  namespace: openshift-ptp
rules:
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: cloud-event-consumer
  namespace: openshift-ptp
subjects:
- kind: ServiceAccount
  name: consumer-sa
  namespace: cloud-events
roleRef:
  kind: Role
  name: cloud-event-consumer
  apiGroup: rbac.authorization.k8s.io
```

## Troubleshooting

### Common Issues

1. **Certificate not found**
   ```
   Error: client certificate file not found: /path/to/cert
   ```
   - Verify certificate paths in configuration
   - Check certificate files exist and are readable
   - Ensure proper file permissions

2. **OAuth token not found**
   ```
   Error: service account token file not found: /path/to/token
   ```
   - Verify ServiceAccount token path
   - Check ServiceAccount exists and has proper permissions
   - Ensure token file is mounted correctly

3. **Authentication failed**
   ```
   Error: 401 Unauthorized
   ```
   - Verify OAuth token is valid
   - Check OAuth server configuration
   - Ensure proper scopes and audience

### Debugging

Enable debug logging to troubleshoot authentication issues:

```bash
# Set log level to debug
export LOG_LEVEL=debug

# Run consumer with debug logging
./consumer --auth-config=auth-config.json
```

### Health Checks

The consumer includes health check endpoints:

- `GET /health` - Basic health check
- `GET /ready` - Readiness check
- `GET /metrics` - Prometheus metrics

## Security Considerations

1. **Certificate Security**
   - Store certificates in Kubernetes secrets
   - Use proper file permissions (600 for private keys)
   - Rotate certificates regularly
   - Monitor certificate expiration

2. **Token Security**
   - Use short-lived tokens when possible
   - Rotate ServiceAccount tokens regularly
   - Monitor token usage and access patterns
   - Implement proper RBAC policies

3. **Network Security**
   - Use TLS for all communications
   - Implement network policies
   - Monitor network traffic
   - Use service mesh for additional security

## Contributing

When contributing to the authentication features:

1. Follow the existing code patterns
2. Add comprehensive tests
3. Update documentation
4. Consider backward compatibility
5. Test with both mTLS and OAuth configurations

## License

This code is licensed under the Apache License 2.0. See the LICENSE file for details.
