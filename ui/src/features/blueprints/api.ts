import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import type { Tab } from "@/features/widgets";
import { type ApiDataResponse, api } from "@/lib/api";

export type PropertyType =
  | "string"
  | "number"
  | "integer"
  | "boolean"
  | "array"
  | "object"
  | "relation";

export type Property = {
  identifier: string;
  title: string;
  description?: string;
  type: PropertyType | "";
  format?: string;
  enum?: (string | number)[];
  required?: boolean;
  pattern?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  items?: { type: PropertyType; format?: string; enum?: (string | number)[] };
  // relation names the blueprint a relation points at.
  relation?: string;
  many?: boolean;
  lookup?: string;
  compute?: string;
  display?: Display;
};

// Display is how a property's values look; the type picks anything unset.
export type Display = {
  as?:
    | "text"
    | "badge"
    | "code"
    | "link"
    | "date"
    | "relative"
    | "number"
    | "progress"
    | "check"
    | "tags"
    | "list"
    | "json";
  unit?: string;
  colors?: Record<string, string>;
  // column shows it in the catalog table until someone picks columns.
  column?: boolean;
  // filter offers its values as a catalog filter.
  filter?: boolean;
  hidden?: boolean;
};

export type BlueprintSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  properties: Property[];
  // catalog and entity are the tabs the blueprint adds to its catalog page
  // and its entities' pages.
  catalog?: { tabs: Tab[] };
  entity?: { tabs: Tab[] };
};

export type Blueprint = ConfigDoc<BlueprintSpec>;
export type BlueprintSummary = DocSummary;

// Writer is an integration that writes a blueprint's entities.
export type Writer = {
  integration: string;
  title: string;
  entities: number;
  // mapped is false when it wrote entities before its mappings dropped the blueprint.
  mapped: boolean;
  lastRunAt: string | null;
};

// Reference is a relation on another blueprint that points at this one.
export type Reference = {
  blueprint: string;
  title: string;
  property: string;
  propertyTitle: string;
  many: boolean;
};

export const blueprints = configDocs<BlueprintSpec>("blueprints");

// Usage is how a blueprint is used: its entities, who writes them, and
// which blueprints relate to it.
export type Usage = { entities: number; writers: Writer[]; references: Reference[] };

export const usageKey = (identifier: string) =>
  [...blueprints.keys.detail(identifier), "usage"] as const;

export async function getUsage(identifier: string): Promise<Usage> {
  return (
    await api
      .get(`blueprints/${encodeURIComponent(identifier)}/usage`)
      .json<ApiDataResponse<Usage>>()
  ).data;
}

// propertySource says where a property's value comes from.
export function propertySource(property: Property) {
  if (property.relation) return "relation";
  if (property.lookup) return "lookup";
  if (property.compute) return "compute";
  return "stored";
}
