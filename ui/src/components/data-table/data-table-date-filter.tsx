import { CalendarRange } from "lucide-react";
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

const presets = [
  { label: "Today", days: 0 },
  { label: "Last 7 days", days: 6 },
  { label: "Last 30 days", days: 29 },
];

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
              onClick={() => onChange({ from: daysAgo(preset.days), to: isoDate(new Date()) })}
            >
              {preset.label}
            </Button>
          ))}
        </div>
        <FieldGroup className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="date-from">From</FieldLabel>
            <Input
              id="date-from"
              type="date"
              value={value.from ?? ""}
              max={value.to}
              onChange={(event) => onChange({ ...value, from: event.target.value || undefined })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="date-to">To</FieldLabel>
            <Input
              id="date-to"
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
