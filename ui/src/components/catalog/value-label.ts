import type { Property } from "@/features/blueprints";
import { formatAbsolute, formatDate } from "@/lib/format";

type Labelled = Pick<Property, "enum" | "items" | "display">;

export type ValueNames = {
  // property is the one the value belongs to, for its enum and its titles.
  property?: Labelled;
  // titles name relation targets and scorecard tiers by value, which only
  // the server and the scorecard know.
  titles?: ReadonlyMap<string, string>;
};

const day = /^\d{4}-\d{2}-\d{2}$/;
const instant = /^\d{4}-\d{2}-\d{2}T/;

// titleCase writes a value as words: on-prem is On Prem.
export function titleCase(text: string) {
  return text
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((word) => word[0].toUpperCase() + word.slice(1))
    .join(" ");
}

// valueLabel is the one way a property's value is written, in filter chips
// and options and in table cells: a boolean as Yes or No, a relation as its
// entity's title, a scorecard tier as its title, and an enum as the
// blueprint's title for it, or else in Title Case.
export function valueLabel(value: unknown, { property, titles }: ValueNames = {}): string {
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value !== "string") return String(value);
  const title = titles?.get(value);
  if (title) return title;
  if (day.test(value)) return formatDate(value);
  if (instant.test(value)) return formatAbsolute(value);
  if (property?.display?.titles?.[value]) return property.display.titles[value];
  if (property?.enum || property?.items?.enum) return titleCase(value);
  return value;
}
