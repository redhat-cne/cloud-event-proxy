#!/bin/bash

# setup-secrets.sh - provision the authentication material for the secured
# cloud-event-consumer (mTLS + OAuth on the O-RAN ocloudNotifications v2 API).
#
# It creates, in the consumer namespace (cloud-events):
#   - consumer-client-certs : the consumer's clientAuth keypair used for mTLS TO
#                             the publisher. Minted out-of-band because OpenShift
#                             Service CA serving certs are serverAuth-only and are
#                             NOT valid as client certs.
#   - server-ca-bundle      : the Service CA cert, so the consumer trusts the
#                             publisher's Service CA-signed server certificate.
#   - consumer-auth-config  : config.json consumed via --auth-config.
# The consumer's own callback server cert (consumer-server-certs) is minted by
# the Service CA via the serving-cert annotation the secured overlay adds to the
# consumer-events-subscription-service Service.
#
# It also publishes the client CA into the publisher namespace (openshift-ptp) as
# the ConfigMap ptp-event-publisher-client-ca, which the ptp-operator folds into
# the publisher's trust bundle (ca-bundle.crt) so the publisher trusts this
# consumer's client certificate. This replaces the old manual "overwrite the
# publisher CA bundle + scale the operator to 0" workaround.
#
# Client-cert provisioning method (CERT_MANAGER):
#   - default (CERT_MANAGER unset/false): openssl. Zero cluster dependencies;
#     a client CA + client cert are generated into ${DIR_CERTS} (default
#     /tmp/certs) and loaded into the consumer-client-certs Secret.
#   - CERT_MANAGER=true: cert-manager. A self-signed CA Issuer is bootstrapped
#     and auth/client-cert-service.yaml mints consumer-client-certs.

set -euo pipefail

NAMESPACE="${NAMESPACE:-cloud-events}"
PUBLISHER_NAMESPACE="${PUBLISHER_NAMESPACE:-openshift-ptp}"
CLIENT_CA_CONFIGMAP="${CLIENT_CA_CONFIGMAP:-ptp-event-publisher-client-ca}"
DIR_CERTS="${DIR_CERTS:-/tmp/certs}"
CERT_MANAGER="${CERT_MANAGER:-false}"
TIMEOUT="${TIMEOUT:-60}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Setting up authentication for cloud-event-consumer"
echo "  consumer namespace : $NAMESPACE"
echo "  publisher namespace: $PUBLISHER_NAMESPACE"
echo "  client-cert method : $([ "$CERT_MANAGER" = "true" ] && echo cert-manager || echo "openssl (DIR_CERTS=$DIR_CERTS)")"

if ! oc get project openshift-service-ca >/dev/null 2>&1 && ! oc get ns openshift-service-ca-operator >/dev/null 2>&1; then
    echo "Error: this script requires OpenShift with the Service CA operator"
    echo "For generic Kubernetes see auth/certificate-example.md"
    exit 1
fi

# wait_for <description> <command...> : polls until the command succeeds.
wait_for() {
    local desc="$1"; shift
    local count=0
    while [ "$count" -lt "$TIMEOUT" ]; do
        if "$@" >/dev/null 2>&1; then
            echo "✓ $desc"
            return 0
        fi
        echo "  waiting for $desc ... ($count/$TIMEOUT)"
        sleep 2
        count=$((count + 1))
    done
    echo "Error: timed out waiting for $desc"
    return 1
}

########################################
# 1. consumer client certificate (mTLS to the publisher)
########################################
CLIENT_CA_PEM=""
if [ "$CERT_MANAGER" = "true" ]; then
    echo "Bootstrapping a cert-manager CA issuer and client certificate..."
    cat <<EOF | oc apply -f -
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
  namespace: $NAMESPACE
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: cloud-events-ca
  namespace: $NAMESPACE
spec:
  isCA: true
  commonName: cloud-events-ca
  secretName: cloud-events-ca
  privateKey:
    algorithm: RSA
    size: 4096
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: cloud-events-ca-issuer
  namespace: $NAMESPACE
spec:
  ca:
    secretName: cloud-events-ca
EOF
    # auth/client-cert-service.yaml mints consumer-client-certs from the CA issuer.
    oc apply -f "$SCRIPT_DIR/client-cert-service.yaml"
    wait_for "consumer-client-certs (cert-manager)" oc get secret consumer-client-certs -n "$NAMESPACE"
    wait_for "cloud-events-ca secret (cert-manager)" oc get secret cloud-events-ca -n "$NAMESPACE"
    CLIENT_CA_PEM="$(oc get secret cloud-events-ca -n "$NAMESPACE" -o jsonpath='{.data.tls\.crt}' | base64 -d)"
