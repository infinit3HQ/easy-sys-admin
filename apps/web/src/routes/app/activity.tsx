import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { When } from '#/components/time'
import { Empty, ErrorBanner, Loading, PageHeader, cn } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'

export const Route = createFileRoute('/app/activity')({
  head: () => ({ meta: [{ title: 'Activity · easy-sys-admin' }] }),
  component: Activity,
})

const sourceStyle: Record<string, string> = {
  web: 'text-ink-2',
  api: 'text-amber',
  mcp: 'text-ok',
  system: 'text-muted-foreground',
}

function Activity() {
  const r = useQuery(q.activity())
  const items = r.data?.items ?? []
  const [downloading, setDownloading] = useState(false)
  const [downloadError, setDownloadError] = useState<unknown>(null)
  async function download() {
    setDownloading(true)
    setDownloadError(null)
    try {
      const data = await call(
        api().GET('/activity', { params: { query: { limit: 5000 } } }),
      )
      const blob = new Blob([JSON.stringify(data.items, null, 2)], {
        type: 'application/json',
      })
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = 'activity.json'
      a.click()
      URL.revokeObjectURL(a.href)
    } catch (e) {
      setDownloadError(e)
    } finally {
      setDownloading(false)
    }
  }
  return (
    <div>
      <PageHeader kicker="Audit log" title="Activity">
        <Button
          size="sm"
          variant="outline"
          busy={downloading}
          onClick={() => void download()}
        >
          Download
        </Button>
      </PageHeader>
      {r.data && (
        <p className="mb-4 text-sm text-muted-foreground">
          {r.data.keptUntilDeletion
            ? 'Rows are kept until this account is deleted.'
            : `Rows older than ${r.data.retentionHours} hours are deleted.`}{' '}
          {r.data.holdUntil && (
            <>
              A hold stops deletion until <When iso={r.data.holdUntil} />.{' '}
            </>
          )}
          {r.data.earliest ? (
            <>
              The oldest row still here is <When iso={r.data.earliest} />.
            </>
          ) : (
            'There are no rows yet.'
          )}{' '}
          The download is the newest 5000 rows.
        </p>
      )}
      {downloadError ? <ErrorBanner error={downloadError} className="mb-4" /> : null}
      {r.isLoading ? (
        <Loading />
      ) : r.error ? (
        <ErrorBanner error={r.error} />
      ) : items.length === 0 ? (
        <Empty title="Nothing yet" />
      ) : (
        <>
          <Card className="md:hidden">
            <ul className="divide-y divide-line/60 font-mono text-xs">
              {items.map((e) => (
                <li key={e.id} className="px-4 py-3">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="min-w-0 truncate text-ink">
                      {e.action}
                    </span>
                    <span
                      className={cn(
                        'shrink-0 text-[10px] uppercase',
                        sourceStyle[e.source],
                      )}
                    >
                      {e.source}
                    </span>
                  </div>
                  <div className="mt-1 truncate text-ink-2">{e.target}</div>
                  <div className="mt-1 flex items-baseline justify-between gap-3 text-[11px] text-muted-foreground">
                    <span className="truncate">{e.userName || '—'}</span>
                    <span className="shrink-0">
                      <When iso={e.createdAt} />
                    </span>
                  </div>
                  {Object.keys(e.detail).length > 0 && (
                    <div className="mt-1 line-clamp-2 text-[10px] break-all text-faint">
                      {JSON.stringify(e.detail)}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </Card>
          <Card className="hidden md:flex">
            <Table className="min-w-[720px] font-mono text-xs">
              <TableHeader>
                <TableRow>
                  {['When', 'Who', 'Via', 'What', 'On', 'Detail'].map(
                    (h, i) => (
                      <TableHead
                        key={h}
                        className={
                          i === 0 ? 'pl-4' : i === 5 ? 'pr-4' : undefined
                        }
                      >
                        {h}
                      </TableHead>
                    ),
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((e) => (
                  <TableRow key={e.id} className="align-top">
                    <TableCell className="py-2 pl-4 whitespace-nowrap text-muted-foreground">
                      <When iso={e.createdAt} />
                    </TableCell>
                    <TableCell className="py-2 text-ink">
                      {e.userName || '—'}
                    </TableCell>
                    <TableCell
                      className={cn('py-2 uppercase', sourceStyle[e.source])}
                    >
                      {e.source}
                    </TableCell>
                    <TableCell className="py-2 text-ink">{e.action}</TableCell>
                    <TableCell className="py-2 text-ink-2">
                      {e.target}
                    </TableCell>
                    <TableCell
                      className="max-w-md truncate py-2 pr-4 text-faint"
                      title={JSON.stringify(e.detail)}
                    >
                      {Object.keys(e.detail).length
                        ? JSON.stringify(e.detail)
                        : ''}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        </>
      )}
    </div>
  )
}
