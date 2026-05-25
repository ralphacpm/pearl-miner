#!/usr/bin/env node
// Direct SDK job post — bypasses nosana CLI TTY issues
// Usage: post.mjs <marketAddr> <jobJsonPath> <timeoutMinutes>
import { readFileSync } from 'fs';
import { homedir } from 'os';
import { execSync } from 'child_process';

const _npmRoot = execSync('npm root -g', { encoding: 'utf8' }).trim();
const _nosanaCli = _npmRoot + '/@nosana/cli/node_modules';

// Import Keypair from the SDK's bundled web3.js so monkey-patching affects the same class
const { Keypair } = await import('file://' + _nosanaCli + '/@solana/web3.js/lib/index.cjs.js');
const { Client } = await import('file://' + _nosanaCli + '/@nosana/sdk/dist/index.js');

const [,, marketAddr, jobJsonPath, timeoutStr] = process.argv;
if (!marketAddr || !jobJsonPath || !timeoutStr) {
  console.error('Usage: post.mjs <marketAddr> <jobJsonPath> <timeoutMinutes>');
  process.exit(1);
}

const RPC = 'https://mainnet.helius-rpc.com/?api-key=59a1f481-bc75-426e-883f-1d5b628339d3';
const WALLET_FILE = homedir() + '/.nosana/nosana_key.json';
const timeoutMins = parseInt(timeoutStr, 10);
const timeoutSecs = timeoutMins * 60;

async function main() {
  const wallet = readFileSync(WALLET_FILE, 'utf8');
  const client = new Client('mainnet', wallet, {
    solana: { network: RPC, market_address: marketAddr },
  });

  const jobDef = JSON.parse(readFileSync(jobJsonPath, 'utf8'));

  // Pre-generate the job keypair so we know the address before posting
  const jobKey = Keypair.generate();
  const jobAddr = jobKey.publicKey.toBase58();
  const tag = jobAddr.slice(0, 8);

  // Embed tag into worker name: --worker nos-$(hostname)-{slug} → --worker nos-$(hostname)-{slug}-{tag}
  for (const op of jobDef.ops || []) {
    const cmd = op?.args?.cmd;
    if (Array.isArray(cmd)) {
      op.args.cmd = cmd.map(s =>
        typeof s === 'string'
          ? s.replace(/(--worker\s+nos-\$\(hostname\)[a-zA-Z0-9_-]*)/, `$1-${tag}`)
          : s
      );
    }
  }

  // Monkey-patch Keypair.generate so list() uses our pre-generated key on its first call
  const orig = Keypair.generate;
  let firstCall = true;
  Keypair.generate = function() {
    if (firstCall) { firstCall = false; return jobKey; }
    return orig.call(this);
  };

  let result;
  try {
    result = await client.jobs.list(jobDef, timeoutSecs, marketAddr);
  } catch (e) {
    Keypair.generate = orig;
    console.error('post-failed: ' + e.message);
    process.exit(1);
  }
  Keypair.generate = orig;

  const resultAddr = result?.job?.toString() || jobAddr;
  console.log('ok: ' + resultAddr);
}

main().catch(e => { console.error(e.message); process.exit(1); });
