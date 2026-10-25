import { Check, Minus, X } from "lucide-react";
import { EntityLink } from "@/components/catalog/entity-link";
import { Badge } from "@/components/ui/badge";
import type { Property } from "@/features/blueprints";
import type { Link } from "@/features/catalog";
import { formatAbsolute, formatRelative } from "@/lib/format";

type PropertyValueProps = {
  property: Property;
  value: unknown;
  // links are the related entities, for relations.
  links?: Link[];
};

// PropertyValue shows one value the way its property's type suggests.
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

  if (Array.isArray(value)) {
    if (value.length === 0) return <Empty />;
    return (
      <span className="flex flex-wrap gap-1">
        {value.map((item) => (
          <Badge key={JSON.stringify(item)} variant="outline" className="font-normal">
            {typeof item === "object" ? JSON.stringify(item) : String(item)}
          </Badge>
        ))}
      </span>
    );
  }
  if (typeof value === "boolean") {
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

  const text = String(value);
  switch (property.format) {
    case "uri":
      return (
        <a href={text} target="_blank" rel="noreferrer" className="break-all hover:underline">
          {text.replace(/^https?:\/\//, "")}
        </a>
      );
    case "email":
      return (
        <a href={`mailto:${text}`} className="hover:underline">
          {text}
        </a>
      );
    case "date-time":
      return <span title={formatAbsolute(text)}>{formatRelative(text)}</span>;
  }
  if (property.enum) {
    return (
      <Badge variant="secondary" className="font-normal">
        {text}
      </Badge>
    );
  }
  return <span className="break-words">{text}</span>;
}

function Empty() {
  return <Minus className="size-3.5 text-muted-foreground/60" aria-label="None" />;
}
