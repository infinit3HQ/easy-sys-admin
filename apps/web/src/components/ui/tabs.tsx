import * as React from 'react'
import { Tabs as TabsPrimitive } from 'radix-ui'
import { cn } from '#/lib/utils'

function Tabs({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn('flex min-w-0 flex-col gap-6', className)} {...props} />
}

/** An underlined tab strip. It scrolls sideways on narrow screens instead of wrapping. */
function TabsList({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn('scrollbar-none -mx-4 flex items-stretch gap-1 overflow-x-auto border-b px-4 sm:mx-0 sm:px-0', className)}
      {...props}
    />
  )
}

function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        '-mb-px inline-flex shrink-0 cursor-pointer items-center gap-1.5 border-b-2 border-transparent px-3 py-2.5 font-mono text-[11px] tracking-wider whitespace-nowrap text-muted-foreground uppercase transition-colors outline-none hover:text-foreground focus-visible:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:border-amber data-[state=active]:text-amber [&_svg]:size-3.5 [&_svg]:shrink-0',
        className,
      )}
      {...props}
    />
  )
}

function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content data-slot="tabs-content" className={cn('min-w-0 flex-1 outline-none', className)} {...props} />
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
