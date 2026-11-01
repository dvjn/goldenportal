import { CalendarRange, ChevronLeft, ListFilter, ListFilterPlus } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  type DateRange,
  datePresets,
  describeDateRange,
  isoDate,
  withinStart,
} from "./data-table-date-filter";
import { DataTableFacetedOptions, type DataTableOption } from "./data-table-faceted-filter";

export type DataTableFilterField =
  | {
      kind: "options";
      id: string;
      title: string;
      // useOptions is called only once the field is picked or filtered, so a
      // table with many fields doesn't load every field's values up front. It
      // returns undefined while they load.
      useOptions: () => DataTableOption[] | undefined;
      // multiple is false when picking a value replaces the last one.
      multiple?: boolean;
    }
  | { kind: "date"; id: string; title: string };

export type DataTableFilterValue = string[] | DateRange;

// DataTableFilters maps each filtered field to its values, or its range of
// days, in the order the filters were added.
export type DataTableFilters = Record<string, DataTableFilterValue>;

// DataTableFilterChange sets a field's filter; undefined removes it.
export type DataTableFilterChange = (id: string, value: DataTableFilterValue | undefined) => void;

// DataTableFilterFields lists the fields a filter can be added for. It's the
// first step of the picker, kept apart so typed filters can suggest from it too.
export function DataTableFilterFields({
  fields,
  onSelect,
}: Readonly<{ fields: DataTableFilterField[]; onSelect: (field: DataTableFilterField) => void }>) {
  return (
    <Command>
      <CommandInput placeholder="Filter by…" />
      <CommandList>
        <CommandEmpty>Nothing matches.</CommandEmpty>
        <CommandGroup>
          {fields.map((field) => (
            <CommandItem key={field.id} value={field.title} onSelect={() => onSelect(field)}>
              {field.kind === "date" ? <CalendarRange /> : <ListFilter />}
              {field.title}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </Command>
  );
}

// DataTableFilterEditor picks the values of one field's filter: options from a
// list, or days from presets and a calendar.
export function DataTableFilterEditor({
  field,
  value,
  onChange,
}: Readonly<{
  field: DataTableFilterField;
  value: DataTableFilterValue | undefined;
  onChange: (value: DataTableFilterValue) => void;
}>) {
  if (field.kind === "date") {
    return <DateEditor value={Array.isArray(value) ? {} : (value ?? {})} onChange={onChange} />;
  }
  return (
    <OptionsEditor field={field} selected={Array.isArray(value) ? value : []} onChange={onChange} />
  );
}

function OptionsEditor({
  field,
  selected,
  onChange,
}: Readonly<{
  field: Extract<DataTableFilterField, { kind: "options" }>;
  selected: string[];
  onChange: (values: string[]) => void;
}>) {
  const options = field.useOptions();
  return (
    <DataTableFacetedOptions
      title={field.title}
      options={options ?? []}
      loading={options === undefined}
      selected={selected}
      onChange={onChange}
      multiple={field.multiple}
    />
  );
}

const day = (date: string | undefined) => (date ? new Date(`${date}T00:00:00`) : undefined);

function DateEditor({
  value,
  onChange,
}: Readonly<{ value: DateRange; onChange: (range: DateRange) => void }>) {
  const preset = presetOf(value);
  // A within range shows as the days it reaches over until a day is picked.
  const shown: DateRange = value.within
    ? { from: isoDate(withinStart(value.within)), to: isoDate(new Date()) }
    : value;
  return (
    <div className="flex flex-col gap-2 sm:flex-row">
      <div className="flex flex-col gap-1 sm:w-40">
        {datePresets.map((p) => (
          <Button
            key={p.label}
            variant={p.label === preset ? "secondary" : "ghost"}
            size="sm"
            className="justify-start"
            aria-pressed={p.label === preset}
            onClick={() => onChange(p.range())}
          >
            {p.label}
          </Button>
        ))}
      </div>
      <Calendar
        mode="range"
        selected={shown.from || shown.to ? { from: day(shown.from), to: day(shown.to) } : undefined}
        defaultMonth={day(shown.to ?? shown.from)}
        onSelect={(range) =>
          onChange({
            from: range?.from && isoDate(range.from),
            to: range?.to && isoDate(range.to),
          })
        }
      />
    </div>
  );
}

function presetOf(range: DateRange) {
  return datePresets.find((p) => {
    const preset = p.range();
    return preset.from === range.from && preset.to === range.to && preset.within === range.within;
  })?.label;
}

// dateFilterLabel names a range by its preset while it still matches one, as
// "Today" reads better than the day it spans.
export function dateFilterLabel(range: DateRange) {
  return presetOf(range) ?? describeDateRange(range);
}

// DataTableFilterPicker adds a filter: pick a field, then its values.
export function DataTableFilterPicker({
  fields,
  value,
  onChange,
}: Readonly<{
  fields: DataTableFilterField[];
  value: DataTableFilters;
  onChange: DataTableFilterChange;
}>) {
  const [open, setOpen] = useState(false);
  const [picked, setPicked] = useState<DataTableFilterField>();

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setPicked(undefined);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="outline">
          <ListFilterPlus />
          Filter
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className={picked?.kind === "date" ? "w-auto gap-0 p-0" : "w-60 gap-0 p-0"}
      >
        {picked ? (
          <>
            <div className="flex items-center gap-1 border-b p-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Back to fields"
                onClick={() => setPicked(undefined)}
              >
                <ChevronLeft />
              </Button>
              <span className="font-medium">{picked.title}</span>
            </div>
            <div className={picked.kind === "date" ? "p-2" : undefined}>
              <DataTableFilterEditor
                field={picked}
                value={value[picked.id]}
                onChange={(next) => onChange(picked.id, next)}
              />
            </div>
          </>
        ) : (
          <DataTableFilterFields fields={fields} onSelect={setPicked} />
        )}
      </PopoverContent>
    </Popover>
  );
}
