import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { PricingCards } from '#/components/pricing'
import { column } from '#/components/site-frame'
import { docHref, docSections, rewriteDocHref } from '#/lib/docs'
import type { DocPage } from '#/lib/docs'
import { cn } from '#/lib/utils'

type Block =
  | { kind: 'h2'; text: string; id: string }
  | { kind: 'h3'; text: string; id: string }
  | { kind: 'p'; text: string }
  | { kind: 'ul'; items: string[] }
  | { kind: 'ol'; items: string[] }
  | { kind: 'table'; headers: string[]; rows: string[][] }
  | { kind: 'code'; text: string }
  | { kind: 'plans' }

function headingId(text: string): string {
  const body = text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
  return `s-${body}`
}

function inline(text: string): ReactNode[] {
  const pattern = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g
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
    } else if (token.startsWith('`')) {
      nodes.push(
        <code key={key} className="font-mono text-[0.92em] text-ink">
          {token.slice(1, -1)}
        </code>,
      )
    } else {
      const linked = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token)
      if (linked) {
        const href = rewriteDocHref(linked[2] ?? '')
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

function isFence(line: string): boolean {
  return line.startsWith('```')
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
    if (line.trim() === '<!-- plans -->') {
      blocks.push({ kind: 'plans' })
      i += 1
      continue
    }
    if (isFence(line)) {
      const rows: string[] = []
      i += 1
      while (i < lines.length && !isFence(lines[i] ?? '')) {
        rows.push(lines[i] ?? '')
        i += 1
      }
      if (i < lines.length) i += 1
      blocks.push({ kind: 'code', text: rows.join('\n') })
      continue
    }
    if (line.startsWith('### ')) {
      const text = line.slice(4).trim()
      blocks.push({ kind: 'h3', text, id: headingId(text) })
      i += 1
      continue
    }
    if (line.startsWith('## ')) {
      const text = line.slice(3).trim()
      blocks.push({ kind: 'h2', text, id: headingId(text) })
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
    if (/^\d+\. /.test(line)) {
      const items: string[] = []
      while (i < lines.length && /^\d+\. /.test(lines[i] ?? '')) {
        items.push((lines[i] ?? '').replace(/^\d+\. /, '').trim())
        i += 1
      }
      blocks.push({ kind: 'ol', items })
      continue
    }
    const paragraph = [line.trim()]
    i += 1
    while (i < lines.length && (lines[i] ?? '').trim() !== '') {
      const next = lines[i] ?? ''
      if (
        next.startsWith('#') ||
        next.startsWith('|') ||
        next.startsWith('- ') ||
        isFence(next) ||
        /^\d+\. /.test(next) ||
        next.trim() === '<!-- plans -->'
      ) {
        break
      }
      paragraph.push(next.trim())
      i += 1
    }
    blocks.push({ kind: 'p', text: paragraph.join(' ') })
  }
  return blocks
}

function DocNav({ slug }: { slug: string }) {
  return (
    <nav aria-label="Documentation" className="space-y-6">
      {docSections.map((section) => (
        <div key={section.label}>
          <p className="label mb-2">{section.label}</p>
          <ul className="space-y-1.5 text-sm">
            {section.items.map((item) => {
              const current = item.slug === slug
              return (
                <li key={item.slug || 'index'}>
                  {item.slug === '' ? (
                    <Link
                      to="/docs"
                      activeOptions={{ exact: true }}
                      aria-current={current ? 'page' : undefined}
                      className={cn(
                        'hover:text-ink',
                        current ? 'text-ink' : 'text-ink-2',
                      )}
                    >
                      {item.label}
                    </Link>
                  ) : (
                    <Link
                      to="/docs/$page"
                      params={{ page: item.slug }}
                      aria-current={current ? 'page' : undefined}
                      className={cn(
                        'hover:text-ink',
                        current ? 'text-ink' : 'text-ink-2',
                      )}
                    >
                      {item.label}
                    </Link>
                  )}
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

function BlockView({ block }: { block: Block }) {
  if (block.kind === 'plans') return <PricingCards idPrefix="docs-plan" />
  if (block.kind === 'h2') {
    return (
      <h2
        id={block.id}
        className="scroll-mt-20 pt-6 font-display text-2xl font-semibold tracking-wide text-ink uppercase"
      >
        {block.text}
      </h2>
    )
  }
  if (block.kind === 'h3') {
    return (
      <h3
        id={block.id}
        className="scroll-mt-20 pt-4 font-display text-lg font-semibold tracking-wide text-ink uppercase"
      >
        {block.text}
      </h3>
    )
  }
  if (block.kind === 'p') return <p>{inline(block.text)}</p>
  if (block.kind === 'ul') {
    return (
      <ul className="list-disc space-y-2 pl-5">
        {block.items.map((item, index) => (
          <li key={index}>{inline(item)}</li>
        ))}
      </ul>
    )
  }
  if (block.kind === 'ol') {
    return (
      <ol className="list-decimal space-y-2 pl-5">
        {block.items.map((item, index) => (
          <li key={index}>{inline(item)}</li>
        ))}
      </ol>
    )
  }
  if (block.kind === 'code') {
    return (
      <pre className="overflow-x-auto rounded-md border border-line bg-panel p-3 font-mono text-xs leading-relaxed text-ink">
        <code>{block.text}</code>
      </pre>
    )
  }
  return (
    <div className="overflow-x-auto rounded-md border border-line">
      <table className="w-full min-w-[28rem] text-left text-sm">
        <thead className="bg-panel font-mono text-[10px] tracking-wider text-faint uppercase">
          <tr>
            {block.headers.map((header) => (
              <th key={header} className="px-3 py-2 font-medium">
                {inline(header)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {block.rows.map((row, rowIndex) => (
            <tr key={rowIndex} className="border-t border-line align-top">
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

export function DocArticle({ page }: { page: DocPage }) {
  const blocks = parse(page.body)
  return (
    <div className={cn(column, 'py-10 sm:py-14')}>
      <div className="lg:grid lg:grid-cols-[14rem_minmax(0,46rem)] lg:justify-center lg:gap-12">
        <div className="mb-10 lg:sticky lg:top-20 lg:mb-0 lg:self-start">
          <DocNav slug={page.slug} />
        </div>
        <article className="min-w-0">
          <p className="label mb-4 !text-amber">Documentation</p>
          <h1 className="font-display text-4xl leading-[0.95] font-bold uppercase sm:text-5xl">
            {page.title}
          </h1>
          <p className="mt-4 text-base leading-relaxed text-ink-2">
            {page.description}
          </p>
          <div className="mt-8 space-y-4 text-sm leading-relaxed text-ink-2 sm:text-[15px]">
            {blocks.map((block, index) => (
              <BlockView key={index} block={block} />
            ))}
          </div>
        </article>
      </div>
    </div>
  )
}

export function docPageHead(
  page: DocPage,
  seo: boolean,
  origin: string,
): {
  meta: Array<Record<string, string>>
  links: Array<{ rel: string; href: string }>
} {
  const path = docHref(page.slug)
  const title = `${page.title} · easy-sys-admin`
  return {
    meta: [
      { title },
      { name: 'description', content: page.description },
      { name: 'robots', content: seo ? 'index, follow' : 'noindex, nofollow' },
      ...(seo
        ? [
            { property: 'og:title', content: title },
            { property: 'og:description', content: page.description },
            { property: 'og:type', content: 'article' },
            { property: 'og:url', content: origin ? `${origin}${path}` : path },
          ]
        : []),
    ],
    links:
      seo && origin ? [{ rel: 'canonical', href: `${origin}${path}` }] : [],
  }
}
