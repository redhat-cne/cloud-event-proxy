#!/bin/bash
# entrypoint.sh: Start the cloud-event-proxy and restart on failure

set -e

while true; do
  ./cloud-event-proxy "$@"
  EXIT_CODE=$?
  echo "cloud-event-proxy exited with $EXIT_CODE. Restarting in 2 seconds..."
  sleep 2
done
