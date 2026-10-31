import { useState } from 'react'
import type { GroupInput } from '@esa/api'
import { Button, ErrorBanner, Field, Panel, Toggle, cx } from '#/components/ui'

const days = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const
type Day = (typeof days)[number]

export const defaultGroup: GroupInput = {
  name: '',
  timezone: typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : 'UTC',
  clean: { enabled: false, days: ['sun'], time: '03:30', buildCache: true, vscode: true, journal: false, journalSize: '200M' },
  updates: { enabled: false, mode: 'security', days: ['sun'], time: '04:00', hold: [], rebootIfRequired: false },
}

function DayPicker({ value, onChange }: { value: Array<Day>; onChange: (d: Array<Day>) => void }) {
  return (
    <div className="flex flex-wrap gap-1">
      {days.map((d) => {
        const on = value.includes(d)
        return (
          <button
            key={d}
            type="button"
            onClick={() => onChange(on ? value.filter((x) => x !== d) : [...value, d])}
            className={cx('w-11 rounded border py-1.5 font-mono text-[11px] uppercase', on ? 'border-amber bg-amber/15 text-amber' : 'border-line-2 text-muted')}
          >
            {d}
          </button>
        )
      })}
    </div>
  )
}

export function GroupForm({
  initial,
  submitLabel,
  busy,
  error,
  disabled,
  onSubmit,
}: {
  initial: GroupInput
  submitLabel: string
  busy?: boolean
  error?: unknown
  disabled?: boolean
  onSubmit: (g: GroupInput) => void
}) {
  const [g, setG] = useState<GroupInput>(initial)
  const [hold, setHold] = useState(initial.updates.hold.join(', '))
  const zones = typeof Intl !== 'undefined' && 'supportedValuesOf' in Intl ? Intl.supportedValuesOf('timeZone') : [g.timezone]
  const setClean = (c: Partial<GroupInput['clean']>) => setG({ ...g, clean: { ...g.clean, ...c } })
  const setUpd = (u: Partial<GroupInput['updates']>) => setG({ ...g, updates: { ...g.updates, ...u } })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSubmit({ ...g, updates: { ...g.updates, hold: hold.split(',').map((h) => h.trim()).filter(Boolean) } })
      }}
    >
      <fieldset disabled={disabled} className="space-y-6">
        <Panel title="Group">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <input className="input" value={g.name} onChange={(e) => setG({ ...g, name: e.target.value })} placeholder="production" required />
            </Field>
            <Field label="Timezone" hint="Schedule times are in this zone.">
              <select className="input" value={g.timezone} onChange={(e) => setG({ ...g, timezone: e.target.value })}>
                {[...new Set(['UTC', ...zones])].map((z) => (
                  <option key={z}>{z}</option>
                ))}
              </select>
            </Field>
          </div>
        </Panel>

        <Panel title="Cleanups">
          <Toggle checked={g.clean.enabled} onChange={(enabled) => setClean({ enabled })} label="Clean on a schedule" />
          <div className={cx('mt-3 space-y-4', !g.clean.enabled && 'opacity-50')}>
            <div className="flex flex-wrap items-end gap-6">
              <Field label="Days">
                <DayPicker value={g.clean.days} onChange={(d) => setClean({ days: d })} />
              </Field>
              <Field label="Time">
                <input className="input w-28 font-mono" type="time" value={g.clean.time} onChange={(e) => setClean({ time: e.target.value })} />
              </Field>
            </div>
            <div className="grid gap-1 sm:grid-cols-2">
              <Toggle checked={g.clean.buildCache} onChange={(buildCache) => setClean({ buildCache })} label="Docker build cache" hint="Only when idle. A server running a build is skipped." />
              <Toggle checked={g.clean.vscode} onChange={(vscode) => setClean({ vscode })} label="Old VS Code server copies" hint="Keeps the newest copy and any copy in use." />
              <Toggle checked={g.clean.journal} onChange={(journal) => setClean({ journal })} label="Cap the systemd journal" />
              <Field label="Journal size">
                <input className="input w-28 font-mono" value={g.clean.journalSize} onChange={(e) => setClean({ journalSize: e.target.value })} />
              </Field>
            </div>
          </div>
        </Panel>

        <Panel title="Package updates">
          <Toggle checked={g.updates.enabled} onChange={(enabled) => setUpd({ enabled })} label="Install updates on a schedule" hint="One server at a time. The first failure stops the rest of the group." />
          <div className={cx('mt-3 space-y-4', !g.updates.enabled && 'opacity-50')}>
            <div className="flex flex-wrap items-end gap-6">
              <Field label="What">
                <select className="input" value={g.updates.mode} onChange={(e) => setUpd({ mode: e.target.value as 'security' | 'all' })}>
                  <option value="security">Security updates only</option>
                  <option value="all">All updates (apt upgrade)</option>
                </select>
              </Field>
              <Field label="Days">
                <DayPicker value={g.updates.days} onChange={(d) => setUpd({ days: d })} />
              </Field>
              <Field label="Time">
                <input className="input w-28 font-mono" type="time" value={g.updates.time} onChange={(e) => setUpd({ time: e.target.value })} />
              </Field>
            </div>
            <Field label="Never upgrade" hint="Package names, comma-separated.">
              <input className="input font-mono" value={hold} onChange={(e) => setHold(e.target.value)} placeholder="docker-ce, postgresql-16" />
            </Field>
            <Toggle
              checked={g.updates.rebootIfRequired}
              onChange={(rebootIfRequired) => setUpd({ rebootIfRequired })}
              label="Reboot after updates when a server asks for it"
              hint="One at a time, waiting for each to come back. The server hosting esa is skipped."
            />
          </div>
        </Panel>

        <ErrorBanner error={error} />
        <Button type="submit" variant="primary" busy={busy}>
          {submitLabel}
        </Button>
      </fieldset>
    </form>
  )
}
