import { queryOptions } from "@tanstack/react-query";
import type { Filter } from "@/features/catalog";
import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import type { Widget } from "@/features/widgets";
import { type ApiDataResponse, api } from "@/lib/api";

// DashboardFilter is a value Home asks for above the dashboard, which its
// widgets compare with as $filter.<id>.
export type DashboardFilter = {
  identifier: string;
  title: string;
  type: "enum" | "string" | "relation";
  // values are what an enum filter offers.
  values?: string[];
  // relation is the blueprint whose entities a relation filter offers.
  relation?: string;
  multiple?: boolean;
  // default is what the filter is set to until someone changes it.
  default?: string[];
  // lookup finds the default in the catalog instead, for whoever is
  // looking: the entities of a blueprint that match where.
  lookup?: { from: string; where: Filter };
};

// DashboardSpec is a tab of widgets on Home.
export type DashboardSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  // order sorts Home's tabs, lowest first.
  order: number;
  visibility?: { roles?: string[]; groups?: string[] };
  defaultFor?: { roles?: string[]; groups?: string[] };
  filters: DashboardFilter[];
  widgets: Widget[];
};

// HomeDashboard is a dashboard as Home shows it to whoever is looking,
// with its text filled in for them.
export type HomeDashboard = DashboardSpec & {
  // defaults are what the filters start at for them, by identifier, with
  // lookup defaults found. A filter without a default is left out; a
  // lookup that found nothing is an empty list, which selects nothing.
  defaults: Record<string, string[]>;
  preferred?: boolean;
  // problems say why a lookup default couldn't be found, by filter. The
  // filter starts at nothing then, as if the lookup found none.
  problems?: Record<string, string>;
};

export type Dashboard = ConfigDoc<DashboardSpec>;
export type DashboardSummary = DocSummary;

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
