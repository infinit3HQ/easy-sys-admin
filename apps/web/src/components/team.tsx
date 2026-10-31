import { useNavigate, useRouter } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { Role } from '@esa/api'
import { api, call } from '#/lib/api'
import { ErrorBanner, Field } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card, CardContent } from '#/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '#/components/ui/dialog'
import { Input } from '#/components/ui/input'

/** What each role may do, in one line. */
export const roleText: Record<Role, string> = {
  owner: 'Everything, including members, invites, and deleting the team.',
  admin:
    'Servers, groups, runs, logs, and sharing. Can invite admins and viewers.',
  viewer: 'Read only. Cannot read logs.',
}

/**
 * Starts over after the session moved to another account: every cached
 * query belongs to the old one. `go` navigates first, so the page being left
 * is not reloaded against the new account.
 */
export function useSessionReset() {
  const qc = useQueryClient()
  const router = useRouter()
  return async (go: () => Promise<void>) => {
    qc.clear()
    await go()
    await router.invalidate()
  }
}

/** Asks for a name, creates the team, and switches the session to it. */
export function CreateTeamDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated?: () => void
}) {
  const navigate = useNavigate()
  const reset = useSessionReset()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: async () => {
      const m = await call(
        api().POST('/teams', { body: { name: name.trim() } }),
      )
      await call(api().PUT('/me/account', { body: { accountId: m.id } }))
      return m
    },
    onSuccess: async () => {
      onClose()
      onCreated?.()
      await reset(() => navigate({ to: '/app/team' }))
    },
  })
  const { reset: resetCreate } = create
  useEffect(() => {
    if (open) {
      setName('')
      resetCreate()
    }
  }, [open, resetCreate])

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) create.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>Create a team</DialogTitle>
            <DialogDescription className="text-ink-2">
              You become its owner. Invite people from the team page once it
              exists.
            </DialogDescription>
          </DialogHeader>
          <Field label="Name">
            <Input
              autoFocus
              autoComplete="off"
              maxLength={100}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="ops"
            />
          </Field>
          <ErrorBanner error={create.error} />
          <DialogFooter className="pt-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              busy={create.isPending}
              disabled={!name.trim()}
            >
              Create team
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** The frame for pages opened from a link: invites and share links. */
export function LinkShell({
  kicker,
  title,
  children,
}: {
  kicker: string
  title: ReactNode
  children: ReactNode
}) {
  return (
    <div className="grid min-h-dvh place-items-center px-4 py-10 sm:px-6">
      <div className="w-full max-w-md animate-rise">
        <a href="/app" className="mb-8 flex items-center gap-2.5">
          <img src="/favicon.svg" alt="" className="size-7" />
          <span className="font-display text-2xl font-bold tracking-wide uppercase">
            easy-sys-admin
          </span>
        </a>
        <Card>
          <CardContent className="p-5 sm:p-6">
            <div className="label mb-1.5">{kicker}</div>
            <h1 className="font-display text-4xl leading-[0.95] font-bold break-words uppercase">
              {title}
            </h1>
            <div className="mt-5 space-y-4">{children}</div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
