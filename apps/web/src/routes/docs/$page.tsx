import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocArticle, docPageHead } from '#/components/doc-page'
import { SiteFrame } from '#/components/site-frame'
import { getDoc } from '#/lib/docs'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

export const Route = createFileRoute('/docs/$page')({
  loader: async ({ params }) => {
    const page = getDoc(params.page)
    if (!page) throw notFound()
    return { page, seo: await loadSeo() }
  },
  head: ({ loaderData }) => {
    if (!loaderData?.page) return {}
    return docPageHead(loaderData.page, loaderData.seo === true, siteOrigin())
  },
  component: DocsPage,
})

function DocsPage() {
  const { page } = Route.useLoaderData()
  return (
    <SiteFrame>
      <DocArticle page={page} />
    </SiteFrame>
  )
}
