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

### Automated Deployment (Recommended)

From the `cloud-event-proxy` repository root:

```bash
# Deploy consumer with default cluster name (openshift.local)
make deploy-consumer

# Deploy consumer with custom cluster name
export CLUSTER_NAME=your-cluster-name.com
make deploy-consumer
```

This will:
1. Deploy all Kubernetes resources
2. Set up mTLS certificates using OpenShift Service CA
3. Configure authentication secrets with correct OAuth URLs
4. Generate dynamic authentication configuration based on cluster name
5. Wait for the consumer pod to be ready

### Cluster Name Configuration

The deployment supports dynamic cluster configuration:

- **Default**: `openshift.local` (consistent with ptp-operator)
- **Custom**: Set `CLUSTER_NAME` environment variable before deployment
- **OAuth URLs**: Automatically generated as `https://oauth-openshift.apps.${CLUSTER_NAME}`
- **Security**: OAuth tokens are validated against the exact configured issuer with no bypass mechanisms

#### Updating Cluster Name for Consumer at Runtime

If the PTP operator's cluster name is updated, or you need to deploy the consumer to a different cluster, follow these detailed steps:

**Method 1: Automated Redeployment (Recommended)**
```bash
# Step 1: Set the new cluster name
export CLUSTER_NAME=your-actual-cluster.example.com

# Step 2: Redeploy consumer with new configuration
make undeploy-consumer
make deploy-consumer

# Step 3: Verify the consumer is running with correct configuration
oc get pods -n cloud-events
oc logs deployment/cloud-consumer-deployment -n cloud-events --tail=10
```

**Method 2: Manual ConfigMap Update**
```bash
# Step 1: Update the consumer-auth-config ConfigMap with new OAuth URLs
CLUSTER_NAME=your-actual-cluster.example.com
oc patch configmap consumer-auth-config -n cloud-events --type='json' -p="[
  {\"op\": \"replace\", \"path\": \"/data/config.json\", \"value\": \"{\\\"enableMTLS\\\": true, \\\"useServiceCA\\\": true, \\\"clientCertPath\\\": \\\"/etc/cloud-event-consumer/client-certs/tls.crt\\\", \\\"clientKeyPath\\\": \\\"/etc/cloud-event-consumer/client-certs/tls.key\\\", \\\"caCertPath\\\": \\\"/etc/cloud-event-consumer/ca-bundle/service-ca.crt\\\", \\\"enableOAuth\\\": true, \\\"useOpenShiftOAuth\\\": true, \\\"oauthIssuer\\\": \\\"https://oauth-openshift.apps.$CLUSTER_NAME\\\", \\\"oauthJWKSURL\\\": \\\"https://oauth-openshift.apps.$CLUSTER_NAME/oauth/jwks\\\", \\\"requiredScopes\\\": [\\\"user:info\\\"], \\\"requiredAudience\\\": \\\"openshift\\\", \\\"serviceAccountName\\\": \\\"consumer-sa\\\", \\\"serviceAccountToken\\\": \\\"/var/run/secrets/kubernetes.io/serviceaccount/token\\\"}\"}
]"

# Step 2: Restart consumer deployment to pick up changes
oc rollout restart deployment/cloud-consumer-deployment -n cloud-events

# Step 3: Wait for rollout to complete
oc rollout status deployment/cloud-consumer-deployment -n cloud-events
```

**Step-by-Step Verification Process:**

```bash
# 1. Check if consumer pod is running
oc get pods -n cloud-events -l app=cloud-consumer

# 2. Verify the updated OAuth configuration
oc get configmap consumer-auth-config -n cloud-events -o jsonpath='{.data.config\.json}' | jq '.oauthIssuer'

# 3. Check consumer logs for authentication success
oc logs deployment/cloud-consumer-deployment -n cloud-events --tail=20 | grep -i "auth\|oauth\|token"

# 4. Test OAuth server connectivity from consumer pod
oc exec deployment/cloud-consumer-deployment -n cloud-events -- curl -k "https://oauth-openshift.apps.$CLUSTER_NAME/oauth/jwks" | head -5

# 5. Verify consumer can connect to PTP publisher
oc logs deployment/cloud-consumer-deployment -n cloud-events --tail=50 | grep -i "subscription\|publisher\|connection"
```

**Complete Example: Updating from Default to Real Cluster**

