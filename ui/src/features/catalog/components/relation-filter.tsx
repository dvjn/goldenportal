import { useState } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { EntityFilter } from "../api";
import { useEntityOptions } from "../queries";
import { listFilter, toggleValue } from "../search";
import { useDebouncedValue } from "../use-debounced-value";
import { CheckMark, FilterTrigger } from "./filter-trigger";

const searchDebounceMs = 300;

type RelationFilterProps = {
  label: string;
  /** The blueprint whose entities the filter lists. */
  target: string;
  filter: EntityFilter | undefined;
  onChange: (filter: EntityFilter | undefined) => void;
};

function RelationOptions({ label, target, filter, onChange }: Readonly<RelationFilterProps>) {
  const [search, setSearch] = useState("");
  const options = useEntityOptions(target, useDebouncedValue(search, searchDebounceMs));
  const isEmpty = filter?.empty === true;
  const selected = filter?.in ?? [];

  return (
    <Command shouldFilter={false}>
      <CommandInput
        placeholder={`Search ${label.toLowerCase()}`}
        value={search}
        onValueChange={setSearch}
      />
      <CommandList>
        <CommandGroup>
          <CommandItem
            value="is-empty"
            data-checked={isEmpty}
            onSelect={() => onChange(isEmpty ? undefined : { empty: true })}
          >
            <CheckMark checked={isEmpty} />
            Not set
          </CommandItem>
        </CommandGroup>
        <CommandSeparator />
        {options.isPending ? null : <CommandEmpty>No matches</CommandEmpty>}
        <CommandGroup>
          {(options.data ?? []).map((entity) => (
            <CommandItem
              key={entity.identifier}
              value={entity.identifier}
              data-checked={selected.includes(entity.identifier)}
              onSelect={() => onChange(listFilter(toggleValue(selected, entity.identifier)))}
            >
              <CheckMark checked={selected.includes(entity.identifier)} />
              <span className="truncate">{entity.title || entity.identifier}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </Command>
  );
}

function relationSummary(filter: EntityFilter | undefined): string {
  if (filter?.empty) return "Not set";
  const selected = filter?.in ?? [];
  return selected.length === 0 ? "Any" : `${selected.length} selected`;
}

export function RelationFilter(props: Readonly<RelationFilterProps>) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <FilterTrigger label={props.label} summary={relationSummary(props.filter)} />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 p-0">
        <RelationOptions {...props} />
      </PopoverContent>
    </Popover>
  );
}
