import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Schemas } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Empty, ErrorBanner, Notice, Panel } from '#/components/kit'
import { Ago } from '#/components/time'
import { Button } from '#/components/ui/button'

type Identity = Schemas['Identity']

/** Owners of keys, and an offboarding report for each. */
export function KeyOwners({ canEdit }: { canEdit: boolean }) {
  const r = useQuery(q.identities())
  const [open, setOpen] = useState('')
  const items = r.data?.items ?? []
  return (
    <Panel title="Owners">
      <p className="mb-3 text-sm text-muted-foreground">
        Name a key below to give it an owner. Who owns a key is what you say,
        not something esa can prove.
      </p>
      <ErrorBanner error={r.error} className="mb-3" />
      {items.length === 0 ? (
        <Empty title="No owners yet">Name a key to add its owner here.</Empty>
      ) : (
        <ul className="divide-y divide-line">
          {items.map((i) => (
            <li key={i.name} className="py-3">
              <OwnerRow
                i={i}
                canEdit={canEdit}
                open={open === i.name}
                onToggle={() => setOpen(open === i.name ? '' : i.name)}
              />
              {open === i.name && <OffboardingReport name={i.name} />}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

function OwnerRow({
  i,
  canEdit,
  open,
  onToggle,
}: {
  i: Identity
  canEdit: boolean
  open: boolean
  onToggle: () => void
}) {
  const qc = useQueryClient()
  const save = useMutation({
    mutationFn: (patch: Partial<Pick<Identity, 'kind' | 'status'>>) =>
      call(
        api().PUT('/identities', {
          body: {
            name: i.name,
            kind: i.kind,
            status: i.status,
            note: i.note,
            ...patch,
          },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['keys'] }),
  })
  const select = 'h-8 rounded-md border bg-bg-2 px-2 text-xs'
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0">
        <div className="text-sm break-words text-ink">{i.name}</div>
        <div className="font-mono text-[10px] tracking-wider text-faint uppercase">
          {i.keys} keys · {i.grants} logins · {i.servers} servers
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {canEdit ? (
          <>
            <select
              aria-label={`Kind of ${i.name}`}
              className={select}
              value={i.kind}
              onChange={(e) =>
                save.mutate({ kind: e.target.value as Identity['kind'] })
              }
            >
              <option value="person">person</option>
              <option value="service">service</option>
              <option value="shared">shared key</option>
            </select>
            <select
              aria-label={`Status of ${i.name}`}
              className={select}
              value={i.status}
              onChange={(e) =>
                save.mutate({ status: e.target.value as Identity['status'] })
              }
            >
              <option value="active">active</option>
              <option value="leaving">leaving</option>
              <option value="left">left</option>
            </select>
          </>
        ) : (
          <span className="text-xs text-muted-foreground">
            {i.kind} · {i.status}
          </span>
        )}
        <Button size="sm" variant="outline" onClick={onToggle}>
          {open ? 'Hide report' : 'Offboarding report'}
        </Button>
      </div>
      {save.error && <ErrorBanner error={save.error} />}
    </div>
  )
}

function OffboardingReport({ name }: { name: string }) {
  const r = useQuery(q.offboarding(name))
  if (r.error) return <ErrorBanner error={r.error} className="mt-3" />
  if (!r.data) return null
  const d = r.data
  const download = () => {
    const blob = new Blob([d.markdown], { type: 'text/markdown' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `offboarding-${name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}.md`
    a.click()
    URL.revokeObjectURL(a.href)
  }
  return (
    <div className="mt-3 space-y-3 rounded-md border border-line p-3">
      <Notice tone={d.complete ? 'info' : 'warn'}>
        {d.partial
          ? 'Partial view: some servers belong to another organization. '
          : !d.complete
            ? 'Incomplete: some servers could not be checked. '
            : ''}
        {d.statement}
      </Notice>
      {d.grants.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No known key of {name} was seen in the files esa read.
        </p>
      ) : (
        <ul className="space-y-1 font-mono text-[11px] text-ink-2">
          {d.grants.map((g) => (
            <li key={`${g.serverId}-${g.user}-${g.fingerprint}`}>
              <Link
                to="/app/servers/$serverId"
                params={{ serverId: g.serverId }}
                search={{ tab: 'access' }}
                className="text-ink hover:text-amber"
              >
                {g.server}
              </Link>
              <span className="text-faint">
                {' '}
                · {g.user}
                {g.from ? ` · from ${g.from}` : ''}
                {g.hasCommand ? ' · forced command' : ''} · seen{' '}
                <Ago iso={g.observedAt} />
              </span>
              {g.unchecked && (
                <span className="text-warn"> · {g.unchecked}</span>
              )}
            </li>
          ))}
        </ul>
      )}
      {d.sharedKeys.length > 0 && (
        <p className="text-xs text-warn">
          {d.sharedKeys.length === 1
            ? 'One key opens'
            : `${d.sharedKeys.length} keys open`}{' '}
          more than one login. Removing it affects each of them.
        </p>
      )}
      {d.unchecked.length > 0 && (
        <div className="text-xs text-muted-foreground">
          <div className="text-ink-2">Servers esa could not check:</div>
          <ul className="mt-1 space-y-0.5">
            {d.unchecked.map((u) => (
              <li key={u.serverId}>
                {u.server}: {u.reason}
              </li>
            ))}
          </ul>
        </div>
      )}
      <Button size="sm" variant="outline" onClick={download}>
        Download as Markdown
      </Button>
    </div>
  )
}
