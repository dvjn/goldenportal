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
  | "relation"
  | "scorecard";

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
  // scorecard names the scorecard whose result a scorecard property holds.
  scorecard?: string;
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
  // titles replace an enum value's own text where it's shown.
  titles?: Record<string, string>;
  // filter offers its values as a catalog filter.
  filter?: boolean;
  hidden?: boolean;
  // empty is shown when an entity has no value, such as "No owner".
  empty?: string;
};

// EntityHeader names the properties an entity page shows beside its title.
export type EntityHeader = {
  subtitle?: string;
  badges?: string[];
  // link is a URL property the page offers to open.
  link?: string;
};

// PropertySection groups properties under a title on the entity page.
export type PropertySection = { title: string; properties: string[] };

// Source turns the items of one resource of a connection into the
// blueprint's entities. Every expression is jq over an item.
export type Source = {
  connection: string;
  // resource names the items the source reads, such as users.
  resource: string;
  filter?: string;
  // mode upsert only writes what changed, deleting only what items say was
  // deleted; reconcile also deletes what a complete sync no longer reads.
  mode: "reconcile" | "upsert";
  entity: { identifier: string; title?: string; properties: SourceProperty[] };
};

export type SourceProperty = { identifier: string; expression: string };

export type BlueprintSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  properties: Property[];
  // catalog and entity are the tabs the blueprint adds to its catalog page
  // and its entities' pages.
  // catalog.columns are its table's columns until someone picks their own.
  catalog?: { columns?: string[]; tabs: Tab[] };
  entity?: { header?: EntityHeader; sections?: PropertySection[]; tabs: Tab[] };
  // retention deletes rows once their by property is older than keep, or
  // than the longest keep of the rules they match. Without it, rows are kept
  // for ever.
  retention?: Retention;
  sources?: Source[];
};

export type Retention = { by: string; keep: string; rules?: { keep: string }[] };

export type Blueprint = ConfigDoc<BlueprintSpec>;
export type BlueprintSummary = DocSummary;

// Writer is a connection that writes a blueprint's entities.
export type Writer = {
  connection: string;
  title: string;
  entities: number;
  // mapped is false when it wrote entities before the blueprint's sources stopped reading it.
  mapped: boolean;
  lastRunAt: string | null;
};

// UsedSource is one of a blueprint's published sources, in the order it
// declares them, with how its connection last ran. That run read all the
// connection's resources, not only this one.
export type UsedSource = {
  connection: string;
  title: string;
  resource: string;
  mode: Source["mode"];
  lastRunAt: string | null;
  lastRunOutcome?: "succeeded" | "warnings" | "failed";
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

// Usage is how a blueprint is used: its entities, its sources, who writes
// them, and which blueprints relate to it.
export type Usage = {
  entities: number;
  sources: UsedSource[];
  writers: Writer[];
  references: Reference[];
};

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
  if (property.scorecard) return "scorecard";
  if (property.lookup) return "lookup";
  if (property.compute) return "compute";
  return "stored";
}
