#!/usr/bin/env python3
"""Nosana GPU sniper — watches markets and fires jobs the moment nodes appear."""

import base64, json, os, subprocess, sys, time, urllib.request
from datetime import datetime

# ── CONFIG ────────────────────────────────────────────────────────────────────

RPC              = "https://mainnet.helius-rpc.com/?api-key=59a1f481-bc75-426e-883f-1d5b628339d3"
NOSANA_API       = "https://dashboard.k8s.prd.nos.ci/api"
WALLET           = os.path.expanduser("~/.nosana/nosana_key.json")

POLL_INTERVAL    = 1    # seconds between market checks
COOLDOWN         = 60   # seconds to wait after firing before watching a market again

# market address → { name, job_file, count (how many jobs to fire), timeout (minutes) }
_DIR = os.path.dirname(os.path.abspath(__file__))

ALL_MARKETS = {
    "97G9NnvBDQ2WpKu6fasoMsAKmfj63C9rhysJnkeWodAf": {
        "name":     "RTX 4090",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "5eX3kWkrcbwejEc1svbfP4F7NKYjtPDuyU5KnV1hUBKg": {
        "name":     "RTX 5070",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "9HnJacS25TnErsKMYJmKqWeCAMYuwY7gzhz9Eqhp5VE7": {
        "name":     "RTX 5080",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "6Xt8hgVLLL2PSHC9NtJP8E8oTdA5ZJc95hZEnHcdqKqb": {
        "name":     "RTX 5090",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "8pr3btRVcbqqGxaZspd18QWm41eByTjZR1cB58nVWiNg": {
        "name":     "RTX 5090 Community",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "Dcwz62TisNbWuto6KJM2EGYGVKnHbdZGVGmgLASzsXy8": {
        "name":     "RTX 4090 Community",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
    "9fgU7Btd5gXB3xzAFmT322KdkdUuMjX7GG1LeNT5qFj4": {
        "name":     "RTX 5080 Community",
        "job_file": os.path.join(_DIR, "job_miner.json"),
        "count":    1,
        "timeout":  360,
    },
}

# ── WALLET ────────────────────────────────────────────────────────────────────

B58_ALPHA = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

def b58encode(data: bytes) -> str:
    n = int.from_bytes(data, "big")
    out = ""
    while n:
        n, r = divmod(n, 58)
        out = B58_ALPHA[r] + out
    return B58_ALPHA[0] * (len(data) - len(data.lstrip(b"\x00"))) + out

def wallet_pubkey() -> str:
    with open(WALLET) as f:
        arr = json.load(f)
    return b58encode(bytes(arr[32:64]))

# ── CORE ──────────────────────────────────────────────────────────────────────

SPINNER = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"]

def rpc_get_multiple(addresses):
    body = json.dumps({
        "jsonrpc": "2.0", "id": 1,
        "method": "getMultipleAccounts",
        "params": [addresses, {"encoding": "base64"}]
    }).encode()
    req = urllib.request.Request(RPC, data=body, headers={"Content-Type": "application/json"})
    resp = json.loads(urllib.request.urlopen(req, timeout=8).read())
    return resp["result"]["value"]

def check_markets(addresses):
    vals = rpc_get_multiple(addresses)
    out = {}
    for addr, val in zip(addresses, vals):
        if val:
            raw = base64.b64decode(val["data"][0])
            out[addr] = {"queue_type": raw[146], "queue_count": raw[147]}
    return out

def gpu_slug(market_name):
    import re
    nums = re.search(r'\d+', market_name)
    slug = nums.group() if nums else "gpu"
    if "community" in market_name.lower():
        slug += "c"
    return slug

POST_SCRIPT = os.path.join(_DIR, "post.mjs")

def post_job(addr, cfg):
    import tempfile
    with open(cfg["job_file"]) as f:
        job_json = f.read()
    slug = gpu_slug(cfg["name"])
    patched = job_json.replace("nos-$(hostname)", f"nos-$(hostname)-{slug}")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as tmp:
        tmp.write(patched)
        tmp_path = tmp.name

    cmd = ["node", "--no-warnings", POST_SCRIPT, addr, tmp_path, str(cfg["timeout"])]
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=180)
        os.unlink(tmp_path)
        output = result.stdout.strip()
        if result.returncode == 0 and output.startswith("ok"):
            # output is "ok: <jobAddr>" or "ok: posted"
            after = output[4:].strip()
            job_addr = after if after and after != "posted" else None
            return True, job_addr or "posted"
        else:
            err = (result.stderr.strip() or output)[:120]
            return False, err
    except subprocess.TimeoutExpired:
        try: os.unlink(tmp_path)
        except: pass
        return False, "timed out"
    except Exception as e:
        try: os.unlink(tmp_path)
        except: pass
        return False, str(e)

def ts():
    return datetime.now().strftime("%H:%M:%S")

def fmt_elapsed(seconds):
    seconds = int(seconds)
    h, m = divmod(seconds // 60, 60)
    s = seconds % 60
    if h:
        return f"{h}h{m:02d}m"
    return f"{m}m{s:02d}s"

# ── DASHBOARD ─────────────────────────────────────────────────────────────────

_market_name_cache = {}

def resolve_market_name(market_addr):
    if not market_addr:
        return "unknown"
    if market_addr in ALL_MARKETS:
        return ALL_MARKETS[market_addr]["name"]
    if market_addr in _market_name_cache:
        return _market_name_cache[market_addr]
    try:
        url = f"{NOSANA_API}/markets/{market_addr}"
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        data = json.loads(urllib.request.urlopen(req, timeout=5).read())
        name = data.get("name") or data.get("title") or market_addr[:12] + "…"
        _market_name_cache[market_addr] = name
        return name
    except Exception:
        name = market_addr[:12] + "…"
        _market_name_cache[market_addr] = name
        return name

def fetch_jobs(pubkey):
    # Step 1: RPC memcmp filter to find all jobs owned by this wallet
    JOB_PROGRAM = "nosJhNRqr2bc9g1nfGDcXXTXvYUmxD4cVwy2pMWhrYM"
    body = json.dumps({
        "jsonrpc": "2.0", "id": 1,
        "method": "getProgramAccounts",
        "params": [JOB_PROGRAM, {
            "encoding": "base64",
            "dataSlice": {"offset": 0, "length": 0},
            "filters": [{"memcmp": {"offset": 136, "bytes": pubkey, "encoding": "base58"}}]
        }]
    }).encode()
    req = urllib.request.Request(RPC, data=body, headers={"Content-Type": "application/json"})
    resp = json.loads(urllib.request.urlopen(req, timeout=20).read())
    addresses = [acc["pubkey"] for acc in resp.get("result", [])]

    # Step 2: query API per-address for real state/timeStart
    jobs = []
    for addr in addresses:
        try:
            url = f"{NOSANA_API}/jobs/{addr}"
            req2 = urllib.request.Request(url, headers={"Accept": "application/json"})
            j = json.loads(urllib.request.urlopen(req2, timeout=5).read())
            if "state" not in j:
                continue
            jobs.append({
                "address":    addr,
                "market":     j.get("market", ""),
                "state":      j.get("state", 99),
                "time_start": j.get("timeStart") or 0,
            })
        except Exception:
            pass
    return jobs

def show_dashboard(pubkey):
    print()
    try:
        jobs    = fetch_jobs(pubkey)
        STATE   = {0: "QUEUED", 1: "RUNNING", 2: "DONE", 3: "STOPPED", 4: "EXPIRED"}
        active  = [j for j in jobs if j["state"] in (0, 1)]

        print(f"  ── Jobs ({len(active)} active, {len(jobs)} total) {'─'*30}")
        if not active:
            print("  No active jobs.")
        for j in active:
            addr    = j["address"]
            state   = STATE.get(j["state"], f"?({j['state']})")
            gpu     = resolve_market_name(j["market"])
            elapsed = ""
            if j["state"] == 1 and j["time_start"]:
                elapsed = fmt_elapsed(time.time() - j["time_start"])
            print(f"  {gpu:<24} {state:<8} {elapsed}")
            print(f"    address : {addr}")
            print(f"    jupyter : https://{addr}.node.k8s.prd.nos.ci")
        print(f"  {'─'*54}")
    except Exception as e:
        print(f"  [dashboard] Could not fetch jobs: {e}")
    print()

# ── MAIN ──────────────────────────────────────────────────────────────────────

def select_markets():
    entries = list(ALL_MARKETS.items())
    print("\nAvailable markets:")
    for i, (addr, cfg) in enumerate(entries, 1):
        print(f"  [{i}] {cfg['name']}")
    print(f"  [a] All markets")
    print()
    raw = input("Select markets (e.g. 1,3 or a): ").strip().lower()
    if raw == "a" or raw == "":
        return dict(entries)
    selected = {}
    for part in raw.split(","):
        part = part.strip()
        if part.isdigit():
            idx = int(part) - 1
            if 0 <= idx < len(entries):
                addr, cfg = entries[idx]
                selected[addr] = cfg
    if not selected:
        print("No valid selection — watching all markets.")
        return dict(entries)
    return selected

def select_duration():
    raw = input("Job duration in minutes [default 360]: ").strip()
    if not raw:
        return 360
    if raw.isdigit() and int(raw) > 0:
        return int(raw)
    print("Invalid input — using 360 minutes.")
    return 360

def select_max_snipes():
    raw = input("Max snipes before stopping [default unlimited]: ").strip()
    if not raw:
        return None
    if raw.isdigit() and int(raw) > 0:
        return int(raw)
    print("Invalid input — unlimited snipes.")
    return None

def main():
    MARKETS    = select_markets()
    duration   = select_duration()
    max_snipes = select_max_snipes()
    for cfg in MARKETS.values():
        cfg["timeout"] = duration
    addresses  = list(MARKETS.keys())
    cooldowns  = {}
    tick       = 0
    total_fired = 0

    pubkey = None
    try:
        pubkey = wallet_pubkey()
    except Exception:
        pass

    limit_str = str(max_snipes) if max_snipes else "unlimited"
    print(f"\n[{ts()}] Nosana sniper started — watching {len(addresses)} market(s)")
    print(f"         poll={POLL_INTERVAL}s  cooldown={COOLDOWN}s  duration={duration}m  max={limit_str}")
    if pubkey:
        print(f"         wallet={pubkey[:16]}…")
    print()
    for addr, cfg in MARKETS.items():
        print(f"  {cfg['name']:20} — {cfg['job_file'].split('/')[-1]}")
    print()

    while True:
        try:
            now  = time.time()
            spin = SPINNER[tick % len(SPINNER)]
            tick += 1

            data = check_markets(addresses)

            fired_any = False
            for addr, mkt in data.items():
                cfg    = MARKETS[addr]
                qtype  = mkt["queue_type"]
                qcount = mkt["queue_count"]

                if cooldowns.get(addr, 0) > now:
                    continue

                if qtype == 1 and qcount > 0:
                    fired_any = True
                    print(f"\n[{ts()}] 🎯 {cfg['name']}: {qcount} node(s) available — firing {cfg['count']} job(s)...")
                    for i in range(cfg["count"]):
                        ok, msg = post_job(addr, cfg)
                        if ok:
                            total_fired += 1
                            job_addr = msg if (len(msg) in (43, 44) and all(c in B58_ALPHA for c in msg)) else None
                            print(f"         ✓ job {i+1} posted  [{total_fired}/{limit_str}]")
                            if job_addr:
                                print(f"            address : {job_addr}")
                                print(f"            jupyter : https://{job_addr}.node.k8s.prd.nos.ci")
                            else:
                                print(f"            address : (run --jobs to get address)")
                            print(f"            market  : {cfg['name']}")
                            print(f"            duration: {cfg['timeout']}m")
                        else:
                            print(f"         ✗ job {i+1} FAILED: {msg}")
                    cooldowns[addr] = now + COOLDOWN

                    if max_snipes and total_fired >= max_snipes:
                        print(f"\n[{ts()}] Reached {max_snipes} snipe(s) — stopping.")
                        sys.exit(0)

            if not fired_any:
                parts = []
                for addr, mkt in data.items():
                    name   = MARKETS[addr]["name"]
                    qtype  = mkt["queue_type"]
                    qcount = mkt["queue_count"]
                    cd     = cooldowns.get(addr, 0)
                    if cd > now:
                        parts.append(f"{name}:cooldown({int(cd-now)}s)")
                    elif qtype == 1:
                        parts.append(f"{name}:{qcount}nodes")
                    else:
                        parts.append(f"{name}:{qcount}queued")
                line = "  ".join(parts)
                print(f"\033[2K\r{spin} [{ts()}]  {line}", end="", flush=True)

            time.sleep(POLL_INTERVAL)

        except KeyboardInterrupt:
            print(f"\n\n[{ts()}] Sniper stopped.")
            sys.exit(0)
        except Exception as e:
            print(f"\n[{ts()}] Error: {e} — retrying in 5s")
            time.sleep(5)

if __name__ == "__main__":
    if "--jobs" in sys.argv:
        try:
            pubkey = wallet_pubkey()
            show_dashboard(pubkey)
        except Exception as e:
            print(f"Error: {e}")
        sys.exit(0)
    main()
