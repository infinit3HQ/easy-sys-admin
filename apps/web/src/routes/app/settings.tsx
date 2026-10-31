import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { KeyRoundIcon } from 'lucide-react'
import type { Scope } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { CopyBlock, ErrorBanner, Field, Notice, NumberInput, PageHeader, Panel, Toggle } from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { Separator } from '#/components/ui/separator'
import { useRole } from './route'

export const Route = createFileRoute('/app/settings')({
  head: () => ({ meta: [{ title: 'Settings · easy-sys-admin' }] }),
  component: Settings,
})

const scopeText: Record<Scope, string> = {
  read: 'Read the board, servers, runs, and alerts',
  'logs:read': 'Read server and container logs',
  actions: 'Start runs and service or container actions',
  admin: 'Change servers, groups, rules, and certificates',
}

function Settings() {
  // The server render cannot know the browser's origin; fill it in after hydration.
  const [origin, setOrigin] = useState('https://esa.example.com')
  useEffect(() => setOrigin(window.location.origin), [])
  return (
    <div>
      <PageHeader kicker="Account" title="Settings" />
      <div className="grid gap-6 xl:grid-cols-2">
        <Tokens origin={origin} />
        <div className="min-w-0 space-y-6">
          <SshKey />
          <Panel title="CLI">
            <CopyBlock value={`esa login ${origin} --token esa_…\nesa status\nesa servers show <name>\nesa runs start measure --group <group>`} />
          </Panel>
        </div>
      </div>
    </div>
  )
}

function Tokens({ origin }: { origin: string }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const tokens = useQuery(q.tokens())
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<Array<Scope>>(['read'])
  const [days, setDays] = useState(90)
  const [secret, setSecret] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () => call(api().POST('/tokens', { body: { name, scopes, expiresInDays: days } })),
    onSuccess: (r) => {
      setSecret(r.secret)
      setName('')
      return qc.invalidateQueries({ queryKey: ['tokens'] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/tokens/{tokenId}', {
          params: { path: { tokenId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tokens'] }),
  })

  const mcp = JSON.stringify(
    {
      mcpServers: {
        esa: {
          type: 'http',
          url: `${origin}/mcp`,
          headers: { Authorization: `Bearer ${secret ?? 'esa_…'}` },
        },
      },
    },
    null,
    2,
  )
  const list = tokens.data?.items ?? []

  return (
    <Panel title="API tokens · CLI and MCP">
      {secret && (
        <div className="mb-6 space-y-3">
          <Notice tone="warn">Copy the token now. esa stores only a hash and cannot show it again.</Notice>
          <CopyBlock value={secret} />
          <div className="label pt-1">MCP client config</div>
          <CopyBlock value={mcp} />
        </div>
      )}
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="claude on laptop" required />
          </Field>
          <Field label="Expires after (days)" hint="0 never expires">
            <NumberInput min={0} max={3650} value={days} onChange={setDays} />
          </Field>
        </div>
        <fieldset>
          <legend className="label mb-1">Scopes</legend>
          {(Object.keys(scopeText) as Array<Scope>).map((s) => (
            <Toggle
              key={s}
              checked={scopes.includes(s)}
              onChange={(on) => setScopes(on ? [...scopes, s] : scopes.filter((x) => x !== s || s === 'read'))}
              label={<span className="font-mono text-xs">{s}</span>}
              hint={scopeText[s] + (s !== 'read' && !canAct ? ' (needs the admin role)' : '')}
            />
          ))}
        </fieldset>
        <ErrorBanner error={create.error} />
        <Button type="submit" busy={create.isPending} className="w-full sm:w-auto">
          <KeyRoundIcon /> Create token
        </Button>
      </form>
      {list.length > 0 && (
        <>
          <Separator className="my-5" />
          <ul className="space-y-3">
            {list.map((t) => (
              <li key={t.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs">
                <span className={t.revokedAt ? 'text-faint line-through' : 'text-ink'}>{t.name}</span>
                <span className="text-faint">{t.prefix}…</span>
                <span className="flex flex-wrap gap-1">
                  {t.scopes.map((s) => (
                    <Badge key={s} variant="outline">
                      {s}
                    </Badge>
                  ))}
                </span>
                <span className="ml-auto flex items-center gap-2">
                  <span className="text-faint">
                    {t.revokedAt ? (
                      'revoked'
                    ) : t.lastUsedAt ? (
                      <>
                        used <Ago iso={t.lastUsedAt} />
                      </>
                    ) : (
                      'never used'
                    )}
                  </span>
                  {!t.revokedAt && (
                    <Button
                      size="xs"
                      variant="ghost"
                      className="hover:text-fail"
                      onClick={() => revoke.mutate(t.id)}
                      busy={revoke.isPending && revoke.variables === t.id}
                    >
                      revoke
                    </Button>
                  )}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Panel>
  )
}

function SshKey() {
  const key = useQuery(q.sshKey())
  return (
    <Panel title="Control plane SSH key">
      <p className="mb-3 text-xs text-muted-foreground">
        esa logs in to servers as the esa user with this key. It may run only esa-helper there. To enroll a server by hand, put it in{' '}
        <span className="font-mono">~esa/.ssh/authorized_keys</span> with the <span className="font-mono">restrict</span> option.
      </p>
      {key.data && (
        <>
          <CopyBlock value={key.data.publicKey} />
          <p className="mt-2 font-mono text-[11px] break-all text-muted-foreground">{key.data.fingerprint}</p>
        </>
      )}
    </Panel>
  )
}
