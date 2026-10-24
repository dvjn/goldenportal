import { queryOptions } from "@tanstack/react-query";
import { type ApiDataResponse, api } from "@/lib/api";

export type Entity = {
  blueprint: string;
  identifier: string;
  title: string;
  // properties include relations and computed properties.
  properties: Record<string, unknown>;
  integration: string;
  createdAt: string;
  updatedAt: string;
};

export type EntityPage = { entities: Entity[]; total: number };

export type EntityFilters = {
  blueprint?: string;
  integration?: string;
  q?: string;
  page?: number;
  size?: number;
};

export const entityKeys = {
  all: ["entities"] as const,
  page: (filters: EntityFilters) => [...entityKeys.all, filters] as const,
};

export async function listEntities(filters: EntityFilters): Promise<EntityPage> {
  const searchParams = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") searchParams.set(key, String(value));
  }
  return (await api.get("entities", { searchParams }).json<ApiDataResponse<EntityPage>>()).data;
}

export const entitiesQueryOptions = (filters: EntityFilters) =>
  queryOptions({ queryKey: entityKeys.page(filters), queryFn: () => listEntities(filters) });
