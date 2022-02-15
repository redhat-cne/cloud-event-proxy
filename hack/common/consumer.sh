#!/bin/bash

set -e

deploy_consumer() {
  action=$1
  consumer_namespace=$2
  amq_namespace=$3
  consumer_service_account $action $consumer_namespace || true
  consumer_role $action $consumer_namespace || true
  deploy_event_consumer $action $consumer_namespace $amq_namespace || true

}

deploy_event_consumer() {
  action=$1
  consumer_namespace=$2
  amq_namespace=$3
  cat <<EOF | oc $action -n $consumer_namespace -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloud-consumer-deployment
  labels:
    app: consumer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: consumer
  template:
    metadata:
      labels:
        app: consumer
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
      serviceAccountName: cloud-event-consumer-sa
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: cloud-event-consumer
          image: "$CONSUMER_IMG"
          args:
            - "--local-api-addr=127.0.0.1:9089"
            - "--api-path=/api/cloudNotifications/v1/"
            - "--api-addr=127.0.0.1:9095"
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: CONSUMER_TYPE
              value: "$CONSUMER_TYPE"
        - name: cloud-event-proxy
          image: "$IMG"
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

consumer_service_account() {
  action=$1
  consumer_namespace=$2

  cat <<EOF | oc $action -n $consumer_namespace -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cloud-event-consumer-sa
  namespace: $consumer_namespace
EOF
}


consumer_role() {
  action=$1
  consumer_namespace=$2

  cat <<EOF | oc $action -n $consumer_namespace -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cloud-event-consumer-role
rules:
  - apiGroups: ["authentication.k8s.io"]
    resources: ["tokenreviews"]
    verbs: ["create"]
  - apiGroups: ["authorization.k8s.io"]
    resources: ["subjectaccessreviews"]
    verbs: ["create"]
EOF

  cat <<EOF | oc $action -n $consumer_namespace -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cloud-event-consumer-role-binding
roleRef:
  kind: ClusterRole
  name: cloud-event-consumer-role
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: cloud-event-consumer-sa
    namespace: $consumer_namespace
EOF

}
