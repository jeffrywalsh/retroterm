// Run on an administrator machine with Node 22+. No credentials are printed.
// Adapted from PeakRunner's src/deploy/public/cloudflare.mjs; see README.md.
import fs from 'node:fs';
import {spawnSync} from 'node:child_process';

const spec = JSON.parse(fs.readFileSync(new URL('./cloudflare.json', import.meta.url)));
const known = ['--apply', '--sync-config', '--start', '--cutover', '--rollback', '--check-up', '--check-down'];
const args = new Set(process.argv.slice(2));
const has = flag => args.has(flag);
const host = process.env.RETROTERM_SSH_HOST || '';
const directory = process.env.RETROTERM_DEPLOY_DIR || '/data/retroterm/tunnel';
const sshPort = process.env.RETROTERM_SSH_PORT || '22';
// A host where the admin isn't in the docker group runs Docker through
// passwordless sudo, keeping only the tunnel token.
const sudo = process.env.RETROTERM_REMOTE_SUDO === '1'
  ? 'sudo -n --preserve-env=RETROTERM_TUNNEL_TOKEN ' : '';
const fail = message => { throw new Error(message); };
const quote = s => "'" + s.replaceAll("'", "'\\''") + "'";

async function main() {
  if ([...args].some(a => !known.includes(a))) fail('Unknown argument.');
  if (has('--sync-config') && !has('--apply')) fail('--sync-config requires --apply.');
  if (has('--cutover') && has('--rollback')) fail('--cutover and --rollback are exclusive.');
  if (has('--check-up') && has('--check-down')) fail('--check-up and --check-down are exclusive.');
  if (!process.env.CF_API_TOKEN_FILE) fail('Set CF_API_TOKEN_FILE to your protected token file.');
  if (has('--start') && !host) fail('Set RETROTERM_SSH_HOST to the host running this Compose project.');
  if (host && (!/^[A-Za-z0-9_.@:-]+$/.test(host) || host.startsWith('-'))) fail('Invalid SSH destination.');
  if (!/^[0-9]{1,5}$/.test(sshPort)) fail('Invalid SSH port.');
  if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(spec.previous_origin ?? '')) fail('previous_origin must be an IPv4 address.');

  const source = fs.readFileSync(process.env.CF_API_TOKEN_FILE, 'utf8').trim();
  const token = source.match(/Bearer\s+([A-Za-z0-9_-]+)/i)?.[1]
    ?? (/^[A-Za-z0-9_-]+$/.test(source) ? source : undefined);
  if (!token) fail('Unrecognized token file; contents withheld.');
  async function api(path, method = 'GET', body) {
    const response = await fetch('https://api.cloudflare.com/client/v4' + path, {
      method, headers: {Authorization: 'Bearer ' + token, 'Content-Type': 'application/json'},
      body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(20000),
    });
    const data = await response.json();
    if (!response.ok || !data.success) fail(`Cloudflare ${method}: HTTP ${response.status}; codes ${(data.errors ?? []).map(e => e.code).join(',')}`);
    return data.result;
  }

  const zones = await api('/zones?name=' + encodeURIComponent(spec.zone));
  if (zones.length !== 1 || zones[0].status !== 'active') fail('Expected one active zone.');
  const zone = zones[0];
  const base = `/accounts/${zone.account.id}/cfd_tunnel`;
  const tunnels = (await api(base + '?is_deleted=false')).filter(t => t.name === spec.tunnel);
  if (tunnels.length > 1) fail('Ambiguous tunnel name.');
  let tunnel = tunnels[0];
  if (!tunnel && has('--apply')) tunnel = await api(base, 'POST', {name: spec.tunnel, config_src: 'cloudflare'});
  if (!tunnel) fail('Tunnel missing; --apply can provision it.');

  // Routing comes from cloudflare.json, never the dashboard.
  const current = await api(`${base}/${tunnel.id}/configurations`);
  if (has('--apply') && (has('--sync-config') || !current.config?.ingress?.length)) {
    await api(`${base}/${tunnel.id}/configurations`, 'PUT', {config: spec.config});
    console.log('Applied version-controlled tunnel routing.');
  }

  // DNS. Only --cutover, --rollback and the check flags write records, and
  // each replaces only a record it recognizes: our tunnel CNAME, or the
  // proxied A record at previous_origin. Anything else is refused.
  const target = `${tunnel.id}.cfargotunnel.com`;
  const records = `/zones/${zone.id}/dns_records`;
  // Only address records can collide with the tunnel CNAME. MX and TXT at the
  // same name (email routing, DKIM) are never read or touched.
  const lookup = async name => (await api(`${records}?name=${encodeURIComponent(name)}`))
    .filter(r => ['A', 'AAAA', 'CNAME'].includes(r.type));
  const isTunnel = r => r.type === 'CNAME' && r.content === target && r.proxied;
  const isOrigin = r => r.type === 'A' && r.content === spec.previous_origin && r.proxied;
  const viaTunnel = name => ({name, type: 'CNAME', content: target, proxied: true, ttl: 1});

  for (const {name, before} of spec.hosts) {
    const found = await lookup(name);
    if (found.length > 1) fail(`Several DNS records for ${name}; preserved unchanged.`);
    const record = found[0];
    let state = !record ? 'none' : isTunnel(record) ? 'tunnel' : isOrigin(record) ? 'origin' : 'other';
    if (state === 'other') fail(`Unexpected DNS for ${name}; preserved unchanged.`);
    if (has('--cutover') && state !== 'tunnel') {
      if (record) await api(`${records}/${record.id}`, 'PUT', viaTunnel(name));
      else await api(records, 'POST', viaTunnel(name));
      state = 'tunnel (cut over)';
    } else if (has('--rollback') && state === 'tunnel') {
      if (before === 'A') {
        await api(`${records}/${record.id}`, 'PUT', {name, type: 'A', content: spec.previous_origin, proxied: true, ttl: 1});
        state = 'origin (rolled back)';
      } else {
        await api(`${records}/${record.id}`, 'DELETE');
        state = 'none (rolled back)';
      }
    }
    console.log(`${name}: ${state}`);
  }

  // A throwaway hostname that reaches the site through the tunnel before any
  // real record moves (its ingress rule sets the Host header).
  if (spec.check) {
    const name = spec.check.name;
    const found = await lookup(name);
    if (found.length > 1 || (found.length === 1 && !isTunnel(found[0]))) fail(`Unexpected DNS for ${name}; preserved unchanged.`);
    let state = found.length ? 'up' : 'down';
    if (has('--check-up') && !found.length) { await api(records, 'POST', viaTunnel(name)); state = 'up (created)'; }
    if (has('--check-down') && found.length) { await api(`${records}/${found[0].id}`, 'DELETE'); state = 'down (removed)'; }
    console.log(`${name} (check): ${state}`);
  }

  if (has('--start')) {
    const tunnelToken = await api(`${base}/${tunnel.id}/token`);
    const remote = 'set -eu; IFS= read -r RETROTERM_TUNNEL_TOKEN; export RETROTERM_TUNNEL_TOKEN; '
      + `cd ${quote(directory)}; ${sudo}docker network inspect data_default >/dev/null; `
      + `${sudo}docker compose -f compose.yaml config --quiet; `
      + `${sudo}docker compose -f compose.yaml up -d`;
    const result = spawnSync('ssh', ['-p', sshPort, '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=5', host, remote], {
      input: tunnelToken + '\n', encoding: 'utf8', timeout: 180000,
    });
    if (result.status !== 0) fail('Compose start failed; remote output withheld for credential safety.');
    console.log('Started the tunnel connector; the API credential stayed on this machine.');
  }

  const state = await api(`${base}/${tunnel.id}`);
  console.log(`Tunnel: ${state.name}; status: ${state.status}; connections: ${state.connections?.length ?? 0}`);
}

main().catch(error => {
  // Filesystem/fetch errors can include sensitive paths; print only our own errors.
  console.error(error.constructor === Error ? error.message : 'Operation failed; details withheld.');
  process.exitCode = 1;
});
