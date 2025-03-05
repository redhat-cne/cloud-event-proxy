#!/bin/bash

set -e

deploy_consumer() {
  action=$1
  consumer_namespace=$2
  transport_host=$3
  http_event_publishers=$4
  consumer_service_account $action $consumer_namespace || true
  consumer_role $action $consumer_namespace || true
  deploy_event_consumer $action $consumer_namespace $transport_host $http_event_publishers || true
  consumer_http_service $action $consumer_namespace || true

}

deploy_event_consumer() {
  action=$1
  consumer_namespace=$2
  transport_host=$3
  http_event_publishers=$4
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
          imagePullPolicy: Always
          args:
            - "--local-api-addr=consumer-events-subscription-service.$consumer_namespace.svc.cluster.local:9043"
            - "--api-path=/api/ocloudNotifications/v2/"
            - "--api-addr=127.0.0.1:9095"
            - "--api-version=2.0"
            - "--http-event-publishers=$http_event_publishers"
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: CONSUMER_TYPE
              value: "$CONSUMER_TYPE"
            - name: ENABLE_STATUS_CHECK
              value: "true"
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

consumer_http_service(){
  action=$1
  consumer_namespace=$2
  cat <<EOF | oc $action -n $consumer_namespace -f -
  apiVersion: v1
  kind: Service
  metadata:
    name: consumer-events-subscription-service
    namespace: $consumer_namespace
    labels:
      app: consumer-service
  spec:
    ports:
      - name: sub-port
        port: 9043
    selector:
      app: consumer
    clusterIP: None
    sessionAffinity: None
    type: ClusterIP
EOF
}
