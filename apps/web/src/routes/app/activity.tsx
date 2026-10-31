import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '#/lib/queries'
import { When } from '#/components/time'
import { Empty, ErrorBanner, Loading, PageHeader, cx } from '#/components/ui'

export const Route = createFileRoute('/app/activity')({
  head: () => ({ meta: [{ title: 'Activity · easy-sys-admin' }] }),
  component: Activity,
})

const sourceStyle: Record<string, string> = {
  web: 'text-ink-2',
  api: 'text-amber',
  mcp: 'text-ok',
  system: 'text-muted',
}

function Activity() {
  const r = useQuery(q.activity())
  return (
    <div>
      <PageHeader kicker="Audit log" title="Activity" />
      {r.isLoading ? (
        <Loading />
      ) : r.error ? (
        <ErrorBanner error={r.error} />
      ) : (r.data?.items ?? []).length === 0 ? (
        <Empty title="Nothing yet" />
      ) : (
        <div className="panel overflow-x-auto">
          <table className="w-full min-w-[720px] text-left font-mono text-xs">
            <thead>
              <tr className="border-b border-line">
                {['When', 'Who', 'Via', 'What', 'On', 'Detail'].map((h) => (
                  <th key={h} className="label px-3 py-2.5 font-normal">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {r.data!.items.map((e) => (
                <tr key={e.id} className="border-b border-line/60 align-top last:border-0">
                  <td className="whitespace-nowrap px-3 py-2 text-muted"><When iso={e.createdAt} /></td>
                  <td className="px-3 py-2 text-ink">{e.userName || '—'}</td>
                  <td className={cx('px-3 py-2 uppercase', sourceStyle[e.source])}>{e.source}</td>
                  <td className="px-3 py-2 text-ink">{e.action}</td>
                  <td className="px-3 py-2 text-ink-2">{e.target}</td>
                  <td className="max-w-md truncate px-3 py-2 text-faint" title={JSON.stringify(e.detail)}>
                    {Object.keys(e.detail).length ? JSON.stringify(e.detail) : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
