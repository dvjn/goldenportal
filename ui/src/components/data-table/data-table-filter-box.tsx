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

type DataTableFilterBoxProps = {
  label: string;
  query: string;
  onQueryChange: (query: string) => void;
  fields: DataTableFilterField[];
  value: DataTableFilters;
  onChange: DataTableFilterChange;
  // onClear empties the search and removes every filter.
  onClear: () => void;
  // fixed are filters the table can show but not edit, only remove.
  fixed?: DataTableFixedFilter[];
};

export type DataTableFixedFilter = { title: string; summary: string; onRemove: () => void };

// DataTableFilterBox is a search box that holds the table's filters as chips
// ahead of the search text, with a button beside it to add more.
export function DataTableFilterBox({
  label,
  query,
  onQueryChange,
  fields,
  value,
  onChange,
  onClear,
  fixed = [],
}: DataTableFilterBoxProps) {
  const input = useRef<HTMLInputElement>(null);
  const chips = Object.entries(value).flatMap(([id, filter]) => {
    const field = fields.find((f) => f.id === id);
    return field ? [{ field, filter }] : [];
  });
  const last = chips.at(-1);

  // Chips go away under the pointer or with Backspace, so the box keeps focus.
  function remove(id: string) {
    onChange(id, undefined);
    input.current?.focus();
  }

  return (
    <div className="flex min-w-0 flex-1 items-start gap-2">
      <InputGroup className="h-auto min-h-8 max-w-3xl flex-1 items-start">
        <InputGroupAddon className="h-7.5 py-0">
          <Search />
        </InputGroupAddon>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 py-1 pr-1 pl-1.5">
          {fixed.map((filter) => (
            <Badge
              key={`${filter.title}: ${filter.summary}`}
              variant="secondary"
              className="h-5.5 max-w-full gap-0 rounded-md p-0 pr-0.5"
            >
              <span className="flex min-w-0 items-center gap-1 pr-1 pl-1.5">
                <span className="text-muted-foreground">{filter.title}:</span>{" "}
                <span className="truncate">{filter.summary}</span>
              </span>
              <button
                type="button"
                aria-label={`Remove ${filter.title} filter`}
                onClick={() => {
                  filter.onRemove();
                  input.current?.focus();
                }}
                className="flex size-4 items-center justify-center rounded-sm text-muted-foreground outline-none hover:bg-foreground/10 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
              >
                <X className="size-3" />
              </button>
            </Badge>
          ))}
          {chips.map(({ field, filter }) => (
            <FilterChip
              key={field.id}
              field={field}
              filter={filter}
              onChange={(next) => onChange(field.id, next)}
              onRemove={() => remove(field.id)}
            />
          ))}
          <InputGroupInput
            ref={input}
            type="search"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Backspace" && query === "" && last) remove(last.field.id);
            }}
            placeholder={chips.length > 0 || fixed.length > 0 ? undefined : label}
            aria-label={label}
            className="h-5.5 min-w-32 px-0 not-first:pl-1 [&::-webkit-search-cancel-button]:appearance-none"
          />
        </div>
        {(chips.length > 0 || fixed.length > 0 || query !== "") && (
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
        <DataTableFilterPicker fields={fields} value={value} onChange={onChange} />
      )}
    </div>
  );
}

type FilterChipProps = {
  field: DataTableFilterField;
  filter: DataTableFilterValue;
  onChange: (value: DataTableFilterValue) => void;
  onRemove: () => void;
};

function FilterChip(props: FilterChipProps) {
  const { field, filter } = props;
  if (field.kind === "date") {
    return <Chip {...props} summary={dateFilterLabel(Array.isArray(filter) ? {} : filter)} />;
  }
  return <OptionsChip {...props} field={field} />;
}

function OptionsChip(
  props: FilterChipProps & { field: Extract<DataTableFilterField, { kind: "options" }> },
) {
  const options = props.field.useOptions();
  const values = Array.isArray(props.filter) ? props.filter : [];
  const labels = values.map(
    (value) => options?.find((option) => option.value === value)?.label ?? value,
  );
  const summary =
    labels.length > 2
      ? `${labels.slice(0, 2).join(", ")} +${labels.length - 2}`
      : labels.join(", ");
  return <Chip {...props} summary={summary} />;
}

// Chip is one filter in the box: it opens the filter's editor, and its × removes it.
function Chip({
  field,
  filter,
  onChange,
  onRemove,
  summary,
}: FilterChipProps & { summary: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Badge variant="secondary" className="h-5.5 max-w-full gap-0 rounded-md p-0 pr-0.5">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="flex h-full min-w-0 items-center gap-1 rounded-l-md pr-1 pl-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
          >
            <span className="text-muted-foreground">{field.title}:</span>{" "}
            <span className="truncate">{summary}</span>
          </button>
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className={field.kind === "date" ? "w-auto p-2" : "w-60 gap-0 p-0"}
        >
          <DataTableFilterEditor field={field} value={filter} onChange={onChange} />
        </PopoverContent>
      </Popover>
      <button
        type="button"
        aria-label={`Remove ${field.title} filter`}
        onClick={onRemove}
        className="flex size-4 items-center justify-center rounded-sm text-muted-foreground outline-none hover:bg-foreground/10 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-inset"
      >
        <X className="size-3" />
      </button>
    </Badge>
  );
}
