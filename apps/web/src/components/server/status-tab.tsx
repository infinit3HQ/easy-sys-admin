import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { q } from '#/lib/queries'
import { bytes, duration, pct } from '#/lib/format'
import { When } from '#/components/time'
import { LineChart } from '#/components/chart'
import { Loading, Meter, Panel, cx } from '#/components/ui'

const windows = ['1h', '24h', '7d', '30d', '90d'] as const

export function StatusTab({ serverId }: { serverId: number }) {
  const { data } = useSuspenseQuery(q.server(serverId))
  const snap = data.snapshot
  const [win, setWin] = useState<(typeof windows)[number]>('24h')
  const metrics = useQuery(q.metrics(serverId, win))

  if (!snap) {
    return <p className="font-mono text-sm text-muted">No snapshot yet. The first poll runs within a minute of enrollment.</p>
  }
  const mem = snap.memTotal ? (100 * snap.memUsed) / snap.memTotal : 0
  const points = metrics.data?.points ?? []

  return (
    <div className="grid gap-6 xl:grid-cols-[2fr_1fr]">
      <div className="space-y-6">
        <Panel
          title="History"
          actions={windows.map((w) => (
            <button
              key={w}
              onClick={() => setWin(w)}
              className={cx('rounded px-2 py-0.5 font-mono text-[10px] uppercase', w === win ? 'bg-amber text-bg' : 'text-muted hover:text-ink')}
            >
              {w}
            </button>
          ))}
        >
          {metrics.isLoading ? (
            <Loading label="Loading samples" />
          ) : (
            <LineChart
              times={points.map((p) => p.at)}
              series={[
                { key: 'cpu', label: 'CPU', color: 'var(--color-amber)', values: points.map((p) => p.cpuPercent) },
                { key: 'mem', label: 'Memory', color: 'var(--color-ok)', values: points.map((p) => p.memPercent) },
                { key: 'disk', label: 'Fullest disk', color: 'var(--color-ink-2)', values: points.map((p) => p.diskPercent) },
              ]}
            />
          )}
        </Panel>

        <Panel title="Disks">
          <div className="space-y-4">
            {snap.disks.map((d) => {
              const used = d.total ? (100 * d.used) / d.total : 0
              const inodes = d.inodesTotal ? (100 * d.inodesUsed) / d.inodesTotal : 0
              return (
                <div key={d.mount}>
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <span className="font-mono text-sm text-ink">{d.mount}</span>
                    <span className="num text-xs text-ink-2">
                      {bytes(d.used)} of {bytes(d.total)} · <span className={used >= 85 ? 'text-warn' : ''}>{pct(used)}</span>
                    </span>
                  </div>
                  <Meter className="mt-1.5" value={used} />
                  <div className="mt-1 font-mono text-[10px] text-faint">
                    {d.device} · {d.fsType} · {bytes(d.avail)} free{d.inodesTotal ? ` · inodes ${pct(inodes)}` : ''}
                  </div>
                </div>
              )
            })}
          </div>
        </Panel>
      </div>

      <div className="space-y-6">
        <Panel title="Now">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-4">
            <Fact label="CPU" value={pct(snap.cpuPercent)} sub={`${snap.cpuCount} cores`} />
            <Fact label="Load" value={snap.load1.toFixed(2)} sub={`${snap.load5.toFixed(2)} · ${snap.load15.toFixed(2)}`} />
            <Fact label="Memory" value={pct(mem)} sub={`${bytes(snap.memUsed)} of ${bytes(snap.memTotal)}`} />
            <Fact label="Swap" value={snap.swapTotal ? bytes(snap.swapUsed) : 'none'} sub={snap.swapTotal ? `of ${bytes(snap.swapTotal)}` : undefined} />
            <Fact label="Uptime" value={duration(snap.uptimeSeconds)} sub={<>since <When iso={snap.bootTime} /></>} />
            <Fact label="Clock" value={snap.timeSynced ? 'synced' : 'not synced'} tone={snap.timeSynced ? undefined : 'warn'} />
          </dl>
        </Panel>

        <Panel title="System">
          <dl className="space-y-2 font-mono text-xs">
            <Row k="OS" v={snap.os} />
            <Row k="Kernel" v={snap.kernel} />
            <Row k="Arch" v={snap.arch} />
            <Row k="Hostname" v={snap.hostname} />
            <Row k="Helper" v={data.server.helperVersion || '—'} />
            <Row k="Collected" v={<When iso={snap.collectedAt} />} />
          </dl>
          {snap.rebootRequired && (
            <div className="mt-4 rounded border border-warn/40 bg-warn/10 p-3 text-xs text-warn">
              Reboot required{snap.rebootPackages?.length ? ` for ${snap.rebootPackages.join(', ')}` : ''}.
            </div>
          )}
          {snap.failedUnits.length > 0 && (
            <div className="mt-3 rounded border border-fail/40 bg-fail/10 p-3 text-xs text-fail">Failed: {snap.failedUnits.join(', ')}</div>
          )}
        </Panel>

        {snap.docker && (
          <Panel title={`Docker ${snap.docker.version}`}>
            <dl className="grid grid-cols-2 gap-4">
              <Fact label="Running" value={String(snap.docker.running)} sub={`of ${snap.docker.total}`} />
              <Fact label="Unhealthy" value={String(snap.docker.unhealthy + snap.docker.restarting)} tone={snap.docker.unhealthy + snap.docker.restarting ? 'fail' : undefined} />
              <Fact label="Images" value={bytes(snap.docker.imagesBytes)} />
              <Fact label="Build cache" value={bytes(snap.docker.buildCacheBytes)} tone={snap.docker.buildCacheBytes > 10 * 2 ** 30 ? 'warn' : undefined} />
              <Fact label="Volumes" value={bytes(snap.docker.volumesBytes)} />
              <Fact label="Writable layers" value={bytes(snap.docker.containersBytes)} />
            </dl>
          </Panel>
        )}

        <Panel title="Network since boot">
          <dl className="space-y-2 font-mono text-xs">
            {snap.net.map((n) => (
              <Row key={n.name} k={n.name} v={`↓ ${bytes(n.rxBytes)}  ↑ ${bytes(n.txBytes)}`} />
            ))}
          </dl>
        </Panel>
      </div>
    </div>
  )
}

function Fact({ label, value, sub, tone }: { label: string; value: string; sub?: ReactNode; tone?: 'warn' | 'fail' }) {
  return (
    <div>
      <dt className="label">{label}</dt>
      <dd className={cx('mt-1 font-display text-2xl font-semibold leading-none', tone === 'warn' ? 'text-warn' : tone === 'fail' ? 'text-fail' : 'text-ink')}>
        {value}
      </dd>
      {sub && <dd className="mt-1 font-mono text-[10px] text-muted">{sub}</dd>}
    </div>
  )
}

function Row({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-muted">{k}</dt>
      <dd className="truncate text-right text-ink-2">{v}</dd>
    </div>
  )
}
