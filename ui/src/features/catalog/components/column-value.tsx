import type { EntitySummary } from "../api";
import type { Column } from "../columns";
import { DerivedValue } from "./derived-value";
import { PropertyValue } from "./property-value";
import { RelationLinks } from "./relation-links";

export function ColumnValue({
  column,
  entity,
}: Readonly<{ column: Column; entity: EntitySummary }>) {
  if (column.derived) {
    return <DerivedValue derived={column.derived} items={entity.derived[column.key]} compact />;
  }
  if (column.property) {
    return (
      <PropertyValue value={entity.properties[column.key]} property={column.property} compact />
    );
  }
  return <RelationLinks refs={entity.relations[column.key]} />;
}
