import { cn } from "cn";
import type { ReactNode } from "react";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EmptyValue, formatNumber } from "@/components/catalog/property-value";
import { interactiveCard, stretchedLink } from "@/components/link-card";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import type { LinkData, StatData, Widget } from "@/features/widgets";
import { formatPercent } from "@/lib/format";
import { WidgetError, type WidgetProps, WidgetTitle } from "./widget-grid";
import { linkable, WidgetLink } from "./widget-link";

// StatWidget shows one number, worked out from a query's entities, in the
// unit the server settled on, and a share how many of how many it is.
export function StatWidget({ widget, context, data, error }: WidgetProps<StatData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) {
    return (
      <StatCard widget={widget}>
        <Skeleton className="h-8 w-12" aria-hidden />
        <span className="sr-only">Loading</span>
      </StatCard>
    );
  }

  const link = context.draft ? undefined : data.link;
  return (
    <StatCard widget={widget} link={link}>
      <span className="text-2xl font-semibold tracking-tight tabular-nums">
        {data.value === null ? (
          <EmptyValue title="Nothing to measure" />
        ) : data.unit === "%" && data.count !== undefined && data.total ? (
          formatPercent(data.count, data.total)
        ) : (
          formatNumber(data.value, data.unit)
        )}
      </span>
      {data.count !== undefined && data.total !== undefined && (
        <span className="truncate text-xs text-muted-foreground tabular-nums">
          {data.count} of {data.total}
        </span>
      )}
    </StatCard>
  );
}

// StatCard is a stat's compact frame: its icon, if it has one, beside its
// title, number and, for a share, how many of how many on a line of its
// own, which narrow cards would otherwise wrap. The icon sits by the title,
// so icons line up across a row whether or not a card has that line. A card
// too narrow for both, as on a zoomed-in page, shrinks the icon rather than
// cutting the number short. With a link, the whole card is the link.
function StatCard({
  widget,
  link,
  children,
}: Readonly<{ widget: Widget; link?: LinkData; children: ReactNode }>) {
  return (
    <Card size="sm" className={cn("@container px-4", linkable(link) && interactiveCard)}>
      <div className="flex items-start gap-3">
        {widget.icon && <IconTile icon={widget.icon} color={widget.color} size="stat" />}
        <div className="grid min-w-0 gap-0.5">
          <span className="truncate text-sm text-muted-foreground">
            <WidgetLink link={link} className={stretchedLink}>
              <WidgetTitle widget={widget} />
            </WidgetLink>
          </span>
          {children}
        </div>
      </div>
    </Card>
  );
}