else
    echo "Generating a client CA and client certificate with openssl into $DIR_CERTS..."
    mkdir -p "$DIR_CERTS"
    ext="$DIR_CERTS/client-ext.cnf"
    printf 'extendedKeyUsage=clientAuth\nkeyUsage=digitalSignature,keyEncipherment\n' > "$ext"
    # Client CA (only regenerated if missing, so re-runs keep the published CA stable).
    if [ ! -f "$DIR_CERTS/client-ca.crt" ] || [ ! -f "$DIR_CERTS/client-ca.key" ]; then
        openssl req -x509 -newkey rsa:4096 -nodes \
            -keyout "$DIR_CERTS/client-ca.key" -out "$DIR_CERTS/client-ca.crt" \
            -days 365 -subj "/CN=cloud-event-consumer-client-ca" >/dev/null 2>&1
    fi
    # Consumer client cert (clientAuth), signed by the client CA.
    openssl req -newkey rsa:4096 -nodes \
        -keyout "$DIR_CERTS/client.key" -out "$DIR_CERTS/client.csr" \
        -subj "/O=cloud-event-consumer/CN=cloud-event-consumer" >/dev/null 2>&1
    openssl x509 -req -in "$DIR_CERTS/client.csr" \
        -CA "$DIR_CERTS/client-ca.crt" -CAkey "$DIR_CERTS/client-ca.key" -CAcreateserial \
        -out "$DIR_CERTS/client.crt" -days 365 -extfile "$ext" >/dev/null 2>&1
    oc create secret tls consumer-client-certs \
        --cert="$DIR_CERTS/client.crt" --key="$DIR_CERTS/client.key" \
        --namespace="$NAMESPACE" --dry-run=client -o yaml | oc apply -f -
    CLIENT_CA_PEM="$(cat "$DIR_CERTS/client-ca.crt")"
    echo "✓ consumer-client-certs secret created (openssl)"
fi

########################################
# 2. publish the client CA to the publisher so it trusts this consumer
########################################
echo "Publishing client CA to $PUBLISHER_NAMESPACE/$CLIENT_CA_CONFIGMAP..."
if oc get namespace "$PUBLISHER_NAMESPACE" >/dev/null 2>&1; then
    oc create configmap "$CLIENT_CA_CONFIGMAP" \
        --from-literal=client-ca.crt="$CLIENT_CA_PEM" \
        --namespace="$PUBLISHER_NAMESPACE" --dry-run=client -o yaml | oc apply -f -
    echo "✓ $CLIENT_CA_CONFIGMAP published (ptp-operator folds it into the publisher trust bundle)"
else
    echo "Warning: namespace $PUBLISHER_NAMESPACE not found; skipping client-CA publish."
    echo "         The publisher will not trust this consumer's client cert until"
    echo "         $CLIENT_CA_CONFIGMAP exists in $PUBLISHER_NAMESPACE."
fi

########################################
# 3. server CA bundle (consumer trusts the publisher's server cert)
########################################
wait_for "Service CA injection into server-ca-bundle-configmap" \
    bash -c "oc get configmap server-ca-bundle-configmap -n $NAMESPACE -o jsonpath='{.data.service-ca\.crt}' 2>/dev/null | grep -q 'BEGIN CERTIFICATE'"
CA_CERT="$(oc get configmap server-ca-bundle-configmap -n "$NAMESPACE" -o jsonpath='{.data.service-ca\.crt}')"
oc create secret generic server-ca-bundle \
    --from-literal=service-ca.crt="$CA_CERT" \
    --namespace="$NAMESPACE" --dry-run=client -o yaml | oc apply -f -
echo "✓ server-ca-bundle secret created"

########################################
# 4. consumer callback server cert (Service CA serving cert)
########################################
wait_for "consumer-server-certs serving cert" oc get secret consumer-server-certs -n "$NAMESPACE"

########################################
# 5. consumer auth config
########################################
echo "Creating consumer-auth-config ConfigMap..."
oc delete configmap consumer-auth-config -n "$NAMESPACE" --ignore-not-found=true
cat <<EOF | oc apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: consumer-auth-config
  namespace: $NAMESPACE
data:
  config.json: |
    {
      "enableMTLS": true,
      "useServiceCA": true,
      "clientCertPath": "/etc/cloud-event-consumer/client-certs/tls.crt",
      "clientKeyPath": "/etc/cloud-event-consumer/client-certs/tls.key",
      "serverCertPath": "/etc/cloud-event-consumer/server-certs/tls.crt",
      "serverKeyPath": "/etc/cloud-event-consumer/server-certs/tls.key",
      "caCertPath": "/etc/cloud-event-consumer/ca-bundle/service-ca.crt",
      "enableOAuth": true,
      "useOpenShiftOAuth": true,
      "requiredAudiences": ["https://kubernetes.default.svc"],
      "serviceAccountName": "consumer-sa",
      "serviceAccountToken": "/var/run/secrets/kubernetes.io/serviceaccount/token"
    }
EOF
echo "✓ consumer-auth-config ConfigMap created"

echo ""
echo "Authentication setup completed successfully."
