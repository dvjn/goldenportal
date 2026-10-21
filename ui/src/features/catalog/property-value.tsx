import { Check, Minus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { formatDateTime } from "@/lib/time";
import type { PropertyView } from "./api";

const dateFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

export function PropertyValue({ property, value }: { property: PropertyView; value: unknown }) {
  if (value === undefined || value === null || value === "") {
    return <span className="text-muted-foreground">Not set</span>;
  }

  switch (property.type) {
    case "boolean":
      return value ? (
        <span className="flex items-center gap-1">
          <Check className="size-4" />
          Yes
        </span>
      ) : (
        <span className="flex items-center gap-1">
          <Minus className="size-4" />
          No
        </span>
      );
    case "number":
      return <span className="tabular-nums">{Number(value).toLocaleString("en")}</span>;
    case "array":
      return (
        <span className="flex flex-wrap gap-1">
          {(value as unknown[]).map((item) => (
            <Badge key={String(item)} variant="secondary">
              {String(item)}
            </Badge>
          ))}
        </span>
      );
    case "object":
      return <code className="font-mono text-xs break-all">{JSON.stringify(value)}</code>;
    default:
      return <StringValue property={property} value={String(value)} />;
  }
}

function StringValue({ property, value }: { property: PropertyView; value: string }) {
  if (property.enum) return <Badge variant="secondary">{value}</Badge>;

  switch (property.format) {
    case "url":
      return (
        <a href={value} target="_blank" rel="noreferrer" className="break-all underline">
          {value}
        </a>
      );
    case "email":
      return (
        <a href={`mailto:${value}`} className="underline">
          {value}
        </a>
      );
    case "date":
      return <span>{dateFormat.format(new Date(value))}</span>;
    case "date-time":
      return <span>{formatDateTime(value)}</span>;
    default:
      return <span className="break-words">{value}</span>;
  }
}
