import { Link as RouterLink } from "@tanstack/react-router";
import { cn } from "cn";
import { Check, X } from "lucide-react";
import { EntityLink } from "@/components/catalog/entity-link";
import { ListValue } from "@/components/catalog/list-value";
import { useLookupTarget } from "@/components/catalog/lookup-target";
import { Markdown } from "@/components/markdown";
import { ScorecardValue } from "@/components/scorecards/scorecard-value";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import type { Display, Property } from "@/features/blueprints";
import type { Link } from "@/features/catalog";
import { swatch } from "@/lib/colors";
import { formatAbsolute, formatDate, formatRelative } from "@/lib/format";

type PropertyValueProps = {
  property: Property;
  value: unknown;
  // links are the related entities, for relations.
  links?: Link[];
  // entity is whose value it is, which a scorecard value links to its
  // result on.
  entity?: { blueprint: string; identifier: string };
  // compact fits the value on one table row: long text is cut short, with the
  // whole value on hover, and lists show their first item.
  compact?: boolean;
};

// displayAs is how a property's values look: as its display says, or as its
// type suggests.
export function displayAs(property: Property): NonNullable<Display["as"]> {
  if (property.display?.as) return property.display.as;
  if (property.enum) return "badge";
  switch (property.format) {
    case "uri":
    case "email":
      return "link";
    case "date":
      return "date";
    case "date-time":
      return "relative";
  }
  switch (property.type) {
    case "number":
    case "integer":
      return "number";
    case "boolean":
      return "check";
    case "array":
      return "tags";
    case "object":
      return "json";
  }
  return "text";
}

const numbers = new Intl.NumberFormat("en", { maximumFractionDigits: 2 });

