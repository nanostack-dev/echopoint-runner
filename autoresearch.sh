#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER_NAME="echopoint-runner-autoresearch-wiremock"
WIREMOCK_IMAGE="wiremock/wiremock:3.3.1"
WIREMOCK_MAPPINGS_DIR="$ROOT_DIR/it/wiremock/stubs"
BENCH_PACKAGE="./pkg/engine"
BENCH_NAME='^BenchmarkParallelHTTPFlow$'

ensure_wiremock() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "docker is required for autoresearch" >&2
    exit 1
  fi

  if ! docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    docker run \
      --detach \
      --rm \
      --name "$CONTAINER_NAME" \
      --publish 0:8080 \
      --volume "$WIREMOCK_MAPPINGS_DIR:/home/wiremock/mappings:ro" \
      "$WIREMOCK_IMAGE" \
      --global-response-templating \
      --disable-banner \
      >/dev/null
  else
    running="$(docker inspect -f '{{.State.Running}}' "$CONTAINER_NAME")"
    if [[ "$running" != "true" ]]; then
      docker start "$CONTAINER_NAME" >/dev/null
    fi
  fi

  port="$({ docker port "$CONTAINER_NAME" 8080/tcp || true; } | python3 -c 'import sys
line = sys.stdin.read().strip()
print(line.rsplit(":", 1)[-1] if line else "")')"
  if [[ -z "$port" ]]; then
    echo "failed to determine WireMock port" >&2
    exit 1
  fi

  base_url="http://127.0.0.1:${port}"
  python3 - "$base_url" <<'PY'
import json
import sys
import time
import urllib.request

base_url = sys.argv[1]
deadline = time.time() + 20
last_error = None
while time.time() < deadline:
    try:
        with urllib.request.urlopen(base_url + "/__admin/health", timeout=1) as response:
            payload = json.load(response)
        if payload.get("status") == "healthy":
            print(base_url)
            sys.exit(0)
    except Exception as exc:  # noqa: BLE001
        last_error = exc
    time.sleep(0.2)

print(f"WireMock health check failed: {last_error}", file=sys.stderr)
sys.exit(1)
PY
}

parse_metrics() {
  python3 - <<'PY'
import pathlib
import re
import sys

output = pathlib.Path("/tmp/autoresearch-bench.txt").read_text()
pattern = re.compile(
    r"BenchmarkParallelHTTPFlow(?:-\d+)?\s+\d+\s+([0-9.]+)\s+ns/op\s+([0-9.]+)\s+B/op\s+([0-9.]+)\s+allocs/op"
)
match = pattern.search(output)
if not match:
    print("failed to parse benchmark metrics", file=sys.stderr)
    sys.exit(1)

ns_per_op, bytes_per_op, allocs_per_op = match.groups()
print(f"METRIC parallel_flow_ns_per_op={ns_per_op}")
print(f"METRIC parallel_flow_b_per_op={bytes_per_op}")
print(f"METRIC parallel_flow_allocs_per_op={allocs_per_op}")
PY
}

go test "$BENCH_PACKAGE" -run '^$' -count=1 >/dev/null
base_url="$(ensure_wiremock)"

ECHOPOINT_BENCH_BASE_URL="$base_url" \
  go test "$BENCH_PACKAGE" -run '^$' -bench "$BENCH_NAME" -benchmem -count=1 \
  | tee /tmp/autoresearch-bench.txt

parse_metrics
