#!/bin/bash

# ==============================================================================
# Cloudflare Tunnel Automated Setup Script for avandab.com
# ==============================================================================

set -e

TUNNEL_NAME="avandab-tunnel"
TUNNEL_ID="16b2fee0-e242-42de-967a-1ed401a5bebc"
CLOUDFLARED_BIN="/home/abhishek/.local/bin/cloudflared"

echo "=============================================================================="
echo "🚀 Setting up Cloudflare Tunnel for avandab.com..."
echo "=============================================================================="

# 1. Ensure ~/.cloudflared directory exists
mkdir -p ~/.cloudflared

# 2. Write Cloudflare Tunnel Configuration
echo "1. Creating ~/.cloudflared/config.yml..."
cat << EOF > ~/.cloudflared/config.yml
tunnel: ${TUNNEL_ID}
credentials-file: /home/abhishek/.cloudflared/${TUNNEL_ID}.json

ingress:
  # MQTT over WebSocket -> mosquitto WS listener. Must stay first: cloudflared
  # matches rules in order and the path-only rule catches /mqtt on any host.
  # The mobile app connects to wss://<host>/mqtt (mobile/src/services/mqtt.ts).
  - path: ^/mqtt$
    service: http://localhost:9001
  - hostname: avandab.com
    service: http://localhost:8080
  - hostname: www.avandab.com
    service: http://localhost:8080
  - service: http_status:404
EOF

echo "✅ Configuration file created at ~/.cloudflared/config.yml"

# 3. Attempt DNS routing (with overwrite force attempt)
echo ""
echo "2. Routing DNS records for avandab.com and www.avandab.com..."
$CLOUDFLARED_BIN tunnel route dns --overwrite-dns ${TUNNEL_NAME} avandab.com || echo "⚠️  Note: If DNS record exists, ensure old A/CNAME record in Cloudflare Dashboard is deleted or pointed to ${TUNNEL_ID}.cfargotunnel.com"
$CLOUDFLARED_BIN tunnel route dns --overwrite-dns ${TUNNEL_NAME} www.avandab.com || echo "⚠️  Note: If DNS record exists, ensure old A/CNAME record in Cloudflare Dashboard is deleted or pointed to ${TUNNEL_ID}.cfargotunnel.com"

echo ""
echo "=============================================================================="
echo "🎉 Cloudflare Tunnel Setup Completed Successfully!"
echo "=============================================================================="
echo "To start your site live, run:"
echo "   $CLOUDFLARED_BIN tunnel run ${TUNNEL_NAME}"
echo "=============================================================================="
