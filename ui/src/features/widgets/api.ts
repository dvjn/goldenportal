import { queryOptions } from "@tanstack/react-query";
import type { Property } from "@/features/blueprints";
import type { Facet, Filter, Link } from "@/features/catalog";
import type { Tier } from "@/features/scorecards";
import { type ApiDataResponse, api } from "@/lib/api";

export type WidgetType =
  | "stat"
  | "bar"
  | "donut"
  | "table"
  | "scorecard"
  | "markdown"
  | "properties";

// Query picks entities: a blueprint's that match a filter. name is set for
// one of the page's named queries.
export type Query = { name?: string; blueprint: string; filter?: Filter };

// WidgetLink is what a widget, or each of its groups or rows, opens: the
// catalog with a query's entities, or a url or path template.
export type WidgetLink = {
  query?: Query;
  filter?: Filter;
  group?: boolean;
  url?: string;
  path?: string;
};

// Column is a table column: a field path, read in SQL, or a title and jq
// the server works out for each row.
export type Column = { path?: string; title?: string; jq?: string; unit?: string };

// Value is the number a stat shows from a query's entities: their count,
// unless it names a field to sum, average or take the least or most of,
// a condition whose share of them it is, or jq over them.
export type Value = {
  query: Query;
  sum?: string;
  avg?: string;
  min?: string;
  max?: string;
  share?: Filter;
  jq?: string;
  unit?: string;
};

// Widget is one tile on a page: a dashboard on Home, or a tab a blueprint
// gives its catalog or entity pages.
export type Widget = {
  identifier: string;
  type: WidgetType;
  title?: string;
  description?: string;
  // width is how many of the 12 grid columns it spans.
  width: number;
  query?: Query;
  value?: Value;
  groupBy?: string;
  columns?: Column[];
  sort?: string;
  limit?: number;
  scorecard?: string;
  text?: string;
  properties?: string[];
  // icon and color mark a stat widget's card; color tints the icon.
  icon?: string;
  color?: string;
  // link is what clicking the widget opens; without one it isn't a link.
  link?: WidgetLink;
  // rowLink is what a table's rows open instead of their entity's page.
  rowLink?: WidgetLink;
};

// Tab is a page tab of widgets.
export type Tab = { identifier: string; title: string; icon?: string; widgets: Widget[] };

// WidgetSource is the document a widget is written in.
export type WidgetSource =
  | { dashboard: string }
  | { blueprint: string; page: "catalog" | "entity"; tab: string };

// WidgetContext is what widgets load against: the entity whose page it
// is, what the dashboard's inputs are set to, and whether to load a
// builder's draft. An input is set to the values picked, to none when
// cleared to show everything, or to null when its default query found
// nothing, which shows nothing. The request can't say "nothing", so a null
// input is left out of it and the server finds nothing again.
export type WidgetContext = {
  entity?: string;
  inputs?: Record<string, string[] | null>;
  draft?: boolean;
};

// LinkData is where a link goes, as the server filled it in: the catalog
// showing a blueprint's entities that match filter, or href, a URL or a
// path in the portal.
export type LinkData = { blueprint?: string; filter?: Filter; sort?: string; href?: string };

// unit is the stat's own, or % for a share, or else the unit of the field
// it sums up.
export type StatData = { value: number | null; unit?: string; link?: LinkData };
// none counts the entities without a value, which noneLink opens. A
// group's color, such as a scorecard tier's, wins over its property's.
export type GroupData = {
  groups: (Facet & { color?: string; link?: LinkData })[];
  none: number;
  noneLink?: LinkData;
  link?: LinkData;
};
// TierData counts entities by the highest tier they reached, and their
// checks by outcome; failed takes in those that errored. scorecard has
// the tiers to show them by.
export type TierData = {
  tiers: { name: string; count: number }[];
  checks: { passed: number; failed: number };
  scorecard: { identifier: string; blueprint: string; tiers: Tier[] };
  link?: LinkData;
};
// TableColumn says how to show a table column: by the definition of the
// property its path ends on, from whichever blueprint that is, as a
// built-in field, or as a scorecard's tier, with the scorecard's tiers.
export type TableColumn = Column & {
  title: string;
  property?: Property;
  builtIn?: string;
  scorecard?: { identifier: string; blueprint: string; tiers: Tier[] };
};
// TableRow holds a value per column. links name the related entities of a
// relation column, with their titles, and for a tier column the entity
// the scorecard scored.
export type TableRow = {
  identifier: string;
  title: string;
  values: unknown[];
  links?: (Link[] | null)[];
  link?: LinkData;
};
export type TableData = {
  blueprint: string;
  total: number;
  columns: TableColumn[];
  rows: TableRow[];
  link?: LinkData;
};
// TextData is a markdown widget's text, filled in for whoever is looking.
export type TextData = { text: string };
export type WidgetData = StatData | GroupData | TableData | TierData | TextData;

// WidgetResult is one widget's share of its page's data, or why it
// couldn't load, so one failing widget leaves the rest.
export type WidgetResult = { data?: WidgetData; error?: string };

function dataPath(source: WidgetSource) {
  const parts =
    "dashboard" in source
      ? ["dashboards", source.dashboard, "data"]
      : ["blueprints", source.blueprint, "pages", source.page, source.tab, "data"];
  return parts.map(encodeURIComponent).join("/");
}

// PageData is every widget's result, by identifier, and on a dashboard,
// the entities its relation inputs are set to, with their titles.
export type PageData = {
  widgets: Record<string, WidgetResult>;
  inputs?: Record<string, Link[]>;
};

// loadWidgets loads every widget on a page in one request, which the
// server plans so widgets sharing a query share its statements.
export async function loadWidgets(source: WidgetSource, context: WidgetContext): Promise<PageData> {
  const searchParams = new URLSearchParams();
  if (context.entity) searchParams.set("entity", context.entity);
  if (context.draft) searchParams.set("draft", "true");
  for (const [input, values] of Object.entries(context.inputs ?? {})) {
    if (values === null) continue;
    if (values.length === 0) searchParams.append(`input.${input}`, "");
    for (const value of values) searchParams.append(`input.${input}`, value);
  }
  return (await api.get(dataPath(source), { searchParams }).json<ApiDataResponse<PageData>>()).data;
}

export const widgetsQueryOptions = (source: WidgetSource, context: WidgetContext) =>
  queryOptions({
    queryKey: ["widgets", source, context] as const,
    queryFn: () => loadWidgets(source, context),
  });
