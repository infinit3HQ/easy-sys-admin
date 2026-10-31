import { Link, createFileRoute } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { CopyBlock, StatusDot, cx } from '#/components/ui'
import type { ServerStatus } from '@esa/api'

export const Route = createFileRoute('/')({ component: Landing })

const demo: Array<{ name: string; group: string; status: ServerStatus; cpu: number; disk: number; note: string }> = [
  { name: 'media-01', group: 'production', status: 'ok', cpu: 12, disk: 41, note: 'freed 47.2 GiB build cache' },
  { name: 'builder-02', group: 'builders', status: 'warning', cpu: 64, disk: 88, note: 'disk / at 88%' },
  { name: 'edge-01', group: 'production', status: 'ok', cpu: 4, disk: 22, note: '3 security updates installed' },
  { name: 'client-a', group: 'client A', status: 'failing', cpu: 9, disk: 97, note: 'disk /var at 97%' },
  { name: 'lab-pi', group: 'home', status: 'unreachable', cpu: 0, disk: 0, note: 'no answer for 4m' },
  { name: 'db-01', group: 'production', status: 'ok', cpu: 23, disk: 57, note: 'reboot scheduled sun 04:00' },
]

function Landing() {
  return (
    <div className="min-h-screen overflow-x-hidden">
      <Nav />
      <Hero />
      <Watches />
      <Touch />
      <Agents />
      <Install />
      <footer className="border-t border-line px-6 py-10 font-mono text-[11px] text-muted md:px-12">
        easy-sys-admin · self-hosted · Go control plane, TanStack Start board, MCP for agents
      </footer>
    </div>
  )
}

function Nav() {
  return (
    <header className="sticky top-0 z-40 flex items-center justify-between border-b border-line bg-bg/80 px-6 py-3 backdrop-blur md:px-12">
      <Link to="/" className="flex items-center gap-2.5">
        <img src="/favicon.svg" alt="" className="size-6" />
        <span className="font-display text-xl font-bold uppercase tracking-wide">easy-sys-admin</span>
      </Link>
      <nav className="flex items-center gap-6 font-mono text-xs text-ink-2">
        <a href="#watch" className="hidden hover:text-ink sm:inline">
          Watches
        </a>
        <a href="#safety" className="hidden hover:text-ink sm:inline">
          Safety
        </a>
        <a href="#agents" className="hidden hover:text-ink sm:inline">
          MCP
        </a>
        <Link to="/app" className="rounded border border-amber px-3 py-1.5 text-amber hover:bg-amber hover:text-bg">
          Open the board
        </Link>
      </nav>
    </header>
  )
}

function Hero() {
  return (
    <section className="relative grid gap-12 px-6 pb-20 pt-16 md:px-12 lg:grid-cols-[1.1fr_1fr] lg:items-center">
      <div className="animate-rise">
        <p className="label mb-6 !text-amber">Fleet control room · self-hosted</p>
        <h1 className="font-display text-[clamp(3.5rem,9vw,8.5rem)] font-extrabold uppercase leading-[0.82] tracking-tight">
          One board
          <br />
          for the
          <br />
          <span className="text-amber">whole fleet.</span>
        </h1>
        <p className="mt-8 max-w-xl text-lg leading-relaxed text-ink-2">
          See every server on one page, refreshed each minute. Put servers in groups, set when their disks get cleaned and their packages
          patched, and stop logging in to each machine. AI agents read the same data over MCP.
        </p>
        <div className="mt-10 flex flex-wrap gap-3">
          <Link to="/app" className="rounded bg-amber px-5 py-3 font-semibold text-bg hover:bg-amber-2">
            Open the board
          </Link>
          <a href="#install" className="rounded border border-line-2 px-5 py-3 text-ink hover:border-ink-2">
            Self-host it
          </a>
        </div>
      </div>
      <Board />
    </section>
  )
}

