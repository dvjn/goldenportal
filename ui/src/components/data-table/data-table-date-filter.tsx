import { CalendarRange } from "lucide-react";
import { useId } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";

// Dates are YYYY-MM-DD in the viewer's time zone; to includes that day.
export type DateRange = { from?: string; to?: string };

type DataTableDateFilterProps = {
  title: string;
  value: DateRange;
  onChange: (range: DateRange) => void;
};

function isoDate(date: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function daysAgo(days: number) {
  const date = new Date();
  date.setDate(date.getDate() - days);
  return isoDate(date);
}

const presets: { label: string; range: () => DateRange }[] = [
  { label: "Today", range: () => ({ from: isoDate(new Date()), to: isoDate(new Date()) }) },
  { label: "Last 7 days", range: () => ({ from: daysAgo(6), to: isoDate(new Date()) }) },
  { label: "Last 30 days", range: () => ({ from: daysAgo(29), to: isoDate(new Date()) }) },
  { label: "Last 90 days", range: () => ({ from: daysAgo(89), to: isoDate(new Date()) }) },
  { label: "Older than 90 days", range: () => ({ to: daysAgo(90) }) },
];

// dateRangeBounds turns a range of days into the instants it starts and
// ends: from is the start of its day, and before is the start of the day after to.
export function dateRangeBounds({ from, to }: DateRange): { from?: string; before?: string } {
  const start = (date: string) => new Date(`${date}T00:00:00`);
  const dayAfter = (date: string) => {
    const next = start(date);
    next.setDate(next.getDate() + 1);
    return next;
  };
  return {
    from: from ? start(from).toISOString() : undefined,
    before: to ? dayAfter(to).toISOString() : undefined,
  };
}

const shortDate = new Intl.DateTimeFormat("en", { month: "short", day: "numeric" });

function describe({ from, to }: DateRange) {
  const show = (date: string) => shortDate.format(new Date(`${date}T00:00:00`));
  if (from && to) return from === to ? show(from) : `${show(from)} – ${show(to)}`;
  if (from) return `From ${show(from)}`;
  if (to) return `Until ${show(to)}`;
  return "";
}

// DataTableDateFilter limits a table to a range of days, with quick presets.
export function DataTableDateFilter({ title, value, onChange }: DataTableDateFilterProps) {
  const summary = describe(value);
  const id = useId();

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" className="border-dashed">
          <CalendarRange />
          {title}
          {summary && (
            <>
              <Separator orientation="vertical" className="mx-1 data-vertical:h-4" />
              <Badge variant="secondary">{summary}</Badge>
            </>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="grid w-72 gap-4" align="start">
        <div className="flex flex-wrap gap-2">
          {presets.map((preset) => (
            <Button
              key={preset.label}
              variant="secondary"
              size="sm"
              onClick={() => onChange(preset.range())}
            >
              {preset.label}
            </Button>
          ))}
        </div>
        <FieldGroup className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor={`${id}-from`}>From</FieldLabel>
            <Input
              id={`${id}-from`}
              type="date"
              value={value.from ?? ""}
              max={value.to}
              onChange={(event) => onChange({ ...value, from: event.target.value || undefined })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-to`}>To</FieldLabel>
            <Input
              id={`${id}-to`}
              type="date"
              value={value.to ?? ""}
              min={value.from}
              onChange={(event) => onChange({ ...value, to: event.target.value || undefined })}
            />
          </Field>
        </FieldGroup>
        {summary && (
          <Button variant="ghost" size="sm" onClick={() => onChange({})}>
            Clear dates
          </Button>
        )}
      </PopoverContent>
    </Popover>
  );
}
