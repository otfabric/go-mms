#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="/tmp/mms-interop-server.pid"

# Allow overriding the server binary path
SERVER_BIN="${INTEROP_SERVER_BIN:-/tmp/libIEC61850/build/examples/server_example_basic_io/server_example_basic_io}"

if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
        echo "Server already running (PID: $OLD_PID)"
        exit 0
    fi
    rm -f "$PID_FILE"
fi

if [ ! -x "$SERVER_BIN" ]; then
    echo "C server binary not found at $SERVER_BIN"
    echo ""
    echo "Build it first:"
    echo "  git clone https://github.com/mz-automation/libIEC61850.git /tmp/libIEC61850"
    echo "  cd /tmp/libIEC61850 && mkdir -p build && cd build && cmake .. && make"
    echo ""
    echo "Or set INTEROP_SERVER_BIN to point to your server binary."
    exit 1
fi

echo "Starting C MMS server on port 102..."
"$SERVER_BIN" &
echo $! > "$PID_FILE"
sleep 2

if kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "Server started (PID: $(cat "$PID_FILE"))"
else
    echo "Server failed to start"
    rm -f "$PID_FILE"
    exit 1
fi