/** The hero's departure board: demo rows that flip in one after another. */
function Board() {
  const [tick, setTick] = useState(0)
  useEffect(() => {
    const t = setInterval(() => setTick((x) => x + 1), 2200)
    return () => clearInterval(t)
  }, [])
  return (
    <div className="panel animate-rise overflow-hidden shadow-[0_40px_120px_-40px_rgb(255_176_0/0.25)]" style={{ animationDelay: '120ms' }}>
      <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
        <span className="label">Fleet · demo data</span>
        <span className="flex items-center gap-2 font-mono text-[10px] text-muted">
          <span className="size-1.5 animate-blink rounded-full bg-ok" /> live
        </span>
      </div>
      <div className="divide-y divide-line">
        {demo.map((s, i) => {
          const wobble = s.status === 'unreachable' ? 0 : Math.round(Math.sin((tick + i) * 1.7) * 4)
          return (
            <div key={s.name} className="grid grid-cols-[auto_1fr_auto] items-center gap-4 px-4 py-3 animate-rise" style={{ animationDelay: `${200 + i * 90}ms` }}>
              <StatusDot status={s.status} />
              <div className="min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="font-mono text-sm text-ink">{s.name}</span>
                  <span className="font-mono text-[10px] uppercase tracking-wider text-faint">{s.group}</span>
                </div>
                <div className="truncate font-mono text-[11px] text-muted">{s.note}</div>
              </div>
              <div className="w-28 space-y-1.5">
                <MiniBar label="cpu" value={Math.max(0, s.cpu + wobble)} />
                <MiniBar label="disk" value={s.disk} />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function MiniBar({ label, value }: { label: string; value: number }) {
  const c = value >= 95 ? 'bg-fail' : value >= 85 ? 'bg-warn' : 'bg-ink-2'
  return (
    <div className="flex items-center gap-2">
      <span className="w-7 font-mono text-[9px] uppercase text-faint">{label}</span>
      <div className="h-1 flex-1 bg-line">
        <div className={cx('h-full transition-[width] duration-1000', c)} style={{ width: `${value}%` }} />
      </div>
    </div>
  )
}

const watches = [
  ['Status', 'CPU, memory, load, every real disk and its inodes, uptime, network. Polled each minute, kept 90 days.'],
  ['Updates', 'Pending packages split into security and the rest. Upgrades run as their own systemd job, so a dropped SSH session cannot kill apt halfway.'],
  ['Cleanups', 'Idle Docker build cache and stale VS Code server copies, on the group schedule. A server running a build is skipped.'],
  ['Services', 'Every systemd unit, failed ones first. Start, stop, restart. sshd and Docker cannot be stopped from here.'],
  ['Containers', 'State, health, restarts, and whether the registry has a newer image. Pull and recreate one compose service at a time.'],
  ['Logs', 'Journal per unit and container output, filtered by priority and text.'],
  ['Access', 'Who has a login shell, which SSH keys they carry, who is in sudo and docker, recent logins, failed attempts.'],
  ['Security', 'Listening ports and their processes, firewall, CrowdSec and fail2ban, unattended-upgrades.'],
  ['Alerts', 'Disk, memory, unreachable, failed units, unhealthy containers, expiring certificates, failed runs. To ntfy, Discord, Slack, a webhook, or email.'],
]

function Watches() {
  return (
    <section id="watch" className="border-t border-line px-6 py-20 md:px-12">
      <h2 className="font-display text-6xl font-bold uppercase leading-none">What it watches</h2>
      <div className="mt-12 grid gap-px overflow-hidden rounded-md border border-line bg-line sm:grid-cols-2 lg:grid-cols-3">
        {watches.map(([title, body], i) => (
          <div key={title} className="bg-bg p-6">
            <div className="flex items-baseline gap-3">
              <span className="font-mono text-[10px] text-amber">{String(i + 1).padStart(2, '0')}</span>
              <h3 className="font-display text-2xl font-semibold uppercase tracking-wide">{title}</h3>
            </div>
            <p className="mt-3 text-sm leading-relaxed text-ink-2">{body}</p>
          </div>
        ))}
      </div>
    </section>
  )
}

const rules = [
  'Never deletes Docker volumes.',
  'Never removes an image a container uses, never runs system prune.',
  'Never restarts Docker or the edge containers from a cleanup.',
  'Stops everything for a server whose host key changed.',
  'Never shows a password or private key again after you save it.',
  'Never reboots the server it runs on from a schedule.',
]

function Touch() {
  return (
    <section id="safety" className="grid gap-12 border-t border-line px-6 py-20 md:px-12 lg:grid-cols-2">
      <div>
        <h2 className="font-display text-6xl font-bold uppercase leading-none">
          One binary.
          <br />
          One sudo line.
        </h2>
        <p className="mt-6 max-w-lg leading-relaxed text-ink-2">
          Each server gets a small helper and a system user that may run only that helper. The control plane logs in with its own key over
          SSH, pins the host key it saw the first time, and refuses to talk to a machine that presents a different one. No agent listens on
          your servers.
        </p>
        <CopyBlock className="mt-8 max-w-lg" value={'esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *'} />
      </div>
      <div className="panel p-6">
        <div className="label mb-4">Rules with no switch</div>
        <ul className="space-y-3">
          {rules.map((r) => (
            <li key={r} className="flex gap-3 text-sm text-ink">
              <span className="mt-1.5 size-1.5 shrink-0 bg-amber" />
              {r}
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

function Agents() {
  const snippet = `{
  "mcpServers": {
    "esa": {
      "type": "http",
      "url": "https://esa.example.com/mcp",
      "headers": { "Authorization": "Bearer esa_..." }
    }
  }
}`
  return (
    <section id="agents" className="grid gap-12 border-t border-line px-6 py-20 md:px-12 lg:grid-cols-[1fr_1.1fr]">
      <div>
        <h2 className="font-display text-6xl font-bold uppercase leading-none">Agents read the fleet</h2>
        <p className="mt-6 max-w-lg leading-relaxed text-ink-2">
          The MCP endpoint gives an agent the same view you have: fleet summary, per-server status, updates, services, containers, security
          and access reports, run history, alerts. It uses an API token you create, so it can do what that token&apos;s scopes allow and
          nothing more. Every call lands in the activity log, marked MCP.
        </p>
        <p className="mt-4 font-mono text-xs text-muted">“Which servers are low on disk?” · “What failed last night?” · “Any containers with newer images?”</p>
      </div>
      <CopyBlock value={snippet} />
    </section>
  )
}

function Install() {
  return (
    <section id="install" className="border-t border-line px-6 py-20 md:px-12">
      <h2 className="font-display text-6xl font-bold uppercase leading-none">Run it</h2>
      <div className="mt-10 grid gap-6 lg:grid-cols-3">
        {(
          [
            ['Start', 'cp deploy/.env.example deploy/.env\n# set ESA_MASTER_KEY, the database password, OIDC\ndocker compose -f deploy/compose.yaml up -d'],
            ['Add a server', '# in the board: Add server, check the host key,\n# then paste the one-time command on the server:\ncurl -fsSL https://esa.example.com/enroll/enr_… | sudo sh'],
            ['Use the CLI', 'esa login https://esa.example.com --token esa_…\nesa status\nesa runs start clean --group production'],
          ] as const
        ).map(([t, code], i) => (
          <div key={t}>
            <div className="mb-3 flex items-baseline gap-3">
              <span className="font-mono text-[10px] text-amber">{i + 1}</span>
              <h3 className="font-display text-2xl font-semibold uppercase">{t}</h3>
            </div>
            <CopyBlock value={code} />
          </div>
        ))}
      </div>
    </section>
  )
}
