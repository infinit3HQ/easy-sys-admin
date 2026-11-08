import type { ReactNode } from 'react'
import { column, SiteFrame } from '#/components/site-frame'
import { cn } from '#/lib/utils'

type Block =
  | { kind: 'meta'; text: string; draft: boolean }
  | { kind: 'h1'; text: string }
  | { kind: 'h2'; text: string; id: string }
  | { kind: 'p'; text: string; short: boolean; draft: boolean }
  | { kind: 'ul'; items: string[] }
  | { kind: 'table'; headers: string[]; rows: string[][] }

function slug(text: string): string {
  const body = text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
  // A leading digit is an invalid CSS id selector, so the contents links
  // would not be able to address the heading.
  return `s-${body}`
}

function rewrite(href: string): string {
  if (href === 'PRIVACY.md') return '/privacy'
  if (href === 'TERMS.md') return '/terms'
  return href
}

/** Inline marks used by the legal drafts: bold, emphasis, code, and links. */
function inline(text: string): ReactNode[] {
  const pattern =
    /(\*\*[^*]+\*\*|(?<![A-Za-z0-9])_[^_\n]+_(?![A-Za-z0-9])|`[^`]+`|\[[^\]]+\]\([^)]+\))/g
  const nodes: ReactNode[] = []
  let last = 0
  let key = 0
  for (const match of text.matchAll(pattern)) {
    const index = match.index
    if (index > last) nodes.push(text.slice(last, index))
    const token = match[0]
    if (token.startsWith('**')) {
      nodes.push(
        <strong key={key} className="font-medium text-ink">
          {token.slice(2, -2)}
        </strong>,
      )
    } else if (token.startsWith('_')) {
      nodes.push(<em key={key}>{token.slice(1, -1)}</em>)
    } else if (token.startsWith('`')) {
      nodes.push(
        <code key={key} className="font-mono text-[0.92em] text-ink">
          {token.slice(1, -1)}
        </code>,
      )
    } else {
      const linked = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token)
      if (linked) {
        const href = rewrite(linked[2] ?? '')
        const external = href.startsWith('http')
        nodes.push(
          <a
            key={key}
            href={href}
            className="text-amber underline decoration-amber/40 underline-offset-2 hover:decoration-amber"
            {...(external ? { rel: 'noreferrer' } : {})}
          >
            {linked[1]}
          </a>,
        )
      }
    }
    key += 1
    last = index + token.length
  }
  if (last < text.length) nodes.push(text.slice(last))
  return nodes
}

function parse(markdown: string): Block[] {
  const lines = markdown.replace(/\r\n/g, '\n').split('\n')
  const blocks: Block[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i] ?? ''
    if (line.trim() === '') {
      i += 1
      continue
    }
    if (line.startsWith('# ')) {
      blocks.push({ kind: 'h1', text: line.slice(2).trim() })
      i += 1
      continue
    }
    if (line.startsWith('## ')) {
      const text = line.slice(3).trim()
      blocks.push({ kind: 'h2', text, id: slug(text) })
      i += 1
      continue
    }
    if (line.startsWith('|')) {
      const rows: string[][] = []
      while (i < lines.length && (lines[i] ?? '').startsWith('|')) {
        const cells = (lines[i] ?? '')
          .split('|')
          .slice(1, -1)
          .map((cell) => cell.trim())
        if (!cells.every((cell) => /^:?-+:?$/.test(cell))) rows.push(cells)
        i += 1
      }
      const [headers, ...body] = rows
      if (headers) blocks.push({ kind: 'table', headers, rows: body })
      continue
    }
    if (line.startsWith('- ')) {
      const items: string[] = []
      while (i < lines.length && (lines[i] ?? '').startsWith('- ')) {
        items.push((lines[i] ?? '').slice(2).trim())
        i += 1
      }
      blocks.push({ kind: 'ul', items })
      continue
    }
    const paragraph = [line.trim()]
    i += 1
    while (
      i < lines.length &&
      (lines[i] ?? '').trim() !== '' &&
      !(lines[i] ?? '').startsWith('#') &&
      !(lines[i] ?? '').startsWith('|') &&
      !(lines[i] ?? '').startsWith('- ')
    ) {
      paragraph.push((lines[i] ?? '').trim())
      i += 1
    }
    const text = paragraph.join(' ')
    if (text.startsWith('_') && text.endsWith('_')) {
      const inner = text.slice(1, -1)
      blocks.push({ kind: 'meta', text: inner, draft: inner.includes('DRAFT') })
      continue
    }
    blocks.push({
      kind: 'p',
      text,
      short: text.includes('The short version:'),
      draft: text.includes('DRAFT: pending lawyer review'),
    })
  }
  return blocks
}

