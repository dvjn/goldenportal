import { queryOptions } from "@tanstack/react-query";
import type { EntityPage, Facet, Filter } from "@/features/catalog";
import { type ApiDataResponse, api } from "@/lib/api";

export type WidgetType =
  | "stat"
  | "bar"
  | "donut"
  | "table"
  | "scorecard"
  | "markdown"
  | "properties";

// Widget is one tile on a page: a dashboard on Home, or a tab a blueprint
// gives its catalog or entity pages.
export type Widget = {
  identifier: string;
  type: WidgetType;
  title?: string;
  description?: string;
  // width is how many of the 12 grid columns it spans.
  width: number;
  blueprint?: string;
  filter?: Filter;
  aggregate?: "count" | "sum" | "avg" | "min" | "max";
  property?: string;
  unit?: string;
  groupBy?: string;
  columns?: string[];
  sort?: string;
  limit?: number;
  scorecard?: string;
  text?: string;
  properties?: string[];
};

// Tab is a page tab of widgets.
export type Tab = { identifier: string; title: string; icon?: string; widgets: Widget[] };

// WidgetSource is the document a widget is written in.
export type WidgetSource =
  | { dashboard: string }
  | { blueprint: string; page: "catalog" | "entity"; tab: string };

// WidgetContext is what a widget loads against: the entity whose page it
// is, and whether to load a builder's draft.
export type WidgetContext = { entity?: string; draft?: boolean };

export type StatData = { value: number | null };
// none counts the entities without a value.
export type GroupData = { groups: Facet[]; none: number };
export type TierData = { tiers: { name: string; count: number }[] };
export type WidgetData = StatData | GroupData | EntityPage | TierData;

function widgetPath(source: WidgetSource, widget: string) {
  const parts =
    "dashboard" in source
      ? ["dashboards", source.dashboard, "widgets", widget]
      : ["blueprints", source.blueprint, "widgets", source.page, source.tab, widget];
  return parts.map(encodeURIComponent).join("/");
}

export async function loadWidget<D extends WidgetData>(
  source: WidgetSource,
  widget: string,
  context: WidgetContext,
): Promise<D> {
  const searchParams: Record<string, string> = {};
  if (context.entity) searchParams.entity = context.entity;
  if (context.draft) searchParams.draft = "true";
  return (await api.get(widgetPath(source, widget), { searchParams }).json<ApiDataResponse<D>>())
    .data;
}

export const widgetQueryOptions = <D extends WidgetData>(
  source: WidgetSource,
  widget: string,
  context: WidgetContext,
) =>
  queryOptions({
    queryKey: ["widgets", source, widget, context] as const,
    queryFn: () => loadWidget<D>(source, widget, context),
  });
