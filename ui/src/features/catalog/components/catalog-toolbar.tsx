import { CheckIcon, FilterXIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandGroup,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { IntegrationIcon } from "@/features/configuration/components/icons";
import type { Blueprint, EntityFilter, EntityPage, PropertyView } from "../api";
import { type Column, derivedByKey, inverseByKey, propertyByKey, relationByKey } from "../columns";
import type { SourceIcons } from "../sources";
import { BooleanFilter } from "./boolean-filter";
import { ColumnPicker } from "./column-picker";
import { DateFilter } from "./date-filter";
import { EnumBadge, enumColor, enumLabel } from "./enum-badge";
import { EnumFilter } from "./enum-filter";
import { FilterTrigger } from "./filter-trigger";
import { RelationFilter } from "./relation-filter";
import { SearchBox } from "./search-box";

type FilterChange = (filter: EntityFilter | undefined) => void;

function PropertyFilter({
  property,
  label,
  filter,
  onChange,
}: Readonly<{
  property: PropertyView;
  label: string;
  filter: EntityFilter | undefined;
  onChange: FilterChange;
}>) {
  switch (property.filter_kind) {
    case "enum":
      return (
        <EnumFilter
          label={label}
          options={(property.enum ?? []).map((value) => {
            const valueLabel = enumLabel(property, value);
            return {
              value,
              label: valueLabel,
              content: <EnumBadge label={valueLabel} color={enumColor(property, value)} />,
            };
          })}
          filter={filter}
          onChange={onChange}
        />
      );
    case "boolean":
      return <BooleanFilter label={label} filter={filter} onChange={onChange} />;
    case "date":
      return <DateFilter label={label} filter={filter} onChange={onChange} />;
    default:
      return null;
  }
}

function FilterControl({
  blueprint,
  filterKey,
  filter,
  onChange,
}: Readonly<{
  blueprint: Blueprint;
  filterKey: string;
  filter: EntityFilter | undefined;
  onChange: FilterChange;
}>) {
  const relation = relationByKey(blueprint, filterKey);
  if (relation) {
    return (
      <RelationFilter
        label={relation.title || relation.key}
        target={relation.target}
        filter={filter}
        onChange={onChange}
      />
    );
  }
  const inverse = inverseByKey(blueprint, filterKey);
  if (inverse) {
    return (
      <RelationFilter
        label={inverse.title || filterKey}
        target={inverse.source}
        filter={filter}
        onChange={onChange}
      />
    );
  }
  const derived = derivedByKey(blueprint, filterKey);
  if (derived) {
    const label = derived.title || derived.key;
    if (derived.property) {
      return (
        <PropertyFilter
          property={derived.property}
          label={label}
          filter={filter}
          onChange={onChange}
        />
      );
    }
    return (
      <RelationFilter label={label} target={derived.target} filter={filter} onChange={onChange} />
    );
  }
  const property = propertyByKey(blueprint, filterKey);
  if (!property) return null;
  return (
    <PropertyFilter
      property={property}
      label={property.title || property.key}
      filter={filter}
      onChange={onChange}
    />
  );
}

function IntegrationFilter({
  integrations,
  sources,
  selected,
  onChange,
}: Readonly<{
  integrations: EntityPage["integrations"];
  sources: SourceIcons | undefined;
  selected: string | undefined;
  onChange: (integration: string | undefined) => void;
}>) {
  const summary =
    integrations.find((integration) => integration.identifier === selected)?.title ??
    selected ??
    "Any";

  return (
    <Popover>
      <PopoverTrigger asChild>
        <FilterTrigger label="Integration" summary={summary} />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 p-0">
        <Command>
          <CommandList>
            <CommandGroup>
              {integrations.map((integration) => (
                <CommandItem
                  key={integration.identifier}
                  value={integration.identifier}
                  data-checked={selected === integration.identifier}
                  onSelect={() =>
                    onChange(
                      selected === integration.identifier ? undefined : integration.identifier,
                    )
                  }
                >
                  <IntegrationIcon
                    icon={sources?.ofIntegration(integration.identifier) ?? ""}
                    className="size-4"
                  />
                  <span className="min-w-0 flex-1 truncate">{integration.title}</span>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {integration.count}
                  </span>
                  <CheckIcon
                    aria-hidden="true"
                    className={selected === integration.identifier ? "opacity-100" : "opacity-0"}
                  />
                </CommandItem>
              ))}
            </CommandGroup>
            {selected ? (
              <>
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem onSelect={() => onChange(undefined)}>Clear selection</CommandItem>
                </CommandGroup>
              </>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function countLabel({
  total,
  entityCount,
  filtered,
}: Readonly<{ total: number | undefined; entityCount: number; filtered: boolean }>): string {
  if (filtered) return `${total ?? 0} of ${entityCount} match`;
  const count = total ?? entityCount;
  return `${count} ${count === 1 ? "entity" : "entities"}`;
}

export function CatalogToolbar({
  blueprint,
  searchText,
  onSearchTextChange,
  filters,
  onFilterChange,
  integrations,
  sources,
  integration,
  onIntegrationChange,
  filtered,
  onClearFilters,
  total,
  columnOptions,
  visibleColumns,
  onVisibleColumnsChange,
  onResetColumns,
}: Readonly<{
  blueprint: Blueprint;
  searchText: string;
  onSearchTextChange: (text: string) => void;
  filters: Record<string, EntityFilter>;
  onFilterChange: (key: string, filter: EntityFilter | undefined) => void;
  integrations: EntityPage["integrations"];
  sources: SourceIcons | undefined;
  integration: string | undefined;
  onIntegrationChange: (integration: string | undefined) => void;
  filtered: boolean;
  onClearFilters: () => void;
  total: number | undefined;
  columnOptions: Column[];
  visibleColumns: string[];
  onVisibleColumnsChange: (visible: string[]) => void;
  onResetColumns: () => void;
}>) {
  const showIntegrations = integrations.length > 0 || integration !== undefined;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <SearchBox
          label={`Search ${blueprint.title.toLowerCase()}`}
          value={searchText}
          onChange={onSearchTextChange}
        />
        <div className="flex shrink-0 items-center gap-3">
          <span
            className="text-sm whitespace-nowrap tabular-nums text-muted-foreground"
            aria-live="polite"
          >
            {countLabel({ total, entityCount: blueprint.entity_count, filtered })}
          </span>
          <ColumnPicker
            options={columnOptions}
            visible={visibleColumns}
            onChange={onVisibleColumnsChange}
            onReset={onResetColumns}
          />
        </div>
      </div>
      {blueprint.table.filters.length > 0 || showIntegrations ? (
        <div className="flex flex-wrap items-center gap-2">
          {blueprint.table.filters.map((key) => (
            <FilterControl
              key={key}
              blueprint={blueprint}
              filterKey={key}
              filter={filters[key]}
              onChange={(filter) => onFilterChange(key, filter)}
            />
          ))}
          {showIntegrations ? (
            <IntegrationFilter
              integrations={integrations}
              sources={sources}
              selected={integration}
              onChange={onIntegrationChange}
            />
          ) : null}
          {filtered ? (
            <Button type="button" variant="ghost" onClick={onClearFilters}>
              <FilterXIcon aria-hidden="true" />
              Clear filters
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
