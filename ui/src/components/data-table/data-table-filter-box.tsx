import { Search, X } from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  type DataTableFilterChange,
  DataTableFilterEditor,
  type DataTableFilterField,
  DataTableFilterPicker,
  type DataTableFilters,
  type DataTableFilterValue,
  dateFilterLabel,
} from "./data-table-filter-picker";

// DataTableChip is one filter in the box. With a field it opens that
// field's editor; without one it can only be removed.
export type DataTableChip = {
  title: string;
  // summary says what the filter matches. An editable chip without one
  // lists the labels of its picked values.
  summary?: string;
  field?: DataTableFilterField;
  value?: DataTableFilterValue;
  negated?: boolean;
  onChange?: (value: DataTableFilterValue, negated?: boolean) => void;
  onRemove: () => void;
};

type DataTableFilterBoxProps = {
  label: string;
  query: string;
  onQueryChange: (query: string) => void;
  // chips are the filters, in the order they read.
  chips: DataTableChip[];
  fields: DataTableFilterField[];
  value: DataTableFilters;
  negated?: Record<string, boolean>;
  onChange: DataTableFilterChange;
  // onClear empties the search and removes every filter.
  onClear: () => void;
};

export type DataTableFixedFilter = { title: string; summary: string; onRemove: () => void };

// recordChips are the chips of a table whose filters are a record of
// fields: the fixed ones first, then one for each filtered field.
export function recordChips(
  fields: DataTableFilterField[],
  value: DataTableFilters,
  onChange: DataTableFilterChange,
  fixed: DataTableFixedFilter[] = [],
): DataTableChip[] {
  return [
    ...fixed,
    ...Object.entries(value).flatMap(([id, filter]): DataTableChip[] => {
      const field = fields.find((f) => f.id === id);
      if (!field) return [];
      return [
        {
          title: field.title,
          field,
          value: filter,
          onChange: (next) => onChange(id, next),
          onRemove: () => onChange(id, undefined),
        },
      ];
    }),
  ];
}

// DataTableFilterBox is a search box that holds the table's filters as chips
// ahead of the search text, with a button beside it to add more.
export function DataTableFilterBox({
  label,
  query,
  onQueryChange,
  chips,
  fields,
  value,
  negated,
  onChange,
  onClear,
}: DataTableFilterBoxProps) {
  const input = useRef<HTMLInputElement>(null);
  const last = chips.at(-1);

  // Chips go away under the pointer or with Backspace, so the box keeps focus.
  function remove(chip: DataTableChip) {
    chip.onRemove();
    input.current?.focus();
  }

  return (
    <div className="flex min-w-0 flex-1 items-start gap-2">
      <InputGroup className="h-auto min-h-8 max-w-3xl flex-1 items-start">
        <InputGroupAddon className="h-7.5 py-0">
          <Search />
        </InputGroupAddon>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 py-1 pr-1 pl-1.5">
          {chips.map((chip, index) => (
            // Chips are keyed by where they stand, as nothing else is unique to them.
            // biome-ignore lint/suspicious/noArrayIndexKey: two filters may read alike.
            <FilterChip key={index} chip={chip} onRemove={() => remove(chip)} />
          ))}
          <InputGroupInput
            ref={input}
            type="search"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Backspace" && query === "" && last) remove(last);
            }}
            placeholder={chips.length > 0 ? undefined : label}
            aria-label={label}
            className="h-5.5 min-w-32 px-0 not-first:pl-1 [&::-webkit-search-cancel-button]:appearance-none"
          />
        </div>
        {(chips.length > 0 || query !== "") && (
          <InputGroupAddon align="inline-end" className="h-7.5 py-0">
            <InputGroupButton
              size="icon-xs"
              aria-label="Clear search and filters"
              onClick={() => {
                onClear();
                input.current?.focus();
              }}
            >
              <X />
            </InputGroupButton>
          </InputGroupAddon>
        )}
      </InputGroup>
      {fields.length > 0 && (
        <DataTableFilterPicker
          fields={fields}
          value={value}
          negated={negated}
          onChange={onChange}
        />
      )}
    </div>
  );
}

function FilterChip({ chip, onRemove }: Readonly<{ chip: DataTableChip; onRemove: () => void }>) {
  const { field } = chip;
  if (!field) return <Chip chip={chip} summary={chip.summary} onRemove={onRemove} />;
  if (field.kind === "date") {
    const range = Array.isArray(chip.value) ? {} : (chip.value ?? {});
    return (
      <Chip chip={chip} summary={chip.summary ?? dateFilterLabel(range)} onRemove={onRemove} />
    );
  }
  return <OptionsChip chip={chip} field={field} onRemove={onRemove} />;
}

function OptionsChip({
  chip,
  field,
  onRemove,
}: Readonly<{
  chip: DataTableChip;
  field: Extract<DataTableFilterField, { kind: "options" }>;
  onRemove: () => void;
}>) {
  const options = field.useOptions();
  const values = Array.isArray(chip.value) ? chip.value : [];
  const labels = values.map(
    (value) => options?.find((option) => option.value === value)?.label ?? value,
  );
  const summary = chip.summary ?? `${chip.negated ? "Not " : ""}${labels.join(", ")}`;
  return <Chip chip={chip} summary={summary} onRemove={onRemove} />;
}

// Chip is one filter in the box: it opens the filter's editor, if it has
// one, and its × removes it.
function Chip({
  chip,
  summary,
  onRemove,
}: Readonly<{ chip: DataTableChip; summary: ReactNode; onRemove: () => void }>) {
  const [open, setOpen] = useState(false);
  const { field } = chip;
  const text = (
    <>
      <span className="text-muted-foreground">{chip.title}:</span>{" "}
      <span className="truncate">{summary}</span>
    </>
  );
  return (
    <Badge variant="secondary" className="h-5.5 max-w-full gap-0 rounded-md p-0 pr-0.5">
      {field && chip.onChange ? (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              className="flex h-full min-w-0 items-center gap-1 rounded-l-md pr-1 pl-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
            >
              {text}
            </button>
          </PopoverTrigger>
          <PopoverContent
            align="start"
            className={field.kind === "date" ? "w-auto p-2" : "w-60 gap-0 p-0"}
          >
            <DataTableFilterEditor
              field={field}
              value={chip.value}
              negated={chip.negated}
              onChange={chip.onChange}
            />
          </PopoverContent>
        </Popover>
      ) : (
        <span className="flex min-w-0 items-center gap-1 pr-1 pl-1.5">{text}</span>
      )}
      <button
        type="button"
        aria-label={`Remove ${chip.title} filter`}
        onClick={onRemove}
        className="flex size-4 items-center justify-center rounded-sm text-muted-foreground outline-none hover:bg-foreground/10 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
      >
        <X className="size-3" />
      </button>
    </Badge>
  );
}
