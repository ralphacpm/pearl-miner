#!/usr/bin/env node
// Direct SDK delist/end — bypasses nosana CLI TTY issues
import { readFileSync } from 'fs';
import { homedir } from 'os';
import { execSync } from 'child_process';

const _npmRoot = execSync('npm root -g', { encoding: 'utf8' }).trim();
const { Client } = await import('file://' + _npmRoot + '/@nosana/cli/node_modules/@nosana/sdk/dist/index.js');

const [,, jobAddr] = process.argv;
if (!jobAddr) { console.error('Usage: delist.mjs <jobAddress>'); process.exit(1); }

const RPC = 'https://mainnet.helius-rpc.com/?api-key=59a1f481-bc75-426e-883f-1d5b628339d3';
const WALLET_FILE = homedir() + '/.nosana/nosana_key.json';

async function main() {
  const wallet = readFileSync(WALLET_FILE, 'utf8');
  const client = new Client('mainnet', wallet, { solana: { network: RPC } });

  let job;
  try {
    job = await client.jobs.get(jobAddr);
  } catch (e) {
    console.error('fetch-failed: ' + e.message);
    process.exit(1);
  }

  const state = typeof job.state === 'number' ? job.state : -1;
  const stateStr = typeof job.state === 'string' ? job.state.toUpperCase() : String(state);
  console.log('state: ' + stateStr);

  if (stateStr === 'COMPLETED' || stateStr === 'STOPPED' || state === 2 || state === 3) {
    console.log('already-done');
    process.exit(0);
  }

  try {
    if (stateStr === 'QUEUED' || state === 0) {
      console.log('calling delist...');
      await client.jobs.delist(jobAddr);
    } else {
      console.log('calling end...');
      await client.jobs.end(jobAddr);
    }
    console.log('ok');
  } catch (e) {
    console.error('tx-failed: ' + e.message);
    process.exit(1);
  }
}

main().catch(e => { console.error(e.message); process.exit(1); });
