import { queryOptions } from "@tanstack/react-query";
import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import type { Widget } from "@/features/widgets";
import { type ApiDataResponse, api } from "@/lib/api";

// DashboardSpec is a tab of widgets on Home.
export type DashboardSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  // order sorts Home's tabs, lowest first.
  order: number;
  widgets: Widget[];
};

export type Dashboard = ConfigDoc<DashboardSpec>;
export type DashboardSummary = DocSummary;

export const dashboards = configDocs<DashboardSpec>("dashboards");

export async function listHome(): Promise<DashboardSpec[]> {
  return (await api.get("home").json<ApiDataResponse<DashboardSpec[]>>()).data;
}

export const homeQueryOptions = queryOptions({
  queryKey: [...dashboards.keys.all, "home"] as const,
  queryFn: listHome,
});
