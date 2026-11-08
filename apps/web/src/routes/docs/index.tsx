import { createFileRoute } from '@tanstack/react-router'
import { DocArticle, docPageHead } from '#/components/doc-page'
import { SiteFrame } from '#/components/site-frame'
import { getDoc } from '#/lib/docs'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

export const Route = createFileRoute('/docs/')({
  loader: async () => {
    const page = getDoc('')
    if (!page) throw new Error('docs index is missing')
    return { page, seo: await loadSeo() }
  },
  head: ({ loaderData }) => {
    if (!loaderData?.page) return {}
    return docPageHead(loaderData.page, loaderData.seo === true, siteOrigin())
  },
  component: DocsIndex,
})

function DocsIndex() {
  const { page } = Route.useLoaderData()
  return (
    <SiteFrame>
      <DocArticle page={page} />
    </SiteFrame>
  )
}
