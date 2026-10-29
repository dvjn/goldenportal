import type { Blueprint, DerivedView, InverseView, PropertyView, RelationView } from "./api";

export type Column = {
  kind: "property" | "relation" | "inverse" | "derived";
  key: string;
  label: string;
  description: string;
  sortable: boolean;
  align: "left" | "right";
  className: string;
  property?: PropertyView;
  relation?: RelationView;
  inverse?: InverseView;
  derived?: DerivedView;
};

const nowrapDisplays = new Set(["relative", "absolute", "badge", "icon", "status"]);

export function propertyByKey(blueprint: Blueprint, key: string | undefined) {
  return blueprint.properties.find((property) => property.key === key);
}

export function relationByKey(blueprint: Blueprint, key: string) {
  return blueprint.relations.find((relation) => relation.key === key);
}

export function inverseByKey(blueprint: Blueprint, key: string) {
  return blueprint.inverses.find((inverse) => inverse.key === key);
}

export function derivedByKey(blueprint: Blueprint, key: string) {
  return blueprint.derived.find((derived) => derived.key === key);
}

export function visibleProperties(blueprint: Blueprint): PropertyView[] {
  return blueprint.properties.filter((property) => !property.hidden);
}

export function propertyClassName(property: PropertyView): string {
  if (property.type === "number") return "text-right tabular-nums";
  if (nowrapDisplays.has(property.display)) return "whitespace-nowrap";
  return "max-w-64 truncate";
}

function propertyColumn(property: PropertyView): Column {
  return {
    kind: "property",
    key: property.key,
    label: property.title || property.key,
    description: property.description ?? "",
    sortable: property.sortable,
    align: property.type === "number" ? "right" : "left",
    className: propertyClassName(property),
    property,
  };
}

function relationColumn(relation: RelationView): Column {
  return {
    kind: "relation",
    key: relation.key,
    label: relation.title || relation.key,
    description: relation.description ?? "",
    sortable: false,
    align: "left",
    className: "max-w-80",
    relation,
  };
}

function inverseColumn(inverse: InverseView & { key: string }): Column {
  return {
    kind: "inverse",
    key: inverse.key,
    label: inverse.title || inverse.key,
    description: "",
    sortable: false,
    align: "left",
    className: "max-w-80",
    inverse,
  };
}

function derivedColumn(derived: DerivedView): Column {
  return {
    kind: "derived",
    key: derived.key,
    label: derived.title || derived.key,
    description: derived.description ?? derived.property?.description ?? "",
    sortable: derived.sortable,
    align: derived.property?.type === "number" ? "right" : "left",
    className: "max-w-80",
    derived,
  };
}

function hasKey(inverse: InverseView): inverse is InverseView & { key: string } {
  return inverse.key !== undefined && inverse.key !== "";
}

export function columnOptions(blueprint: Blueprint): Column[] {
  const options = [
    ...visibleProperties(blueprint).map(propertyColumn),
    ...blueprint.relations.map(relationColumn),
    ...blueprint.inverses.filter(hasKey).map(inverseColumn),
    ...blueprint.derived.map(derivedColumn),
  ];
  const configured = blueprint.table.columns;
  const rank = (column: Column) => {
    const index = configured.indexOf(column.key);
    return index === -1 ? configured.length : index;
  };
  return options.toSorted((left, right) => rank(left) - rank(right));
}

export function resolveColumns(blueprint: Blueprint, visible: readonly string[]): Column[] {
  return columnOptions(blueprint).filter((column) => visible.includes(column.key));
}
