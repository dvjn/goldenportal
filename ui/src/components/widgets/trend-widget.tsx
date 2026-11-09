import { useNavigate } from "@tanstack/react-router";
import { Bar, BarChart, CartesianGrid, Line, LineChart, XAxis } from "recharts";
import { EmptyValue, exactTitle } from "@/components/catalog/property-value";
import {
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import type { LinkData, TrendData, TrendDay } from "@/features/widgets";
import { toCatalogLink } from "./catalog-filter";
import { formatReading, isShare, summaryOp } from "./reading";
import { ViewAll, WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";
import { linkable, WidgetLink } from "./widget-link";

// Days are UTC days, so they're written in UTC to stay on their day.
const shortDay = new Intl.DateTimeFormat("en", { month: "short", day: "numeric", timeZone: "UTC" });
const longDay = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

const formatDay = (day: string, format = shortDay) => format.format(new Date(`${day}T00:00:00Z`));

// TrendWidget shows a measure for each day of a range, with the measure
// over the whole range above it. Counts and sums add up across days, so
// they're bars; averages, percentiles and shares don't, so they're a line
// that breaks on days with nothing to measure. With a link to each day,
// clicking a bar opens that day's entities.
export function TrendWidget({ widget, context, data, error }: WidgetProps<TrendData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-40" />;

  const live = !context.draft;
  const op = summaryOp(widget.measure);
  const additive = op === "count" || op === "sum";
  const all = formatReading(data.all, data.unit);
  return (
    <WidgetCard widget={widget} action={live && <ViewAll link={data.link} />}>
      <div className="grid gap-3">
        <div className="flex items-baseline gap-2 tabular-nums">
          <span
            className="text-2xl font-semibold tracking-tight"
            title={exactTitle(data.all.value, data.unit)}
          >
            {all ?? <EmptyValue title="Nothing to measure" />}
          </span>
          {isShare(data.all) && (
            <span className="text-xs text-muted-foreground">
              {data.all.count} of {data.all.total}
            </span>
          )}
        </div>
        <TrendChart
          days={data.days}
          unit={data.unit}
          label={widget.title ?? ""}
          additive={additive}
          live={live}
        />
        <ul className="sr-only">
          {data.days.map((day) => (
            <li key={day.day}>
              <WidgetLink link={live ? day.link : undefined}>
                {formatDay(day.day, longDay)}
              </WidgetLink>
              {`: ${formatReading(day, data.unit) ?? "nothing to measure"}`}
            </li>
          ))}
        </ul>
      </div>
    </WidgetCard>
  );
}

function TrendChart({
  days,
  unit,
  label,
  additive,
  live,
}: Readonly<{ days: TrendDay[]; unit?: string; label: string; additive: boolean; live: boolean }>) {
  const navigate = useNavigate();
  const open = (link: LinkData | undefined) => {
    if (!live || !linkable(link) || !link.blueprint) return;
    void navigate({
      to: "/catalog/$blueprint",
      params: { blueprint: link.blueprint },
      search: { view: "entities", ...toCatalogLink(link.filter), sort: link.sort },
    });
  };
  const config: ChartConfig = { value: { label, color: "var(--color-primary)" } };
  const points = days.map((day) => ({ ...day, label: formatDay(day.day) }));
  const tooltip = (
    <ChartTooltip
      cursor={additive ? { fill: "var(--color-muted)" } : true}
      content={
        <ChartTooltipContent
          labelFormatter={(_, payload) => {
            const day = payload?.[0]?.payload as TrendDay | undefined;
            return day ? formatDay(day.day, longDay) : "";
          }}
          formatter={(_, __, item) => {
            const day = item.payload as TrendDay;
            return (
              <span className="font-medium tabular-nums" title={exactTitle(day.value, unit)}>
                {formatReading(day, unit) ?? "Nothing to measure"}
                {isShare(day) && (
                  <span className="ml-2 text-muted-foreground">
                    {day.count} of {day.total}
                  </span>
                )}
              </span>
            );
          }}
        />
      }
    />
  );
  const axis = (
    <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
  );
  return (
    <ChartContainer config={config} className="aspect-auto h-36 w-full">
      {additive ? (
        <BarChart data={points} margin={{ left: 0, right: 0 }}>
          <CartesianGrid vertical={false} />
          {axis}
          {tooltip}
          <Bar
            dataKey="value"
            fill="var(--color-value)"
            radius={2}
            isAnimationActive={false}
            className={live ? "cursor-pointer" : undefined}
            onClick={(bar) => open((bar.payload as TrendDay).link)}
          />
        </BarChart>
      ) : (
        <LineChart data={points} margin={{ left: 8, right: 8 }}>
          <CartesianGrid vertical={false} />
          {axis}
          {tooltip}
          <Line
            dataKey="value"
            type="monotone"
            stroke="var(--color-value)"
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      )}
    </ChartContainer>
  );
}
