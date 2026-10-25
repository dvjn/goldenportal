import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Cell, Pie, PieChart } from "recharts";
import {
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { useMe } from "@/features/auth";
import { blueprints, type Property } from "@/features/blueprints";
import type { Facet } from "@/features/catalog";
import { type GroupData, widgetQueryOptions } from "@/features/widgets";
import { categorical, swatch } from "@/lib/colors";
import { type CatalogFilters, toCatalogFilters } from "./catalog-filter";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";

type Group = {
  key: string;
  label: string;
  count: number;
  color: string;
  dot: string;
  link?: CatalogFilters;
};

// GroupWidget counts entities by a property's values, as bars or a donut.
// Each value links to the catalog filtered to it.
export function GroupWidget({ source, widget, context }: WidgetProps) {
  const me = useMe();
  const data = useQuery(widgetQueryOptions<GroupData>(source, widget.identifier, context));
  const { data: bp } = useQuery(blueprints.queries.detail(widget.blueprint ?? ""));
  if (data.error) return <WidgetError widget={widget} error={data.error} />;
  if (!data.data) return <WidgetLoading widget={widget} height="h-32" />;

  const property = bp?.published?.spec.properties.find((p) => p.identifier === widget.groupBy);
  const base = toCatalogFilters(widget.filter, {
    entity: context.entity,
    me,
    filters: context.filters,
  });
  const groups = data.data.groups.map((facet, index) =>
    toGroup(
      facet,
      index,
      property,
      base && widget.groupBy && !context.draft && !base[widget.groupBy]
        ? { ...base, [widget.groupBy]: [facet.value as string] }
        : undefined,
    ),
  );
  if (data.data.none > 0) {
    groups.push({
      key: "none",
      label: "None",
      count: data.data.none,
      color: "var(--color-muted-foreground)",
      dot: "bg-muted-foreground/40",
    });
  }

  return (
    <WidgetCard widget={widget}>
      {groups.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing to count yet.</p>
      ) : widget.type === "donut" ? (
        <Donut groups={groups} blueprint={widget.blueprint ?? ""} />
      ) : (
        <Bars groups={groups} blueprint={widget.blueprint ?? ""} />
      )}
    </WidgetCard>
  );
}

function toGroup(
  facet: Facet,
  index: number,
  property: Property | undefined,
  link?: CatalogFilters,
): Group {
  const key = JSON.stringify(facet.value);
  const raw = typeof facet.value === "boolean" ? (facet.value ? "Yes" : "No") : String(facet.value);
  const color = swatch(
    property?.display?.colors?.[String(facet.value)] ?? categorical[index % categorical.length],
  );
  return {
    key,
    label: facet.title ?? raw,
    count: facet.count,
    color: color?.fill ?? "var(--color-muted-foreground)",
    dot: color?.dot ?? "bg-muted-foreground",
    link,
  };
}

function GroupLabel({ group, blueprint }: Readonly<{ group: Group; blueprint: string }>) {
  if (!group.link) return <span className="truncate">{group.label}</span>;
  return (
    <Link
      to="/catalog/$blueprint"
      params={{ blueprint }}
      search={{ view: "entities", f: group.link }}
      className="truncate hover:underline"
    >
      {group.label}
    </Link>
  );
}

function Bars({ groups, blueprint }: Readonly<{ groups: Group[]; blueprint: string }>) {
  const most = Math.max(...groups.map((group) => group.count));
  return (
    <ul className="grid gap-2.5">
      {groups.map((group) => (
        <li key={group.key} className="grid gap-1 text-sm">
          <span className="flex items-baseline justify-between gap-3">
            <GroupLabel group={group} blueprint={blueprint} />
            <span className="text-xs text-muted-foreground tabular-nums">{group.count}</span>
          </span>
          <span className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
            <span
              className={`block h-full rounded-full ${group.key === "none" ? "bg-muted-foreground/40" : "bg-primary/70"}`}
              style={{ width: `${(group.count / most) * 100}%` }}
            />
          </span>
        </li>
      ))}
    </ul>
  );
}

function Donut({ groups, blueprint }: Readonly<{ groups: Group[]; blueprint: string }>) {
  const total = groups.reduce((sum, group) => sum + group.count, 0);
  const config: ChartConfig = Object.fromEntries(
    groups.map((group) => [group.key, { label: group.label, color: group.color }]),
  );
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-4">
      <div className="relative size-32 shrink-0">
        <ChartContainer config={config} className="aspect-square size-32">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
            <Pie
              data={groups}
              dataKey="count"
              nameKey="key"
              innerRadius={40}
              outerRadius={62}
              strokeWidth={2}
              isAnimationActive={false}
            >
              {groups.map((group) => (
                <Cell key={group.key} fill={group.color} />
              ))}
            </Pie>
          </PieChart>
        </ChartContainer>
        <span className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-xl font-semibold tabular-nums">{total}</span>
        </span>
      </div>
      <ul className="grid min-w-36 flex-1 gap-1.5 text-sm">
        {groups.map((group) => (
          <li key={group.key} className="flex items-center justify-between gap-3">
            <span className="flex min-w-0 items-center gap-2">
              <span className={`size-2 shrink-0 rounded-full ${group.dot}`} aria-hidden />
              <GroupLabel group={group} blueprint={blueprint} />
            </span>
            <span className="text-xs text-muted-foreground tabular-nums">{group.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
