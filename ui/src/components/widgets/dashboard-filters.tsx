import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { DataTableFacetedFilter } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { entitiesQueryOptions } from "@/features/catalog";
import type { DashboardFilter } from "@/features/dashboards";

// FilterValues is what each of a dashboard's filters is set to, by
// identifier: the values picked, none when cleared to show everything, or
// null when its lookup default found nothing, which shows nothing.
export type FilterValues = Record<string, string[] | null>;

// relationOptions is how many entities a relation filter offers to pick from.
const relationOptions = 100;

// DashboardFilters is the bar above a dashboard's widgets where its filters
// are set. Reset shows while any filter is away from its default. A filter
// set to nothing says so, since its control looks the same as a cleared one,
// and offers to show everything instead; one whose lookup default failed
// says why, since problems is what went wrong by filter.
export function DashboardFilters({
  filters,
  values,
  problems,
  onChange,
  onReset,
}: Readonly<{
  filters: DashboardFilter[];
  values: FilterValues;
  problems?: Record<string, string>;
  onChange: (filter: DashboardFilter, values: string[]) => void;
  onReset?: () => void;
}>) {
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Filters">
      {filters.map((filter) => (
        <FilterControl
          key={filter.identifier}
          filter={filter}
          values={values[filter.identifier] ?? []}
          onChange={(next) => onChange(filter, next)}
        />
      ))}
      {onReset && (
        <Button variant="ghost" onClick={onReset}>
          Reset
          <X />
        </Button>
      )}
      {filters
        .filter((filter) => values[filter.identifier] === null)
        .map((filter) => (
          <p key={filter.identifier} className="basis-full text-sm text-muted-foreground">
            {problems?.[filter.identifier]
              ? `For ${filter.title}, ${problems[filter.identifier]}, so the widgets show nothing.`
              : `Nothing matched you for ${filter.title}, so the widgets show nothing.`}{" "}
            <Button
              variant="link"
              className="h-auto p-0 text-sm"
              onClick={() => onChange(filter, [])}
            >
              Show everything
            </Button>
          </p>
        ))}
    </div>
  );
}

function FilterControl({
  filter,
  values,
  onChange,
}: Readonly<{ filter: DashboardFilter; values: string[]; onChange: (values: string[]) => void }>) {
  switch (filter.type) {
    case "enum":
      return (
        <DataTableFacetedFilter
          title={filter.title}
          options={(filter.values ?? []).map((value) => ({ value, label: value }))}
          selected={values}
          onChange={onChange}
          multiple={filter.multiple}
        />
      );
    case "relation":
      return <RelationFilter filter={filter} values={values} onChange={onChange} />;
    case "string":
      // Remounting on the value keeps the draft in step with the URL.
      return (
        <TextFilter
          key={values[0] ?? ""}
          filter={filter}
          value={values[0] ?? ""}
          onChange={onChange}
        />
      );
  }
}

function RelationFilter({
  filter,
  values,
  onChange,
}: Readonly<{ filter: DashboardFilter; values: string[]; onChange: (values: string[]) => void }>) {
  const { data } = useQuery(
    entitiesQueryOptions({ blueprint: filter.relation ?? "", size: relationOptions }),
  );
  const options = (data?.entities ?? []).map((entity) => ({
    value: entity.identifier,
    label: entity.title,
  }));
  return (
    <DataTableFacetedFilter
      title={filter.title}
      options={options}
      selected={values}
      onChange={onChange}
      multiple={filter.multiple}
    />
  );
}

// TextFilter applies its text when it's left or Enter is pressed, so
// widgets don't reload on every keystroke.
function TextFilter({
  filter,
  value,
  onChange,
}: Readonly<{ filter: DashboardFilter; value: string; onChange: (values: string[]) => void }>) {
  const [draft, setDraft] = useState(value);
  function commit() {
    const text = draft.trim();
    if (text !== value) onChange(text === "" ? [] : [text]);
  }
  return (
    <Input
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") commit();
      }}
      placeholder={filter.title}
      aria-label={filter.title}
      className="w-44"
    />
  );
}
