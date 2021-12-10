#!/bin/bash
set -e


deploy_amq() {
  action=$1
  amq_namespace=$2
deploy_config $action $amq_namespace || true
deploy_router $action $amq_namespace || true
deploy_service $action $amq_namespace || true
}


deploy_router() {
  action=$1
  amq_namespace=$2

  cat <<EOF | oc $action -n $amq_namespace -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: amq-router
  name: interconnect-deployment
  namespace: $amq_namespace
spec:
  replicas: 1
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: amq-router
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: amq-router
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
      containers:
        - env:
            - name: QDROUTERD_CONF
              value: /opt/router/qdrouterd.conf
          image: quay.io/interconnectedcloud/qdrouterd:latest
          imagePullPolicy: Always
          name: amq-router
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
          volumeMounts:
            - mountPath: /opt/router
              name: router-config
              readOnly: true
      restartPolicy: Always
      terminationGracePeriodSeconds: 60
      volumes:
        - configMap:
            defaultMode: 420
            name: router-config
          name: router-config
EOF
}

deploy_service(){
  action=$1
  amq_namespace=$2

  cat <<EOF | oc $action -n $amq_namespace  -f -
apiVersion: v1
kind: Service
metadata:
  labels:
    app: amq-router
  name: amq-router
  namespace: $amq_namespace
spec:
  ports:
    - name: "5672"
      port: 5672
      protocol: TCP
      targetPort: 5672
  selector:
    app: amq-router
  type: ClusterIP
EOF
}

deploy_config(){
  action=$1
  amq_namespace=$2

  cat <<EOF | oc $action -n $amq_namespace  -f -
apiVersion: v1
kind: ConfigMap
metadata:
  labels:
    app: router
  name: router-config
  namespace: $amq_namespace
data:
  qdrouterd.conf: |2+
    router {
        mode: standalone
        id: router
    }
    listener {
        host: 0.0.0.0
        port: 5672
        role: normal
    }
    address {
        prefix: closest
        distribution: closest
    }
    address {
        prefix: multicast
        distribution: multicast
    }
    address {
        prefix: unicast
        distribution: closest
    }
    address {
        prefix: exclusive
        distribution: closest
    }
    address {
        prefix: broadcast
        distribution: multicast
    }
    #address {
    #    prefix: cluster/node
    #    distribution: multicast
    #}

EOF
}