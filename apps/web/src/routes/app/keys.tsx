import { Link, createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Empty, ErrorBanner, Loading, PageHeader } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { Input } from '#/components/ui/input'
import { useRole } from './route'

export const Route = createFileRoute('/app/keys')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.keys()),
  head: () => ({ meta: [{ title: 'Keys · easy-sys-admin' }] }),
  component: Keys,
})

function Keys() {
  const r = useQuery(q.keys())
  const items = r.data?.items ?? []
  return (
    <div>
      <PageHeader
        kicker="Access"
        title="Keys"
        description="Fingerprints found in the standard authorized_keys files of accounts with a login shell. A key stored anywhere else is not on this list. Naming a key does not change the server."
      />
      {r.isLoading ? (
        <Loading />
      ) : r.error ? (
        <ErrorBanner error={r.error} />
      ) : items.length === 0 ? (
        <Empty title="No keys recorded yet">
          They appear after a server with a current helper is checked. That
          happens about once an hour.
        </Empty>
      ) : (
        <ul className="grid gap-3">
          {items.map((k) => (
            <li key={k.fingerprint}>
              <Card className="p-4">
                <KeyHead k={k} />
                <ul className="mt-3 space-y-1 font-mono text-[11px] text-ink-2">
                  {k.uses.map((u) => (
                    <li key={`${u.serverId}-${u.user}`}>
                      <Link
                        to="/app/servers/$serverId"
                        params={{ serverId: u.serverId }}
                        search={{ tab: 'access' }}
                        className="text-ink hover:text-amber"
                      >
                        {u.serverName}
                      </Link>
                      <span className="text-faint">
                        {' '}
                        · {u.user}
                        {u.from ? ` · from ${u.from}` : ''}
                        {u.comment ? ` · ${u.comment}` : ''} ·{' '}
                        <Ago iso={u.observedAt} />
                      </span>
                    </li>
                  ))}
                </ul>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function KeyHead({
  k,
}: {
  k: {
    fingerprint: string
    keyType: string
    hasCommand: boolean
    label: string
  }
}) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const [label, setLabel] = useState(k.label)
  const save = useMutation({
    mutationFn: () =>
      call(
        api().PUT('/keys', {
          body: { fingerprint: k.fingerprint, label },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['keys'] }),
  })
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        <div className="font-mono text-sm break-all text-ink">
          {k.label || 'unnamed'}
        </div>
        <div className="mt-1 font-mono text-[11px] break-all text-faint">
          {k.keyType} · {k.fingerprint}
          {k.hasCommand ? ' · forced command' : ''}
        </div>
      </div>
      {canAct && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <Input
            value={label}
            maxLength={80}
            placeholder="Name"
            aria-label="Key name"
            onChange={(e) => setLabel(e.target.value)}
          />
          <Button size="sm" type="submit" busy={save.isPending}>
            Save
          </Button>
        </form>
      )}
    </div>
  )
}
