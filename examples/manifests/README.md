# Cloud Event Consumer Example

These manifests deploy an example cloud-event consumer in either plaintext or
secured mode. Secured mode combines mTLS with ServiceAccount bearer tokens
validated through the Kubernetes TokenReview API.

## Deploy

```bash
# Plaintext compatibility mode
make deploy-consumer

# Secured mode
make deploy-consumer SECURED=true

# Remove resources from either mode
make undeploy-consumer
```

Secured deployment performs these steps:

1. Applies the secured Kustomize overlay.
2. Waits for OpenShift Service CA serving certificates.
3. Creates dedicated consumer and publisher client-auth certificates.
4. Writes the client CA to the operator input ConfigMap
   `ptp-event-publisher-client-ca`.
5. Creates a static authentication ConfigMap using `requiredAudiences`.

There are no cluster-specific OAuth issuer or JWKS URLs. `CLUSTER_NAME` is not
used to generate authentication settings.

See [the authentication guide](auth/README.md) for configuration and
verification commands.

## Components

- `base/`: shared namespace, ServiceAccount, service, deployment, and RBAC.
- `secured/`: TLS/auth volumes, HTTPS publisher address, NetworkPolicy, and
  `--auth-config` arguments.
- `auth/setup-secrets.sh`: provisions authentication material.
- `auth/certificate-example.md`: certificate-generation background.

## Network Policy

The secured overlay applies default-deny ingress to the consumer callback API
and permits port 9043 from `openshift-ptp`, cluster monitoring, and the
OVN-Kubernetes join subnet. The PTP publisher uses `hostNetwork: true`; its push
traffic is commonly source-NATed to that join subnet rather than retaining a
namespace-selectable pod identity.

The example uses `100.64.0.0/16`. If the cluster overrides the internal join
subnet, update `network-policy.yaml` before deployment:

```bash
oc get network.operator cluster -o jsonpath='{.spec.defaultNetwork.ovnKubernetesConfig.ipv4.internalJoinSubnet}'
```

NetworkPolicy does not cover the host-network publisher listener. Apply the
appropriate host-level firewall policy for that endpoint.

## Authentication Settings

The generated consumer configuration is:

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

TokenReview delegates issuer, signature, expiry, and requested-audience checks
to the Kubernetes API server. The legacy JSON field name `enableOAuth` enables
this bearer-token mechanism; it does not make the application call the
OpenShift OAuth server.

## Certificate Ownership

| Resource | Owner | Purpose |
|---|---|---|
| `consumer-server-certs` Secret | OpenShift Service CA | Consumer callback server certificate |
| `consumer-client-certs` Secret | Setup flow | Consumer mTLS client certificate |
| `ptp-event-publisher-client-tls` Secret | Setup flow | Publisher push mTLS client certificate |
| `ptp-event-publisher-client-ca` ConfigMap | External/operator input | Client CA published for merge |
| `ptp-event-publisher-ca-bundle` ConfigMap | PTP operator | Merged live publisher trust bundle |

The setup flow never overwrites the live operator-owned bundle and never scales
the operator to zero.

## Verify

```bash
oc rollout status deployment/cloud-consumer-deployment -n cloud-events
oc get pods -n cloud-events
oc get configmap consumer-auth-config -n cloud-events \
  -o jsonpath='{.data.config\.json}' | jq .requiredAudiences
oc logs deployment/cloud-consumer-deployment -n cloud-events
```

Certificate files are loaded at process startup. Restart the consumer and the
publisher workload after certificate or trust-bundle rotation.

## Security Scope

mTLS and TokenReview provide transport protection and caller authentication.
They do not implement per-caller authorization for individual event API
operations. Add authorization policy, callback allow-listing, dedicated token
audiences, and automated credential reload as a separate hardening phase.
