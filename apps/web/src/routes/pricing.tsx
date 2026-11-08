import { createFileRoute } from '@tanstack/react-router'
import { PricingSection } from '#/components/pricing'
import { column, SiteFrame } from '#/components/site-frame'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

const description =
  'Plans for Easy Sys Admin. The public beta is free. Self-Hosted is free to run yourself. Managed Cloud is coming soon. Nothing is charged.'

export const Route = createFileRoute('/pricing')({
  loader: () => loadSeo(),
  head: ({ loaderData }) => {
    const origin = siteOrigin()
    const seo = loaderData === true
    return {
      meta: [
        { title: 'Plans · easy-sys-admin' },
        { name: 'description', content: description },
        {
          name: 'robots',
          content: seo ? 'index, follow' : 'noindex, nofollow',
        },
        ...(seo
          ? [
              { property: 'og:title', content: 'Plans · easy-sys-admin' },
              { property: 'og:description', content: description },
              { property: 'og:type', content: 'website' },
              {
                property: 'og:url',
                content: origin ? `${origin}/pricing` : '/pricing',
              },
            ]
          : []),
      ],
      links:
        seo && origin ? [{ rel: 'canonical', href: `${origin}/pricing` }] : [],
    }
  },
  component: PricingPage,
})

function PricingPage() {
  return (
    <SiteFrame>
      <div className={column + ' py-14 sm:py-20'}>
        <p className="label mb-4 !text-amber">Pricing</p>
        <PricingSection id="plans" />
      </div>
    </SiteFrame>
  )
}
