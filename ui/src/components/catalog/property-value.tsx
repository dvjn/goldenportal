import { cn } from "cn";
import { Check, Minus, X } from "lucide-react";
import { EntityLink } from "@/components/catalog/entity-link";
import { Markdown } from "@/components/markdown";
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
  // compact fits the value on one table row: long text is cut short, with the
  // whole value on hover, and lists show their first few items.
  compact?: boolean;
};

// compactItems is how many list items and relations a compact value shows.
const compactItems = 3;

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
  const text = numbers.format(value);
  if (!unit) return text;
  return unit === "%" ? `${text}%` : `${text} ${unit}`;
}

// PropertyValue shows one value the way its property's display says.
export function PropertyValue({ property, value, links, compact = false }: PropertyValueProps) {
  const cut = compact ? "block max-w-64 truncate" : "";
  if (property.relation) {
    if (!links || links.length === 0) return <Empty label={property.display?.empty} />;
    const shown = compact ? links.slice(0, compactItems) : links;
    return (
      <span className="flex flex-wrap gap-x-2 gap-y-1">
        {shown.map((link) =>
          link.title ? (
            <EntityLink
              key={link.identifier}
              blueprint={property.relation ?? ""}
              identifier={link.identifier}
            >
              {link.title}
            </EntityLink>
          ) : (
            <span
              key={link.identifier}
              className="font-mono text-xs text-muted-foreground"
              title="Not ingested yet"
            >
              {link.identifier}
            </span>
          ),
        )}
        <More hidden={links.slice(shown.length).map((link) => link.title || link.identifier)} />
      </span>
    );
  }
  if (value === undefined || value === null || value === "")
    return <Empty label={property.display?.empty} />;

  const as = displayAs(property);
  const colors = property.display?.colors;

  if (Array.isArray(value)) {
    if (value.length === 0) return <Empty label={property.display?.empty} />;
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
    const shown = compact ? items.slice(0, compactItems) : items;
    return (
      <span className="flex flex-wrap gap-1">
        {shown.map((item) => (
          <ValueBadge key={item} value={item} color={colors?.[item]} variant="outline" />
        ))}
        <More hidden={items.slice(shown.length)} />
      </span>
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
function More({ hidden }: Readonly<{ hidden: string[] }>) {
  if (hidden.length === 0) return null;
  return (
    <span className="text-xs text-muted-foreground" title={hidden.join(", ")}>
      +{hidden.length}
    </span>
  );
}

// ValueBadge shows a value as a badge, in its color if it has one.
export function ValueBadge({
  value,
  color,
  variant = "secondary",
}: Readonly<{ value: string; color?: string; variant?: "secondary" | "outline" }>) {
  const tint = swatch(color);
  return (
    <Badge variant={variant} className={cn("font-normal", tint?.badge)}>
      {value}
    </Badge>
  );
}

// Empty marks a missing value, in the words the property's display gives.
function Empty({ label }: Readonly<{ label?: string }>) {
  if (label) return <span className="text-muted-foreground">{label}</span>;
  return <Minus className="size-3.5 text-muted-foreground/60" aria-label="None" />;
}
