import { Cell, Pie, PieChart } from "recharts";
import { EmptyValue, exactTitle } from "@/components/catalog/property-value";
import { EmptyState } from "@/components/empty-state";
import {
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import type { Facet } from "@/features/catalog";
import type { GroupData, LinkData, Reading } from "@/features/widgets";
import { categorical, swatch } from "@/lib/colors";
import { formatPercent } from "@/lib/format";
import { formatReading, isShare } from "./reading";
import { ViewAll, WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { WidgetLink } from "./widget-link";

type Group = {
  key: string;
  label: string;
  count: number;
  color: string;
  dot: string;
  measure?: Reading;
  link?: LinkData;
};

// GroupWidget counts entities by a property's values, as bars, a donut, or
// one bar split into shares; bars can show a measure of each group instead.
// A link to each group opens its entities; any other link is the card's.
export function GroupWidget({ widget, context, data, error }: WidgetProps<GroupData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-32" />;

  const live = !context.draft;
  const groups = data.groups.map((facet, index) =>
    toGroup(facet, index, live ? facet.link : undefined),
  );
  if (data.none > 0) {
    groups.push({
      key: "none",
      label: "None",
      count: data.none,
      color: "var(--color-muted-foreground)",
      dot: "bg-muted-foreground/40",
      measure: data.noneMeasure,
      link: live ? data.noneLink : undefined,
    });
  }

  return (
    <WidgetCard widget={widget} action={live && <ViewAll link={data.link} />}>
      {groups.length === 0 ? (
        <EmptyState compact title="No groups yet" />
      ) : widget.type === "donut" ? (
        <Donut groups={groups} />
      ) : widget.type === "split" ? (
        <Split groups={groups} />
      ) : widget.measure ? (
        <MeasureBars groups={groups} unit={data.unit} />
      ) : (
        <Bars groups={groups} />
      )}
    </WidgetCard>
  );
}

// A group without a color, its own or its property's for its value, takes
// the next of the categorical colors.
function toGroup(
  facet: Facet & { color?: string; measure?: Reading },
  index: number,
  link?: LinkData,
): Group {
  const key = JSON.stringify(facet.value);
  const raw = typeof facet.value === "boolean" ? (facet.value ? "Yes" : "No") : String(facet.value);
  const color = swatch(facet.color ?? categorical[index % categorical.length]);
  return {
    key,
    label: facet.title ?? raw,
    count: facet.count,
    color: color?.fill ?? "var(--color-muted-foreground)",
    dot: color?.dot ?? "bg-muted-foreground",
    measure: facet.measure,
    link,
  };
}

function GroupLabel({ group }: Readonly<{ group: Group }>) {
  return (
    <span className="truncate">
      <WidgetLink link={group.link} className="hover:underline">
        {group.label}
      </WidgetLink>
    </span>
  );
}

function Bars({ groups }: Readonly<{ groups: Group[] }>) {
  const most = Math.max(...groups.map((group) => group.count));
  return (
    <ul className="grid gap-2.5">
      {groups.map((group) => (
        <li key={group.key} className="grid gap-1 text-sm">
          <span className="flex items-baseline justify-between gap-3">
            <GroupLabel group={group} />
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

// MeasureBars show each group's measure in the order the server sorted
// them. A share's bar runs to 100% and says how many of how many beside
// it; any other measure's runs to the largest. A group the measure has
// nothing to work on shows none.
function MeasureBars({ groups, unit }: Readonly<{ groups: Group[]; unit?: string }>) {
  const values = groups.map((group) => group.measure?.value ?? 0);
  const most = Math.max(...values, 0);
  return (
    <ul className="grid gap-2.5">
      {groups.map((group) => {
        const measure = group.measure ?? { value: null };
        const share = isShare(measure);
        const width =
          measure.value === null
            ? 0
            : share
              ? measure.value
              : most
                ? (measure.value / most) * 100
                : 0;
        return (
          <li key={group.key} className="grid gap-1 text-sm">
            <span className="flex items-baseline justify-between gap-3">
              <GroupLabel group={group} />
              <span className="flex shrink-0 items-baseline gap-2 tabular-nums">
                {measure.value === null ? (
                  <EmptyValue title="Nothing to measure" />
                ) : (
                  <span className="font-medium" title={exactTitle(measure.value, unit)}>
                    {formatReading(measure, unit)}
                  </span>
                )}
                {share && (
                  <span className="text-xs text-muted-foreground">
                    {measure.count} of {measure.total}
                  </span>
                )}
              </span>
            </span>
            <span className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
              <span
                className={`block h-full rounded-full ${group.key === "none" ? "bg-muted-foreground/40" : "bg-primary/70"}`}
                style={{ width: `${width}%` }}
              />
            </span>
          </li>
        );
      })}
    </ul>
  );
}

// Split is one bar of the groups' shares, in the order the server gives,
// which for a property with listed values is theirs, with a legend of
// counts and shares, as a scorecard's tiers are shown.
function Split({ groups }: Readonly<{ groups: Group[] }>) {
  const total = groups.reduce((sum, group) => sum + group.count, 0);
  const share = (group: Group) => formatPercent(group.count, total);
  return (
    <div className="grid gap-2">
      <div
        className="flex h-2.5 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label="Shares"
      >
        {groups.map((group) => (
          <span
            key={group.key}
            className="h-full"
            style={{ width: `${(group.count / total) * 100}%`, backgroundColor: group.color }}
          />
        ))}
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {groups.map((group) => (
          <li key={group.key} className="flex min-w-0 items-center gap-1.5">
            <span className={`size-2 shrink-0 rounded-full ${group.dot}`} aria-hidden />
            <GroupLabel group={group} />
            <span className="text-muted-foreground tabular-nums">{group.count}</span>
            <span className="text-xs text-muted-foreground tabular-nums">{share(group)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Donut({ groups }: Readonly<{ groups: Group[] }>) {
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
              <GroupLabel group={group} />
            </span>
            <span className="text-xs text-muted-foreground tabular-nums">{group.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
