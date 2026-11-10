# Authentication Configuration for Cloud Event Consumer

This example secures both directions of the event flow with TLS, mutual client
certificate authentication, and Kubernetes ServiceAccount bearer tokens.
Bearer tokens are validated in-process through the Kubernetes TokenReview API;
the OpenShift OAuth issuer and JWKS endpoints are not configured or contacted
directly.

## Authentication Configuration

`setup-secrets.sh` creates `consumer-auth-config` with this shape:

```json
{
  "enableMTLS": true,
  "useServiceCA": true,
  "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
  "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
  "serverCertPath": "/etc/cloud-event-consumer/server-certs/tls.crt",
  "serverKeyPath": "/etc/cloud-event-consumer/server-certs/tls.key",
  "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
  "enableOAuth": true,
  "requiredAudiences": ["https://kubernetes.default.svc"],
  "serviceAccountName": "consumer-sa",
  "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
}
```

The callback server and the publisher serving endpoint use OpenShift Service CA
certificates. Dedicated client-auth certificates are signed by a separate
client CA. The script publishes only that CA certificate to the operator input
ConfigMap `ptp-event-publisher-client-ca`; the operator merges it into its
managed publisher trust bundle. Do not overwrite the managed bundle or scale
the operator down.

## Deployment

From the repository root:

```bash
make deploy-consumer SECURED=true
oc rollout status deployment/cloud-consumer-deployment -n cloud-events
```

The default setup uses OpenSSL for client certificates. To use cert-manager:

```bash
make deploy-consumer SECURED=true CERT_MANAGER=true
```

The OpenSSL path stores its client CA key under `DIR_CERTS` (default
`/tmp/certs`) with mode `0700`. Use a protected persistent directory if the CA
must survive workstation cleanup:

```bash
make deploy-consumer SECURED=true DIR_CERTS=/secure/path/cloud-events-certs
```

## Validation

```bash
# Confirm the active settings and required token audience.
oc get configmap consumer-auth-config -n cloud-events \
  -o jsonpath='{.data.config\.json}' | jq .

# Confirm the consumer and publisher credentials exist without printing them.
oc get secret consumer-client-certs consumer-server-certs server-ca-bundle \
  -n cloud-events
oc get secret ptp-event-publisher-client-tls -n openshift-ptp
oc get configmap ptp-event-publisher-client-ca -n openshift-ptp

# Confirm the consumer can create TokenReviews.
oc auth can-i create tokenreviews.authentication.k8s.io \
  --as system:serviceaccount:cloud-events:consumer-sa

oc logs deployment/cloud-consumer-deployment -n cloud-events
```

## Troubleshooting

- A TLS hostname error usually means the client is using a node-specific
  service name that is not present in the serving certificate. Use the
  certificate-bearing service from the secured overlay.
- `Client certificate required` means the caller did not present a certificate
  signed by a CA in the server trust bundle.
- `Invalid OAuth token` means TokenReview rejected the token or its audience.
- Certificate and trust files are loaded at process startup. Restart the
  affected workload after rotation.

## Security Boundaries

- mTLS authenticates certificate holders and TokenReview authenticates the
  bearer-token identity. Per-caller endpoint authorization is a separate policy
  layer and is not implemented by this example.
- Keep client CA private keys outside the cluster unless a certificate manager
  is intentionally responsible for them.
- Use short-lived projected ServiceAccount tokens and a dedicated audience when
  moving beyond this compatibility-oriented example.
- Constrain callback destinations and network reachability for production.

## Cleanup

```bash
make undeploy-consumer
```

Cleanup removes both plaintext and secured overlays plus the publisher-side
client certificate and client-CA input created by the setup script.
