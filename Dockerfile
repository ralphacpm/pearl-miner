FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates libstdc++6 && \
    rm -rf /var/lib/apt/lists/*

COPY pearl-miner /usr/local/bin/pearl-miner
RUN chmod +x /usr/local/bin/pearl-miner
