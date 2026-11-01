import { queryOptions } from "@tanstack/react-query";
import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import type { Query, Widget } from "@/features/widgets";
import { type ApiDataResponse, api } from "@/lib/api";

// DashboardInput is a value Home asks for above the dashboard. It narrows
// only the queries that compare with it as $input.<id>.
export type DashboardInput = {
  identifier: string;
  title: string;
  type: "enum" | "string" | "relation";
  // values are what an enum input offers.
  values?: string[];
  // relation is the blueprint whose entities a relation input offers.
  relation?: string;
  multiple?: boolean;
  // default is what the input is set to until someone changes it.
  default?: string[];
  // defaultQuery finds the default in the catalog instead, for whoever is
  // looking: the entities its query matches. otherwise is what it starts
  // at when it finds none.
  defaultQuery?: { query: Query; otherwise?: "nothing" | "everything" };
};

// DashboardSpec is a tab of widgets on Home.
export type DashboardSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  // order sorts Home's tabs, lowest first.
  order: number;
  visibility?: Audience;
  defaultFor?: Audience;
  inputs: DashboardInput[];
  // queries are the named queries its widgets share.
  queries?: Query[];
  widgets: Widget[];
};

// HomeDashboard is a dashboard as Home shows it to whoever is looking,
// with its text filled in for them.
export type HomeDashboard = DashboardSpec & {
  // defaults are what the inputs start at for them, by identifier, with
  // default queries run. An input without a default is left out; a query
  // that found nothing is an empty list, which selects nothing.
  defaults: Record<string, string[]>;
  preferred?: boolean;
  // problems say why a default query couldn't run, by input. The input
  // starts at nothing then, as if the query found none.
  problems?: Record<string, string>;
};

export type Dashboard = ConfigDoc<DashboardSpec>;
// Audience is who a dashboard is for: anyone with one of the roles or in
// one of the groups.
export type Audience = { roles?: string[]; groups?: string[] };

// DashboardSummary is a dashboard as listed, with where it sits on Home
// and who sees it there, or opens on it.
export type DashboardSummary = DocSummary & {
  facts?: { order: number; visibility: Audience | null; defaultFor: Audience | null };
};

export const dashboards = configDocs<DashboardSpec>("dashboards");

export async function listHome(): Promise<HomeDashboard[]> {
  return (await api.get("home").json<ApiDataResponse<HomeDashboard[]>>()).data;
}

export const homeQueryOptions = queryOptions({
  queryKey: [...dashboards.keys.all, "home"] as const,
  queryFn: listHome,
});

// viewDashboard is one dashboard as Home would show it to whoever is
// looking, from the draft when a builder asks, for the preview.
export async function viewDashboard(identifier: string, draft: boolean): Promise<HomeDashboard> {
  const searchParams = draft ? { draft: "true" } : {};
  return (
    await api
      .get(`dashboards/${encodeURIComponent(identifier)}/view`, { searchParams })
      .json<ApiDataResponse<HomeDashboard>>()
  ).data;
}

export const dashboardViewQueryOptions = (identifier: string, draft: boolean) =>
  queryOptions({
    queryKey: [...dashboards.keys.detail(identifier), "view", draft] as const,
    queryFn: () => viewDashboard(identifier, draft),
  });