function Contents({
  headings,
}: {
  headings: Array<{ id: string; text: string }>
}) {
  return (
    <nav aria-label="Contents">
      <p className="label mb-3">Contents</p>
      <ol className="space-y-2 text-sm leading-snug text-ink-2">
        {headings.map((heading) => (
          <li key={heading.id}>
            <a href={`#${heading.id}`} className="hover:text-ink">
              {heading.text}
            </a>
          </li>
        ))}
      </ol>
    </nav>
  )
}

/** Renders a root legal markdown file with a contents list and a marked summary. */
export function LegalDocument({ source }: { source: string }) {
  const blocks = parse(source)
  const title = blocks.find((block) => block.kind === 'h1')
  const headings = blocks.flatMap((block) =>
    block.kind === 'h2' ? [{ id: block.id, text: block.text }] : [],
  )
  return (
    <SiteFrame>
      <div className={cn(column, 'py-10 sm:py-14')}>
        <div className="lg:grid lg:grid-cols-[15rem_minmax(0,42rem)] lg:justify-center lg:gap-14">
          <div className="mb-8 lg:sticky lg:top-20 lg:mb-0 lg:self-start">
            <Contents headings={headings} />
          </div>
          <article className="min-w-0">
            {title?.kind === 'h1' && (
              <h1 className="font-display text-4xl leading-[0.95] font-bold uppercase sm:text-5xl">
                {title.text}
              </h1>
            )}
            <div className="mt-6 space-y-5 text-sm leading-relaxed text-ink-2 sm:text-[15px]">
              {blocks.map((block, index) => {
                if (block.kind === 'meta') {
                  return (
                    <p
                      key={index}
                      className="font-mono text-xs text-muted-foreground"
                    >
                      {inline(block.text)}
                    </p>
                  )
                }
                if (block.kind === 'h2') {
                  return (
                    <h2
                      key={block.id}
                      id={block.id}
                      className="scroll-mt-20 pt-6 font-display text-2xl font-semibold tracking-wide text-ink uppercase"
                    >
                      {block.text}
                    </h2>
                  )
                }
                if (block.kind === 'p' && (block.short || block.draft)) {
                  return (
                    <p
                      key={index}
                      className={
                        block.draft
                          ? 'rounded-sm border border-amber/50 bg-amber/10 px-4 py-3 text-amber'
                          : 'border border-amber/60 bg-amber/10 px-4 py-3 text-ink'
                      }
                    >
                      {inline(block.text)}
                    </p>
                  )
                }
                if (block.kind === 'p')
                  return <p key={index}>{inline(block.text)}</p>
                if (block.kind === 'ul') {
                  return (
                    <ul key={index} className="list-disc space-y-2 pl-5">
                      {block.items.map((item) => (
                        <li key={item}>{inline(item)}</li>
                      ))}
                    </ul>
                  )
                }
                if (block.kind === 'table') {
                  return (
                    <div
                      key={index}
                      className="overflow-x-auto border border-line"
                    >
                      <table className="w-full min-w-[28rem] text-left text-sm">
                        <thead className="bg-panel font-mono text-[10px] tracking-wider text-faint uppercase">
                          <tr>
                            {block.headers.map((header) => (
                              <th
                                key={header}
                                className="px-3 py-2 font-medium"
                              >
                                {inline(header)}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {block.rows.map((row) => (
                            <tr
                              key={row.join('|')}
                              className="border-t border-line align-top"
                            >
                              {row.map((cell, cellIndex) => (
                                <td key={cellIndex} className="px-3 py-2">
                                  {inline(cell)}
                                </td>
                              ))}
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )
                }
                return null
              })}
            </div>
          </article>
        </div>
      </div>
    </SiteFrame>
  )
}
