#!/bin/bash

set -e

deploy_producer() {
  action=$1
  cne_namespace=$2
  amq_namespace=$3
  deploy_cne $action $cne_namespace $amq_namespace || true
  producer_service_account $action $cne_namespace || true
  producer_role $action $cne_namespace || true
}
deploy_cne() {
  action=$1
  cne_namespace=$2
  amq_namespace=$3
  cat <<EOF | oc $action -n $cne_namespace  -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloud-native-producer-deployment
  labels:
    app: producer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: producer
  template:
    metadata:
      labels:
        app: producer
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - local
      serviceAccountName: cloud-event-producer-sa
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: cloud-event-proxy
          image: "$CNE_IMG"
          args:
            - "--metrics-addr=127.0.0.1:9091"
            - "--store-path=/store"
            - "--transport-host=amqp://amq-router.$amq_namespace.svc.cluster.local"
            - "--api-port=9095"
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: MOCK_PLUGIN
              value: "true"
          volumeMounts:
            - name: pubsubstore
              mountPath: /store
          ports:
            - name: metrics-port
              containerPort: 9091
          resources:
            requests:
              cpu: 10m
              memory: 20Mi
      volumes:
        - name: pubsubstore
          emptyDir: {}
EOF
}

producer_service_account() {
  action=$1
  producer_namespace=$2

  cat <<EOF | oc $action -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cloud-event-producer-sa
  namespace: $producer_namespace
EOF
}

producer_role() {
  action=$1
  producer_namespace=$2

  cat <<EOF | oc $action -n $producer_namespace -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cloud-event-producer-role
rules:
  - apiGroups: ["authentication.k8s.io"]
    resources: ["tokenreviews"]
    verbs: ["create"]
  - apiGroups: ["authorization.k8s.io"]
    resources: ["subjectaccessreviews"]
    verbs: ["create"]
EOF

  cat <<EOF | oc $action -n $producer_namespace -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cloud-event-producer-role-binding
roleRef:
  kind: ClusterRole
  name: cloud-event-producer-role
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: cloud-event-producer-sa
    namespace: $producer_namespace
EOF

}

