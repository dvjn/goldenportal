import {
  CheckIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  CircleMinusIcon,
  CirclePauseIcon,
  CircleXIcon,
  ClockIcon,
  ExternalLinkIcon,
  LoaderCircleIcon,
  type LucideIcon,
  XIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { formatDateTime, formatRelative, notSet } from "@/lib/time";
import { cn } from "@/lib/utils";
import type { EnumColor, PropertyDisplay, PropertyView } from "../api";
import { displayUrl, isSafeUrl } from "../format";
import { EnumBadge, enumColor, enumLabel } from "./enum-badge";

type RenderContext = {
  value: unknown;
  text: string;
  property: PropertyView | undefined;
  compact: boolean;
};

// EmptyValue shows a blueprint's empty label in full, or behind "Not set" in a
// compact table cell.
export function EmptyValue({
  label,
  compact = false,
}: Readonly<{ label?: string; compact?: boolean }>) {
  if (compact && label) {
    return (
      <span title={label} className="cursor-help text-muted-foreground">
        {notSet}
      </span>
    );
  }
  return <span className="text-muted-foreground">{label || notSet}</span>;
}

function isEmpty(value: unknown): boolean {
  return value === null || value === undefined || value === "";
}

function asText(value: unknown): string {
  if (typeof value === "boolean") return value ? "Yes" : "No";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

export function initials(email: string): string {
  const local = email.split("@")[0] ?? "";
  return local
    .split(/[._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

function PlainText({ text, compact, mono = false }: Readonly<RenderContext & { mono?: boolean }>) {
  return (
    <span
      title={compact ? text : undefined}
      className={cn(compact ? "block truncate" : "break-words", mono && "font-mono text-xs")}
    >
      {text}
    </span>
  );
}

function MailLink({ text, compact }: Readonly<RenderContext>) {
  return (
    <a
      href={`mailto:${text}`}
      title={text}
      className={cn(
        "text-primary underline-offset-4 hover:underline",
        compact ? "block truncate" : "break-all",
      )}
    >
      {text}
    </a>
  );
}

function DateValue({ text, relative }: Readonly<{ text: string; relative: boolean }>) {
  return (
    <time dateTime={text} title={formatDateTime(text)}>
      {relative ? formatRelative(text) : formatDateTime(text)}
    </time>
  );
}

function BooleanIcon({ value }: Readonly<{ value: unknown }>) {
  return value ? (
    <CheckIcon
      role="img"
      aria-label="Yes"
      className="inline-block size-4 align-[-0.3em] text-success"
    />
  ) : (
    <XIcon
      role="img"
      aria-label="No"
      className="inline-block size-4 align-[-0.3em] text-muted-foreground"
    />
  );
}

function JsonValue({ value, text, compact }: Readonly<RenderContext>) {
  if (Array.isArray(value) && value.length === 0) return <EmptyValue />;
  return (
    <code
      title={compact ? text : undefined}
      className={cn(
        "rounded bg-muted px-1 py-0.5 font-mono text-xs",
        compact ? "block truncate" : "break-all",
      )}
    >
      {text}
    </code>
  );
}

const statusIcons: Record<EnumColor, { Icon: LucideIcon; className: string }> = {
  green: { Icon: CircleCheckIcon, className: "text-success" },
  red: { Icon: CircleXIcon, className: "text-destructive" },
  blue: { Icon: LoaderCircleIcon, className: "animate-spin text-muted-foreground" },
  yellow: { Icon: CircleAlertIcon, className: "text-warning" },
  orange: { Icon: CircleAlertIcon, className: "text-warning" },
  purple: { Icon: CirclePauseIcon, className: "text-muted-foreground" },
  teal: { Icon: ClockIcon, className: "text-muted-foreground" },
  gray: { Icon: CircleMinusIcon, className: "text-muted-foreground" },
};

function StatusIcon({ label, color }: Readonly<{ label: string; color: EnumColor }>) {
  const { Icon, className } = statusIcons[color];
  return (
    <span role="img" aria-label={label} title={label} className="inline-flex shrink-0">
      <Icon aria-hidden="true" className={cn("size-4", className)} />
    </span>
  );
}

const visibleChips = 3;

function ChipList({ items, compact }: Readonly<{ items: string[]; compact: boolean }>) {
  if (items.length === 0) return <EmptyValue />;
  const shown = compact ? items.slice(0, visibleChips) : items;
  const rest = items.slice(shown.length);

  return (
    <span className="flex flex-wrap gap-1">
      {shown.map((item) => (
        <Badge
          key={item}
          variant="outline"
          title={compact ? item : undefined}
          className={cn("font-mono", compact ? "max-w-48" : "h-auto max-w-full")}
        >
          <span className={compact ? "truncate" : "break-all whitespace-normal"}>{item}</span>
        </Badge>
      ))}
      {rest.length > 0 ? (
        <Badge variant="muted" title={rest.join(", ")}>
          +{rest.length}
        </Badge>
      ) : null}
    </span>
  );
}

const renderers: Record<PropertyDisplay, (context: RenderContext) => ReactNode> = {
  text: (context) => <PlainText {...context} />,
  mono: (context) => <PlainText {...context} mono />,
  badge: ({ value, text, property }) => {
    if (property?.type === "boolean") {
      return value ? (
        <Badge variant="muted">{property.title || property.key}</Badge>
      ) : (
        <BooleanIcon value={false} />
      );
    }
    if (property?.enum) {
      return <EnumBadge label={text} color={enumColor(property, asText(value))} />;
    }
    return <Badge variant="secondary">{text}</Badge>;
  },
  link: (context) => {
    if (!isSafeUrl(context.text)) return <PlainText {...context} />;
    return (
      <a
        href={context.text}
        target="_blank"
        rel="noreferrer"
        title={context.text}
        className={cn(
          "inline-flex max-w-full items-center gap-1 text-primary underline-offset-4 hover:underline",
          context.compact ? "truncate" : "break-all",
        )}
      >
        <span className="truncate">{context.property?.link_label || displayUrl(context.text)}</span>
        <ExternalLinkIcon className="size-3 shrink-0" aria-hidden="true" />
      </a>
    );
  },
  email: (context) => <MailLink {...context} />,
  avatar: (context) => (
    <span className="inline-flex max-w-full items-center gap-2">
      <span
        aria-hidden="true"
        className="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted text-[0.625rem] font-medium text-muted-foreground"
      >
        {initials(context.text)}
      </span>
      <MailLink {...context} />
    </span>
  ),
  relative: ({ text }) => <DateValue text={text} relative />,
  absolute: ({ text }) => <DateValue text={text} relative={false} />,
  number: ({ value, text }) => (
    <span className="tabular-nums">
      {typeof value === "number" ? value.toLocaleString() : text}
    </span>
  ),
  icon: ({ value }) => <BooleanIcon value={value} />,
  json: (context) => <JsonValue {...context} />,
  status: ({ value, text, property, compact }) => (
    <span className="inline-flex max-w-full items-center gap-1.5">
      <StatusIcon label={text} color={enumColor(property, asText(value))} />
      {compact ? null : <span className="break-words">{text}</span>}
    </span>
  ),
  list: (context) => {
    const { value } = context;
    if (!Array.isArray(value) || value.some((item) => typeof item === "object")) {
      return <JsonValue {...context} />;
    }
    return <ChipList items={[...new Set(value.map(String))]} compact={context.compact} />;
  },
};

export function PropertyValue({
  value,
  property,
  compact = false,
}: Readonly<{
  value: unknown;
  property: PropertyView | undefined;
  compact?: boolean;
}>) {
  if (isEmpty(value)) return <EmptyValue label={property?.empty_label} compact={compact} />;

  const render = renderers[property?.display ?? "text"] ?? renderers.text;
  return render({ value, text: enumLabel(property, asText(value)), property, compact });
}

export function Timestamp({ value }: Readonly<{ value: string }>) {
  return <DateValue text={value} relative />;
}
