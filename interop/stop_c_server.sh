#!/bin/bash
PID_FILE="/tmp/mms-interop-server.pid"

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        kill "$PID"
        echo "Server stopped (PID: $PID)"
    else
        echo "Server not running (stale PID file)"
    fi
    rm -f "$PID_FILE"
else
    echo "No PID file found"
fi
