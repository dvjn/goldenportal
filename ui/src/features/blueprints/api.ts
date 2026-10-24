import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
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
};

export type BlueprintSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  properties: Property[];
};

export type Blueprint = ConfigDoc<BlueprintSpec>;
export type BlueprintSummary = DocSummary;

// Reference is a relation on another blueprint that points at this one.
export type Reference = {
  blueprint: string;
  title: string;
  property: string;
  propertyTitle: string;
  many: boolean;
};

export const blueprints = configDocs<BlueprintSpec>("blueprints");

export const referencesKey = (identifier: string) =>
  [...blueprints.keys.detail(identifier), "references"] as const;

export async function listReferences(identifier: string): Promise<Reference[]> {
  return (
    await api
      .get(`blueprints/${encodeURIComponent(identifier)}/references`)
      .json<ApiDataResponse<Reference[]>>()
  ).data;
}

// propertySource says where a property's value comes from.
export function propertySource(property: Property) {
  if (property.relation) return "relation";
  if (property.lookup) return "lookup";
  if (property.compute) return "compute";
  return "stored";
}
