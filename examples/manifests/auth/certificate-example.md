# Certificate Generation Example

This document provides examples of how to generate certificates manually for the generic cloud event consumer.

## Prerequisites

- OpenSSL installed
- kubectl configured to access your cluster

## Generate Certificates

### 1. Generate CA Certificate

```bash
# Create CA private key
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -new -x509 -key ca.key -sha256 -subj "/C=US/ST=CA/O=MyOrg/CN=MyCA" -days 3650 -out ca.crt
```

### 2. Generate Server Certificate

```bash
# Generate server private key
openssl genrsa -out server.key 4096

# Generate server certificate signing request
openssl req -new -key server.key -out server.csr -subj "/C=US/ST=CA/O=MyOrg/CN=cloud-event-proxy"

# Generate server certificate signed by CA. This creates server.crt and ca.srl
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256
```

### 3. Generate Client Certificate

```bash
# Generate client private key
openssl genrsa -out client.key 4096

# Generate client certificate signing request
openssl req -new -key client.key -out client.csr -subj "/C=US/ST=CA/O=MyOrg/CN=cloud-event-consumer"

# Generate client certificate signed by CA.  This creates client.crt and ca.srl
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365 -sha256
```

## Create Kubernetes Secrets

### 1. Create Server CA Bundle Secret

```bash
# Create server CA bundle secret
kubectl create secret generic server-ca-bundle \
  --from-file=ca.crt=ca.crt \
  --namespace=cloud-events
```

### 2. Create Client Certificate Secret

```bash
# Create client certificate secret
kubectl create secret tls consumer-client-certs \
  --cert=client.crt \
  --key=client.key \
  --namespace=cloud-events
```

## Verify Secrets

```bash
# Check secrets were created
kubectl get secrets -n cloud-events

# Verify secret contents
kubectl describe secret server-ca-bundle -n cloud-events
kubectl describe secret consumer-client-certs -n cloud-events
```

## Certificate Rotation

When certificates expire, you'll need to:

1. Generate new certificates using the same process
2. Update the Kubernetes secrets
3. Restart the consumer deployment to pick up new certificates

```bash
# Update secrets with new certificates
kubectl create secret generic server-ca-bundle \
  --from-file=ca.crt=new-ca.crt \
  --namespace=cloud-events \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret tls consumer-client-certs \
  --cert=new-client.crt \
  --key=new-client.key \
  --namespace=cloud-events \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart deployment to pick up new certificates
kubectl rollout restart deployment/cloud-consumer-deployment -n cloud-events
```

## Production Considerations

For production environments, consider:

1. **Certificate Management Solutions**:
   - cert-manager (works with any Kubernetes cluster)
   - HashiCorp Vault
   - External PKI solutions

2. **Automated Rotation**:
   - Implement automated certificate rotation
   - Monitor certificate expiration
   - Set up alerts for certificate renewal

3. **Security**:
   - Use strong key sizes (4096 bits)
   - Implement proper certificate validation
   - Store private keys securely
   - Use hardware security modules (HSMs) for key storage

4. **Monitoring**:
   - Monitor certificate expiration dates
   - Set up alerts for certificate renewal
   - Log certificate usage and validation failures
