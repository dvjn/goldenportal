import { queryOptions } from "@tanstack/react-query";
import { type ApiDataResponse, api } from "@/lib/api";

// ConfigKind is the API path of a kind of configuration document.
export type ConfigKind = "blueprints" | "integrations" | "scorecards" | "dashboards";

export type IssueSeverity = "error" | "warning";

export type Issue = {
  severity: IssueSeverity;
  path?: string;
  line?: number;
  message: string;
};

export type DocSummary = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  // publishedVersion is 0 until the first publish.
  publishedVersion: number;
  hasDraft: boolean;
  stats: Record<string, number>;
  updatedAt: string;
};

export type ConfigDoc<S> = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  published: {
    version: number;
    source: string;
    spec: S;
    publishedAt: string;
    publishedBy: string;
  } | null;
  draft: {
    source: string;
    spec: S;
    issues: Issue[];
    updatedAt: string;
    updatedBy: string;
  } | null;
  createdAt: string;
};

export type Validation<S> = { spec: S; issues: Issue[] };

export type Change = {
  section: string;
  identifier?: string;
  change: "added" | "removed" | "changed";
  breaking: boolean;
  detail: string;
};

export type Impact = {
  firstVersion: boolean;
  changes: Change[];
  breaking: boolean;
  // affected counts the entities that breaking changes reach.
  affected: number;
  issues: Issue[];
};

export type DocVersion = {
  version: number;
  publishedAt: string;
  publishedBy: string;
  source?: string;
};

export type ConfigDocs<S> = ReturnType<typeof configDocs<S>>;

// configDocs is the API of one kind of configuration document. Every kind
// has the same draft, publish and version lifecycle.
export function configDocs<S>(kind: ConfigKind) {
  const path = (identifier: string) => `${kind}/${encodeURIComponent(identifier)}`;
  const keys = {
    all: [kind] as const,
    detail: (identifier: string) => [kind, identifier] as const,
    versions: (identifier: string) => [kind, identifier, "versions"] as const,
    version: (identifier: string, version: number) =>
      [kind, identifier, "versions", version] as const,
    impact: (identifier: string) => [kind, identifier, "impact"] as const,
    validation: (source: string) => [kind, "validation", source] as const,
  };

  const list = async () => (await api.get(kind).json<ApiDataResponse<DocSummary[]>>()).data;
  const get = async (identifier: string) =>
    (await api.get(path(identifier)).json<ApiDataResponse<ConfigDoc<S>>>()).data;
  const validate = async (source: string) =>
    (
      await api
        .post(`${kind}/validate`, { json: { source } })
        .json<ApiDataResponse<Validation<S>>>()
    ).data;
  const impact = async (identifier: string) =>
    (await api.get(`${path(identifier)}/draft/impact`).json<ApiDataResponse<Impact>>()).data;
  const versions = async (identifier: string) =>
    (await api.get(`${path(identifier)}/versions`).json<ApiDataResponse<DocVersion[]>>()).data;
  const version = async (identifier: string, n: number) =>
    (await api.get(`${path(identifier)}/versions/${n}`).json<ApiDataResponse<DocVersion>>()).data;

  return {
    kind,
    keys,
    list,
    get,
    validate,
    saveDraft: async (identifier: string, source: string) =>
      (
        await api
          .put(`${path(identifier)}/draft`, { json: { source } })
          .json<ApiDataResponse<{ created: boolean; issues: Issue[] }>>()
      ).data,
    discardDraft: async (identifier: string) => {
      await api.delete(`${path(identifier)}/draft`);
    },
    publish: async (identifier: string, acknowledgeBreaking: boolean) =>
      (
        await api
          .post(`${path(identifier)}/draft/publish`, { json: { acknowledgeBreaking } })
          .json<ApiDataResponse<{ version: number }>>()
      ).data,
    remove: async (identifier: string) => {
      await api.delete(path(identifier), { searchParams: { confirm: identifier } });
    },
    exportUrl: (identifier: string) => `/api/v1/${path(identifier)}/export`,
    queries: {
      list: queryOptions({ queryKey: keys.all, queryFn: list }),
      detail: (identifier: string) =>
        queryOptions({ queryKey: keys.detail(identifier), queryFn: () => get(identifier) }),
      versions: (identifier: string) =>
        queryOptions({ queryKey: keys.versions(identifier), queryFn: () => versions(identifier) }),
      version: (identifier: string, n: number) =>
        queryOptions({
          queryKey: keys.version(identifier, n),
          queryFn: () => version(identifier, n),
          staleTime: Infinity,
        }),
      impact: (identifier: string) =>
        queryOptions({
          queryKey: keys.impact(identifier),
          queryFn: () => impact(identifier),
          staleTime: 0,
        }),
      validation: (source: string) =>
        queryOptions({
          queryKey: keys.validation(source),
          queryFn: () => validate(source),
          staleTime: Infinity,
        }),
    },
  };
}
