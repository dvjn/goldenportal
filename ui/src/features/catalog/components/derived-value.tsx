import { Link } from "@tanstack/react-router";
import type { DerivedItem, DerivedView, PropertyView } from "../api";
import { EmptyValue, PropertyValue } from "./property-value";
import { RelationLinks } from "./relation-links";

const selfLinkingDisplays = new Set(["link", "email", "avatar"]);

function PropertyItem({
  item,
  property,
  compact,
}: Readonly<{ item: DerivedItem; property: PropertyView; compact: boolean }>) {
  const value = <PropertyValue value={item.value} property={property} compact={compact} />;
  if (selfLinkingDisplays.has(property.display)) return value;
  return (
    <Link
      to="/catalog/$blueprint/$identifier"
      params={{ blueprint: item.entity.blueprint, identifier: item.entity.identifier }}
      title={item.entity.title || item.entity.identifier}
      className="inline-flex max-w-full"
    >
      {value}
    </Link>
  );
}

export function DerivedValue({
  derived,
  items,
  compact = false,
}: Readonly<{
  derived: DerivedView;
  items: DerivedItem[] | undefined;
  compact?: boolean;
}>) {
  if (!items || items.length === 0) {
    return <EmptyValue label={derived.empty_label} compact={compact} />;
  }
  const { property } = derived;
  if (!property) return <RelationLinks refs={items.map((item) => item.entity)} />;

  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {items.map((item) => (
        <PropertyItem
          key={`${item.entity.blueprint}/${item.entity.identifier}`}
          item={item}
          property={property}
          compact={compact}
        />
      ))}
    </span>
  );
}
