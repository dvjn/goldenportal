import { CalendarRange, ChevronLeft, ListFilter, ListFilterPlus } from "lucide-react";
import { useState } from "react";
import { EmptyState } from "@/components/empty-state";
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
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
      // negatable offers an Is Not choice, which a table whose filters
      // can't say it leaves out.
      negatable?: boolean;
    }
  | { kind: "date"; id: string; title: string };

export type DataTableFilterValue = string[] | DateRange;

// DataTableFilters maps each filtered field to its values, or its range of
// days, in the order the filters were added.
export type DataTableFilters = Record<string, DataTableFilterValue>;

// DataTableFilterChange sets a field's filter; undefined removes it.
// negated is only given for fields that are negatable.
export type DataTableFilterChange = (
  id: string,
  value: DataTableFilterValue | undefined,
  negated?: boolean,
) => void;

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
        <CommandEmpty className="py-0">
          <EmptyState compact title="No filters match" />
        </CommandEmpty>
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
  negated,
  onChange,
}: Readonly<{
  field: DataTableFilterField;
  value: DataTableFilterValue | undefined;
  negated?: boolean;
  onChange: (value: DataTableFilterValue, negated?: boolean) => void;
}>) {
  if (field.kind === "date") {
    return <DateEditor value={Array.isArray(value) ? {} : (value ?? {})} onChange={onChange} />;
  }
  return (
    <OptionsEditor
      field={field}
      selected={Array.isArray(value) ? value : []}
      negated={negated}
      onChange={onChange}
    />
  );
}

function OptionsEditor({
  field,
  selected,
  negated,
  onChange,
}: Readonly<{
  field: Extract<DataTableFilterField, { kind: "options" }>;
  selected: string[];
  negated?: boolean;
  onChange: (values: string[], negated?: boolean) => void;
}>) {
  const options = field.useOptions();
  // A choice made before any value is picked has no filter to live in yet.
  const [not, setNot] = useState(negated ?? false);
  const say = (values: string[]) => onChange(values, field.negatable ? not : undefined);
  return (
    <>
      {field.negatable && (
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={not ? "not" : "is"}
          onValueChange={(next) => {
            if (!next) return;
            setNot(next === "not");
            onChange(selected, next === "not");
          }}
          aria-label={`${field.title} match`}
          className="m-1 w-fit"
        >
          <ToggleGroupItem value="is">Is</ToggleGroupItem>
          <ToggleGroupItem value="not">Is Not</ToggleGroupItem>
        </ToggleGroup>
      )}
      <DataTableFacetedOptions
        title={field.title}
        options={options ?? []}
        loading={options === undefined}
        selected={selected}
        onChange={say}
        multiple={field.multiple}
      />
    </>
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
  negated,
  onChange,
}: Readonly<{
  fields: DataTableFilterField[];
  value: DataTableFilters;
  negated?: Record<string, boolean>;
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
                negated={negated?.[picked.id]}
                onChange={(next, not) => onChange(picked.id, next, not)}
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
