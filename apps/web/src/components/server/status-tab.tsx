import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { q } from '#/lib/queries'
import { bytes, duration, pct } from '#/lib/format'
import { When } from '#/components/time'
import { LineChart } from '#/components/chart'
import {
  KeyValue,
  Loading,
  Meter,
  Notice,
  Panel,
  Segmented,
  Stat,
  cn,
} from '#/components/kit'
import { Card } from '#/components/ui/card'

const windows = ['1h', '24h', '7d', '30d', '90d'] as const
type Window = (typeof windows)[number]

export function StatusTab({ serverId }: { serverId: number }) {
  const { data } = useSuspenseQuery(q.server(serverId))
  const snap = data.snapshot
  const [win, setWin] = useState<Window>('24h')
  const metrics = useQuery(q.metrics(serverId, win))

  if (!snap) {
    return (
      <Notice>
        No snapshot yet. The first poll runs within a minute of enrollment.
      </Notice>
    )
  }
  const mem = snap.memTotal ? (100 * snap.memUsed) / snap.memTotal : 0
  const points = metrics.data?.points ?? []

  return (
    <div className="space-y-6">
      <Card className="grid grid-cols-2 gap-x-6 gap-y-5 p-4 sm:grid-cols-3 sm:p-5 xl:grid-cols-6">
        <Stat
          size="md"
          label="CPU"
          value={pct(snap.cpuPercent)}
          sub={`${snap.cpuCount} cores`}
          tone={snap.cpuPercent >= 90 ? 'warn' : undefined}
        />
        <Stat
          size="md"
          label="Load"
          value={snap.load1.toFixed(2)}
          sub={`5m ${snap.load5.toFixed(2)} · 15m ${snap.load15.toFixed(2)}`}
        />
        <Stat
          size="md"
          label="Memory"
          value={pct(mem)}
          sub={`${bytes(snap.memUsed)} of ${bytes(snap.memTotal)}`}
          tone={mem >= 90 ? 'warn' : undefined}
        />
        <Stat
          size="md"
          label="Swap"
          word={!snap.swapTotal}
          value={snap.swapTotal ? bytes(snap.swapUsed) : 'none'}
          sub={
            snap.swapTotal ? `of ${bytes(snap.swapTotal)}` : 'not configured'
          }
        />
        <Stat
          size="md"
          label="Uptime"
          value={duration(snap.uptimeSeconds)}
          sub={
            <>
              since <When iso={snap.bootTime} />
            </>
          }
        />
        <Stat
          size="md"
          label="Clock"
          word
          value={
            <span className="inline-flex items-center gap-2">
              <span
                className={cn(
                  'size-2 rounded-full',
                  snap.timeSynced ? 'bg-ok' : 'bg-warn',
                )}
              />
              {snap.timeSynced ? 'synced' : 'not synced'}
            </span>
          }
          sub="NTP"
          tone={snap.timeSynced ? undefined : 'warn'}
        />
      </Card>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <div className="min-w-0 space-y-6">
          <Panel
            title="History"
            actions={
              <Segmented
                size="sm"
                aria-label="Time window"
                value={win}
                onChange={setWin}
                options={windows.map((w) => ({ value: w, label: w }))}
              />
            }
          >
            {metrics.isLoading ? (
              <Loading label="Loading samples" />
            ) : (
              <LineChart
                times={points.map((p) => p.at)}
                series={[
                  {
                    key: 'cpu',
                    label: 'CPU',
                    color: 'var(--color-amber)',
                    values: points.map((p) => p.cpuPercent),
                  },
                  {
                    key: 'mem',
                    label: 'Memory',
                    color: 'var(--color-ok)',
                    values: points.map((p) => p.memPercent),
                  },
                  {
                    key: 'disk',
                    label: 'Fullest disk',
                    color: 'var(--color-ink-2)',
                    values: points.map((p) => p.diskPercent),
                  },
                ]}
              />
            )}
          </Panel>

          <Panel title="Disks">
            <div className="space-y-5">
              {snap.disks.map((d) => {
                const used = d.total ? (100 * d.used) / d.total : 0
                const inodes = d.inodesTotal
                  ? (100 * d.inodesUsed) / d.inodesTotal
                  : 0
                return (
                  <div key={d.mount}>
                    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                      <span className="font-mono text-sm break-all text-ink">
                        {d.mount}
                      </span>
                      <span className="num text-xs text-ink-2">
                        {bytes(d.used)} of {bytes(d.total)} ·{' '}
                        <span className={used >= 85 ? 'text-warn' : ''}>
                          {pct(used)}
                        </span>
                      </span>
                    </div>
                    <Meter className="mt-1.5" value={used} />
                    <div className="mt-1 font-mono text-[10px] break-all text-faint">
                      {d.device} · {d.fsType} · {bytes(d.avail)} free
                      {d.inodesTotal ? ` · inodes ${pct(inodes)}` : ''}
                    </div>
                  </div>
                )
              })}
            </div>
          </Panel>
        </div>

        <div className="min-w-0 space-y-6">
          <Panel title="System">
            <dl className="space-y-2">
              <KeyValue k="OS" v={snap.os} />
              <KeyValue k="Kernel" v={snap.kernel} />
              <KeyValue k="Arch" v={snap.arch} />
              <KeyValue k="Hostname" v={snap.hostname} />
              <KeyValue k="Helper" v={data.server.helperVersion || '—'} />
              <KeyValue k="Collected" v={<When iso={snap.collectedAt} />} />
            </dl>
            {snap.rebootRequired && (
              <Notice tone="warn" className="mt-4">
                Reboot required
                {snap.rebootPackages?.length
                  ? ` for ${snap.rebootPackages.join(', ')}`
                  : ''}
                .
              </Notice>
            )}
            {snap.failedUnits.length > 0 && (
              <div className="mt-3 rounded-md border border-fail/40 bg-fail/10 p-3 font-mono text-xs break-all text-fail">
                Failed: {snap.failedUnits.join(', ')}
              </div>
            )}
          </Panel>

          {snap.docker && (
            <Panel title={`Docker ${snap.docker.version}`}>
              <div className="grid grid-cols-2 gap-x-4 gap-y-5">
                <Stat
                  size="md"
                  label="Running"
                  value={String(snap.docker.running)}
                  sub={`of ${snap.docker.total}`}
                />
                <Stat
                  size="md"
                  label="Unhealthy"
                  value={String(snap.docker.unhealthy + snap.docker.restarting)}
                  tone={
                    snap.docker.unhealthy + snap.docker.restarting
                      ? 'fail'
                      : undefined
                  }
                />
                <Stat
                  size="md"
                  label="Images"
                  value={bytes(snap.docker.imagesBytes)}
                />
                <Stat
                  size="md"
                  label="Build cache"
                  value={bytes(snap.docker.buildCacheBytes)}
                  tone={
                    snap.docker.buildCacheBytes > 10 * 2 ** 30
                      ? 'warn'
                      : undefined
                  }
                />
                <Stat
                  size="md"
                  label="Volumes"
                  value={bytes(snap.docker.volumesBytes)}
                />
                <Stat
                  size="md"
                  label="Writable layers"
                  value={bytes(snap.docker.containersBytes)}
                />
              </div>
            </Panel>
          )}

          <Panel title="Network since boot">
            <dl className="space-y-2">
              {snap.net.map((n) => (
                <KeyValue
                  key={n.name}
                  k={n.name}
                  v={`↓ ${bytes(n.rxBytes)}  ↑ ${bytes(n.txBytes)}`}
                />
              ))}
            </dl>
          </Panel>
        </div>
      </div>
    </div>
  )
}
