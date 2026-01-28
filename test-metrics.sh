#!/bin/bash

# Get the game code from the server
GAME_CODE=$(docker-compose logs bingo-server 2>&1 | grep "with code:" | tail -1 | awk -F'code: ' '{print $2}')

if [ -z "$GAME_CODE" ]; then
    echo "Error: Could not find game code in server logs"
    exit 1
fi

echo "Found game code: $GAME_CODE"
echo ""
echo "Connecting 3 clients to test metrics..."
echo ""

# Start 3 clients in background
for i in {1..3}; do
    echo "Starting client $i..."
    ./binGO -mode client -server localhost:8080 -code "$GAME_CODE" << EOF
n

q
EOF &
    sleep 2
done

echo ""
echo "All clients connected. Metrics should now show:"
echo "- Active Games: 1"
echo "- Connected Players: 3"
echo ""
echo "View in Grafana: http://localhost:3000/d/bingo-phase8"
echo ""
echo "Press Ctrl+C to stop"
wait
