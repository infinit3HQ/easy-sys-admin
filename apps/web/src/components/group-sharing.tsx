import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { LinkIcon } from 'lucide-react'
import type { Schemas } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Until } from '#/components/time'
import {
  ConfirmDialog,
  CopyBlock,
  ErrorBanner,
  Field,
  Notice,
  Panel,
  Segmented,
} from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Separator } from '#/components/ui/separator'

type ShareAccess = Schemas['ShareAccess']
type GroupShare = Schemas['GroupShare']

const accessOptions: ReadonlyArray<{ value: ShareAccess; label: string }> = [
  { value: 'view', label: 'view' },
  { value: 'operate', label: 'operate' },
]

const accessHint: Record<ShareAccess, string> = {
  view: 'The other team sees the servers, metrics, runs, and alerts.',
  operate:
    'Its admins can also start runs, act on services and containers, and read logs.',
}

/** Who a group is shared with, and one-time links to share it further. For admins of the owning team. */
export function GroupSharing({ groupId }: { groupId: number }) {
  const qc = useQueryClient()
  const shares = useQuery(q.groupShares(groupId))
  const links = useQuery(q.shareLinks(groupId))
  const [access, setAccess] = useState<ShareAccess>('view')
  const [created, setCreated] = useState<Schemas['CreatedShareLink'] | null>(
    null,
  )
  const [stopping, setStopping] = useState<GroupShare | null>(null)

  const create = useMutation({
    mutationFn: () =>
      call(
        api().POST('/groups/{groupId}/share-links', {
          params: { path: { groupId } },
          body: { access },
        }),
      ),
    onSuccess: (r) => {
      setCreated(r)
      return qc.invalidateQueries({ queryKey: ['share-links', groupId] })
    },
  })
  const update = useMutation({
    mutationFn: ({ id, to }: { id: number; to: ShareAccess }) =>
      call(
        api().PATCH('/group-shares/{shareId}', {
          params: { path: { shareId: id } },
          body: { access: to },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ['group-shares', groupId] }),
  })
  const stop = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/group-shares/{shareId}', {
          params: { path: { shareId: id } },
        }),
      ),
    onSuccess: () => {
      setStopping(null)
      return qc.invalidateQueries({ queryKey: ['group-shares', groupId] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/share-links/{linkId}', {
          params: { path: { linkId: id } },
        }),
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ['share-links', groupId] }),
  })

  const shareList = shares.data?.items ?? []
  const linkList = links.data?.items ?? []

  return (
    <Panel title="Sharing" className="mt-6">
      <p className="mb-4 text-xs text-muted-foreground">
        Share this group and the servers in it with another team. You keep its
        settings, schedules, SSH key, and alerts. Moving a server out of the
        group ends the other team&apos;s access to it.
      </p>

      <div className="label mb-3">Shared with · {shareList.length}</div>
      {shareList.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {shares.isLoading ? 'Reading…' : 'Not shared with any team.'}
        </p>
      ) : (
        <ul className="space-y-3">
          {shareList.map((s) => (
            <li
              key={s.id}
              className="flex flex-wrap items-center gap-x-3 gap-y-2 font-mono text-xs"
            >
              <span className="min-w-0 truncate text-sm text-ink">
                {s.team}
              </span>
              <span className="ml-auto flex items-center gap-2">
                <Segmented
                  size="sm"
                  aria-label={`Access for ${s.team}`}
                  value={s.access}
                  onChange={(v) => update.mutate({ id: s.id, to: v })}
                  options={accessOptions}
                />
                <Button
                  size="xs"
                  variant="ghost"
                  className="hover:text-fail"
                  onClick={() => setStopping(s)}
                >
                  stop sharing
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      <ErrorBanner error={shares.error ?? update.error} className="mt-3" />

      <Separator className="my-5" />

      {created && (
        <div className="mb-5 space-y-3">
          <Notice tone="warn">
            Copy the link now. esa stores only a hash and cannot show it again.
          </Notice>
          <CopyBlock value={created.url} />
          <p className="text-xs text-muted-foreground">
            Send it to an owner or admin of the other team. They open it and
            pick which of their teams gets the group, with {created.link.access}{' '}
            access. It works once and expires{' '}
            <Until iso={created.link.expiresAt} />.
          </p>
        </div>
      )}
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Access" hint={accessHint[access]}>
          <Segmented
            aria-label="Access"
            value={access}
            onChange={setAccess}
            options={accessOptions}
          />
        </Field>
        <ErrorBanner error={create.error} />
        <Button
          type="submit"
          variant="outline"
          busy={create.isPending}
          className="w-full sm:w-auto"
        >
          <LinkIcon /> Create share link
        </Button>
      </form>

      {linkList.length > 0 && (
        <>
          <div className="label mt-5 mb-3">
            Pending links · {linkList.length}
          </div>
          <ul className="space-y-3">
            {linkList.map((l) => (
              <li
                key={l.id}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs"
              >
                <Badge variant={l.access === 'operate' ? 'amber' : 'outline'}>
                  {l.access}
                </Badge>
                <span className="text-faint">by {l.createdBy}</span>
                <span className="ml-auto flex items-center gap-2">
                  <span className="text-faint">
                    expires <Until iso={l.expiresAt} />
                  </span>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="hover:text-fail"
                    onClick={() => revoke.mutate(l.id)}
                    busy={revoke.isPending && revoke.variables === l.id}
                  >
                    revoke
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
      <ErrorBanner error={links.error ?? revoke.error} className="mt-3" />

      <ConfirmDialog
        open={!!stopping}
        title={`Stop sharing with ${stopping?.team ?? ''}`}
        danger
        body="That team loses access to this group and its servers right away. To share it again, send a new link."
        actionLabel="Stop"
        busy={stop.isPending}
        error={stop.error}
        onConfirm={() => stopping && stop.mutate(stopping.id)}
        onClose={() => {
          setStopping(null)
          stop.reset()
        }}
      />
    </Panel>
  )
}