// formatNumber writes a number with its unit, if any.
// isWebUrl reports whether text is an http(s) URL. Values come from sources,
// so anything else, such as a javascript: URL, mustn't become a link.
export function isWebUrl(text: string) {
  try {
    const { protocol } = new URL(text);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

export function formatNumber(value: number, unit?: string) {
  // Percents are whole, as scores are, so a share reads 67%, not 66.67%.
  if (unit === "%") return `${numbers.format(Math.round(value))}%`;
  const text = numbers.format(value);
  return unit ? `${text} ${unit}` : text;
}

// PropertyValue shows one value the way its property's display says.
export function PropertyValue({
  property,
  value,
  links,
  entity,
  compact = false,
}: PropertyValueProps) {
  const cut = compact ? "block max-w-64 truncate" : "";
  if (property.lookup && entity) {
    return (
      <LookupValue
        property={property}
        value={value}
        links={links}
        entity={entity}
        compact={compact}
      />
    );
  }
  if (property.scorecard) {
    return (
      <ScorecardValue
        scorecard={property.scorecard}
        value={value}
        entity={entity}
        empty={property.display?.empty}
      />
    );
  }
  if (property.relation) {
    if (!links || links.length === 0) return <EmptyValue label={property.display?.empty} />;
    const target = property.relation;
    return (
      <ListValue
        compact={compact}
        items={links.map((link) => ({
          key: link.identifier,
          node: link.title ? (
            <EntityLink blueprint={target} identifier={link.identifier}>
              {link.title}
            </EntityLink>
          ) : (
            <span className="font-mono text-xs text-muted-foreground" title="Not ingested yet">
              {link.identifier}
            </span>
          ),
        }))}
        viewAll={
          compact ? (
            entity && <ViewAll entity={entity} tab="relations" />
          ) : (
            <CatalogViewAll blueprint={target} identifiers={links.map((link) => link.identifier)} />
          )
        }
      />
    );
  }
  if (value === undefined || value === null || value === "")
    return <EmptyValue label={property.display?.empty} />;

  const as = displayAs(property);
  const colors = property.display?.colors;

  if (Array.isArray(value)) {
    if (value.length === 0) return <EmptyValue label={property.display?.empty} />;
    const items = value.map((item) =>
      typeof item === "object" ? JSON.stringify(item) : String(item),
    );
    if (as === "list") {
      const joined = items.join(", ");
      return (
        <span className={cn("break-words", cut)} title={compact ? joined : undefined}>
          {joined}
        </span>
      );
    }
    return (
      <ListValue
        compact={compact}
        items={items.map((item) => ({
          key: item,
          node: item,
          tint: swatch(colors?.[item])?.badge,
        }))}
        viewAll={compact && entity && <ViewAll entity={entity} />}
      />
    );
  }
  if (typeof value === "boolean") {
    if (as === "badge") {
      return <ValueBadge value={value ? "Yes" : "No"} color={colors?.[String(value)]} />;
    }
    return value ? (
      <span className="flex items-center gap-1">
        <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" aria-hidden />
        Yes
      </span>
    ) : (
      <span className="flex items-center gap-1 text-muted-foreground">
        <X className="size-3.5" aria-hidden />
        No
      </span>
    );
  }
  if (typeof value === "object") {
    return <code className="font-mono text-xs break-all">{JSON.stringify(value)}</code>;
  }
  if (typeof value === "number") {
    const unit = property.display?.unit;
    if (as === "progress") {
      const min = property.minimum ?? 0;
      const max = property.maximum ?? 100;
      const percent = max > min ? ((value - min) / (max - min)) * 100 : 0;
      return (
        <span className="flex min-w-24 items-center gap-2">
          <Progress value={Math.min(100, Math.max(0, percent))} className="h-1.5 flex-1" />
          <span className="text-xs tabular-nums">{formatNumber(value, unit)}</span>
        </span>
      );
    }
    if (as === "badge")
      return <ValueBadge value={formatNumber(value, unit)} color={colors?.[String(value)]} />;
    return <span className="tabular-nums">{formatNumber(value, unit)}</span>;
  }

  const text = String(value);
  switch (as) {
    case "link":
      return property.format === "email" ? (
        <a href={`mailto:${text}`} className="hover:underline">
          {text}
        </a>
      ) : isWebUrl(text) ? (
        <a
          href={text}
          target="_blank"
          rel="noreferrer"
          className={cn("break-all hover:underline", cut)}
          title={compact ? text : undefined}
        >
          {text.replace(/^https?:\/\//, "")}
        </a>
      ) : (
        <span className={cn("break-all", cut)} title={compact ? text : undefined}>
          {text}
        </span>
      );
    case "relative":
      return (
        <span className="whitespace-nowrap" title={formatAbsolute(text)}>
          {formatRelative(text)}
        </span>
      );
    case "date":
      return <span className="whitespace-nowrap">{formatDate(text)}</span>;
    case "badge":
      return <ValueBadge value={text} color={colors?.[text]} />;
    case "code":
      return (
        <code className={cn("font-mono text-xs break-all", cut)} title={compact ? text : undefined}>
          {text}
        </code>
      );
  }
  if (property.format === "markdown") {
    return compact ? (
      <div className="line-clamp-2 max-w-80">
        <Markdown>{text}</Markdown>
      </div>
    ) : (
      <Markdown>{text}</Markdown>
    );
  }
  return (
    <span className={cn("break-words", cut)} title={compact ? text : undefined}>
      {text}
    </span>
  );
}

// More counts the items a compact value leaves out, and names them on hover.
// ValueBadge shows a value as a badge, in its color if it has one.
export function ValueBadge({
  value,
  color,
  variant = "secondary",
}: Readonly<{ value: string; color?: string; variant?: "secondary" | "outline" }>) {
  const tint = swatch(color);
  return (
    <Badge
      variant={variant}
      className={cn(
        "font-normal",
        tint?.badge ??
          "in-data-[slot=page-badges]:border-border in-data-[slot=page-badges]:bg-card",
      )}
    >
      {value}
    </Badge>
  );
}

// Empty marks a missing value, in the words the property's display gives.
// EmptyValue stands in for a value that isn't there: the label a display
// gives, or else a hyphen, as every empty value shows. title says why on hover.
export function EmptyValue({ label, title }: Readonly<{ label?: string; title?: string }>) {
  if (label) return <span className="text-muted-foreground">{label}</span>;
  return (
    <span className="text-muted-foreground" title={title}>
      <span aria-hidden>-</span>
      <span className="sr-only">{title ?? "None"}</span>
    </span>
  );
}

// ViewAll opens the entity a list belongs to, where the list shows whole.
function ViewAll({
  entity,
  tab,
}: Readonly<{ entity: { blueprint: string; identifier: string }; tab?: string }>) {
  return (
    <EntityLink blueprint={entity.blueprint} identifier={entity.identifier} tab={tab}>
      View all
    </EntityLink>
  );
}

// CatalogViewAll opens the catalog showing just the related entities, to
// page through when there are too many to list.
export function CatalogViewAll({
  blueprint,
  identifiers,
}: Readonly<{ blueprint: string; identifiers: string[] }>) {
  return (
    <RouterLink
      to="/catalog/$blueprint"
      params={{ blueprint }}
      search={{
        view: "entities",
        where: { property: "identifier", op: "in", values: identifiers },
      }}
      className="hover:underline"
    >
      View all
    </RouterLink>
  );
}

// LookupValue shows a looked-up value as the property it reads shows it,
// under the lookup's own title and display.
function LookupValue({
  property,
  entity,
  ...props
}: PropertyValueProps & { entity: { blueprint: string; identifier: string } }) {
  const target = useLookupTarget(entity.blueprint, property.lookup);
  const shown: Property = target
    ? {
        ...target,
        ...property,
        type: target.type,
        format: target.format,
        enum: target.enum,
        relation: target.relation,
        many: target.many,
        items: target.items,
        display: { ...target.display, ...property.display },
        lookup: undefined,
      }
    : { ...property, lookup: undefined };
  return <PropertyValue property={shown} entity={entity} {...props} />;
}
