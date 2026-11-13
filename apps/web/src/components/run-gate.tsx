import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Run } from '@esa/api'
import { api, call } from '#/lib/api'
import { ErrorBanner, Field, Notice } from '#/components/kit'
import { When } from '#/components/time'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'

const decided: Record<string, string> = {
  continued: 'Continued',
  stopped: 'Stopped',
  expired: 'Stopped because nobody answered within 24 hours',
}

/** The canary gate of a run: waiting, or what was decided and by whom. */
export function RunGate({ run, canAct }: { run: Run; canAct: boolean }) {
  const qc = useQueryClient()
  const [note, setNote] = useState('')
  const decide = useMutation({
    mutationFn: (decision: 'continue' | 'stop') =>
      call(
        api().POST('/runs/{runId}/gate', {
          params: { path: { runId: run.id } },
          body: { decision, note: note.trim() },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['run', run.id] }),
  })
  const g = run.gate
  if (!g) return null
  if (g.state !== 'waiting') {
    return (
      <Notice className="mb-6" title="Canary gate">
        {decided[g.state] ?? g.state}
        {g.by ? ` by ${g.by}` : ''}
        {g.note ? `: ${g.note}` : '.'}
      </Notice>
    )
  }
  return (
    <Notice tone="warn" className="mb-6" title="Waiting at the canary gate">
      <p>
        The canaries passed
        {g.at ? (
          <>
            {' '}
            at <When iso={g.at} />
          </>
        ) : (
          ''
        )}
        . The other servers start only when a person continues. Check the
        canaries first.
      </p>
      {canAct ? (
        <form
          className="mt-3 grid gap-2 sm:max-w-md"
          onSubmit={(e) => e.preventDefault()}
        >
          <Field label="Note (optional)">
            <Input
              value={note}
              maxLength={200}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
          <div className="flex gap-2">
            <Button
              type="button"
              busy={decide.isPending && decide.variables === 'continue'}
              onClick={() => decide.mutate('continue')}
            >
              Continue
            </Button>
            <Button
              type="button"
              variant="outline"
              busy={decide.isPending && decide.variables === 'stop'}
              onClick={() => decide.mutate('stop')}
            >
              Stop the rest
            </Button>
          </div>
          <ErrorBanner error={decide.error} />
        </form>
      ) : (
        <p className="mt-2 text-xs">An admin of this space decides.</p>
      )}
    </Notice>
  )
}
