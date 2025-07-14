#!/bin/sh
# healthcheck.sh: exit 1 if process missing OR metrics unhealthy

set -eu

# Check if any cloud-event-proxy process exists
if ! pgrep -f "cloud-event-proxy" >/dev/null; then
  echo "Healthcheck failed: cloud-event-proxy process not found"
  exit 1
fi

# Check metrics endpoint
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:9091/metrics)
if [ "$STATUS" -ne 200 ]; then
  echo "Healthcheck failed: /metrics returned $STATUS"
  exit 1
fi

exit 0