```bash
# Current configuration uses default cluster name
echo "Current OAuth issuer:"
oc get configmap consumer-auth-config -n cloud-events -o jsonpath='{.data.config\.json}' | jq '.oauthIssuer'
# Output: "https://oauth-openshift.apps.openshift.local"

# Update to real cluster name
export CLUSTER_NAME=cnfdg4.sno.ptp.eng.rdu2.dc.redhat.com

# Method 1: Automated redeployment
make undeploy-consumer
make deploy-consumer

# Verify the update
echo "Updated OAuth issuer:"
oc get configmap consumer-auth-config -n cloud-events -o jsonpath='{.data.config\.json}' | jq '.oauthIssuer'
# Expected output: "https://oauth-openshift.apps.cnfdg4.sno.ptp.eng.rdu2.dc.redhat.com"

# Check consumer pod status
oc get pods -n cloud-events
oc logs deployment/cloud-consumer-deployment -n cloud-events --tail=10
```

**Troubleshooting Consumer OAuth Issues:**

```bash
# Check if OAuth server is accessible
CLUSTER_NAME=$(oc get configmap consumer-auth-config -n cloud-events -o jsonpath='{.data.config\.json}' | jq -r '.oauthIssuer' | sed 's|https://oauth-openshift.apps.||' | sed 's|/oauth/jwks||')
curl -k "https://oauth-openshift.apps.$CLUSTER_NAME/oauth/jwks"

# Verify consumer ServiceAccount token
oc get serviceaccount consumer-sa -n cloud-events
oc describe serviceaccount consumer-sa -n cloud-events

# Check consumer authentication configuration
oc get configmap consumer-auth-config -n cloud-events -o jsonpath='{.data.config\.json}' | jq .

# View consumer authentication logs
oc logs deployment/cloud-consumer-deployment -n cloud-events | grep -E "OAuth|authentication|token|issuer"
```

**Important Notes:**
- Always update the consumer configuration when the PTP operator's cluster name changes
- OAuth tokens must match the exact issuer URL - mismatches will cause authentication failures
- The consumer will automatically retry connections after configuration updates
- Changes take effect after the consumer pod restarts (usually within 30 seconds)

### Manual Deployment

If you prefer to deploy manually:

```bash
# Apply the manifests
oc apply -k .

# Set up authentication secrets (OpenShift only)
./auth/setup-secrets.sh
```

### Verify Deployment

```bash
kubectl get deployment cloud-consumer-deployment -n cloud-events
kubectl get pods -n cloud-events
kubectl logs -f deployment/cloud-consumer-deployment -n cloud-events
```

## Components

### Core Resources

- **`namespace.yaml`**: Creates the `cloud-events` namespace
- **`service.yaml`**: Creates a service for the consumer
- **`consumer.yaml`**: Main deployment for the cloud event consumer

### Authentication Resources

- **`setup-secrets.sh`**: Script that creates authentication configuration dynamically using CLUSTER_NAME
- **`auth/service-account.yaml`**: Service account and RBAC
- **`auth/certificate-example.md`**: Certificate generation guide

## Configuration

### Authentication Settings

The consumer supports both mTLS and OAuth authentication with **strict security validation**. The configuration is automatically generated during deployment based on the cluster name:

```json
{
  "enableMTLS": true,
  "useServiceCA": true,
  "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
  "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
  "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
  "enableOAuth": true,
  "useOpenShiftOAuth": true,
  "oauthIssuer": "https://oauth-openshift.apps.${CLUSTER_NAME}",
  "oauthJWKSURL": "https://oauth-openshift.apps.${CLUSTER_NAME}/oauth/jwks",
  "requiredScopes": ["user:info"],
  "requiredAudience": "openshift",
  "serviceAccountName": "consumer-sa",
  "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
}
```

#### Security Features

- **Strict OAuth Validation**: Token issuer must exactly match the configured OAuth issuer
- **No Authentication Bypass**: Mismatched issuers are immediately rejected
- **Comprehensive Validation**: Expiration, audience, and signature verification
- **Dynamic Configuration**: OAuth URLs automatically generated based on cluster name

### Required Secrets

The following secrets are **automatically generated** during deployment:

1. **`consumer-client-certs`**: TLS secret containing client certificate and key (generated by OpenShift Service CA)
2. **`server-ca-bundle`**: Generic secret containing the CA certificate (extracted from Service CA)

For manual setup or troubleshooting, see [Certificate Generation](auth/certificate-example.md) for detailed instructions.

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

OAuth provider settings are automatically configured based on your `CLUSTER_NAME` environment variable:

- `oauthIssuer` is set to `https://oauth-openshift.apps.${CLUSTER_NAME}`
- `oauthJWKSURL` is set to `https://oauth-openshift.apps.${CLUSTER_NAME}/oauth/jwks`
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
