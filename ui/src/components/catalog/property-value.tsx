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
export function formatNumber(value: number, unit?: string) {
  const text = numbers.format(value);
  if (!unit) return text;
  return unit === "%" ? `${text}%` : `${text} ${unit}`;
}

// PropertyValue shows one value the way its property's display says.
export function PropertyValue({ property, value, links }: PropertyValueProps) {
  if (property.relation) {
    if (!links || links.length === 0) return <Empty />;
    return (
      <span className="flex flex-wrap gap-x-2 gap-y-1">
        {links.map((link) =>
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
      </span>
    );
  }
  if (value === undefined || value === null || value === "") return <Empty />;

  const as = displayAs(property);
  const colors = property.display?.colors;

  if (Array.isArray(value)) {
    if (value.length === 0) return <Empty />;
    const items = value.map((item) =>
      typeof item === "object" ? JSON.stringify(item) : String(item),
    );
    if (as === "list") return <span className="break-words">{items.join(", ")}</span>;
    return (
      <span className="flex flex-wrap gap-1">
        {items.map((item) => (
          <ValueBadge key={item} value={item} color={colors?.[item]} variant="outline" />
        ))}
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
      ) : (
        <a href={text} target="_blank" rel="noreferrer" className="break-all hover:underline">
          {text.replace(/^https?:\/\//, "")}
        </a>
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
      return <code className="font-mono text-xs break-all">{text}</code>;
  }
  if (property.format === "markdown") return <Markdown>{text}</Markdown>;
  return <span className="break-words">{text}</span>;
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

function Empty() {
  return <Minus className="size-3.5 text-muted-foreground/60" aria-label="None" />;
}
