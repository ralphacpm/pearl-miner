#!/bin/bash

MARKETS=(
  "97G9NnvBDQ2WpKu6fasoMsAKmfj63C9rhysJnkeWodAf:RTX 4090"
  "5eX3kWkrcbwejEc1svbfP4F7NKYjtPDuyU5KnV1hUBKg:RTX 5070"
  "9HnJacS25TnErsKMYJmKqWeCAMYuwY7gzhz9Eqhp5VE7:RTX 5080"
  "6Xt8hgVLLL2PSHC9NtJP8E8oTdA5ZJc95hZEnHcdqKqb:RTX 5090"
  "8pr3btRVcbqqGxaZspd18QWm41eByTjZR1cB58nVWiNg:RTX 5090 Community"
  "Dcwz62TisNbWuto6KJM2EGYGVKnHbdZGVGmgLASzsXy8:RTX 4090 Community"
  "9fgU7Btd5gXB3xzAFmT322KdkdUuMjX7GG1LeNT5qFj4:RTX 5080 Community"
  "CA5pMpqkYFKtme7K31pNB1s62X2SdhEv1nN9RdxKCpuQ:RTX 3090"
)

RPC="https://mainnet.helius-rpc.com/?api-key=59a1f481-bc75-426e-883f-1d5b628339d3"

check_markets() {
  echo "$(date '+%H:%M:%S') --- Nosana GPU Availability ---"
  for entry in "${MARKETS[@]}"; do
    address="${entry%%:*}"
    name="${entry##*:}"
    python3 << PYEOF 2>/dev/null
import urllib.request, json, base64

address = "$address"
name = "$name"

req = urllib.request.Request("$RPC",
    data=json.dumps({"jsonrpc":"2.0","id":1,"method":"getAccountInfo",
        "params":[address,{"encoding":"base64"}]}).encode(),
    headers={"Content-Type":"application/json"})
raw = base64.b64decode(json.loads(urllib.request.urlopen(req, timeout=8).read())
    ["result"]["value"]["data"][0])

# byte[146]: queue type  — 1 = node queue (nodes available), 0 = job queue (no nodes)
# byte[147]: item count in queue
qtype  = raw[146]
qcount = raw[147]

if qtype == 1:
    print(f"  {name:<18} {qcount:>3} nodes available  [READY]")
elif qtype == 0 and qcount == 0:
    print(f"  {name:<18}   0 nodes           [EMPTY]")
else:
    print(f"  {name:<18}   0 nodes, {qcount} jobs queued  [FULL]")
PYEOF
  done
  echo ""
}

if [ "$1" == "--watch" ]; then
  INTERVAL="${2:-30}"
  echo "Watching every ${INTERVAL}s (Ctrl+C to stop)..."
  while true; do
    check_markets
    sleep "$INTERVAL"
  done
else
  check_markets
fi
