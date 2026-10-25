import type { DashboardFilter, HomeDashboard } from "@/features/dashboards";
import { DashboardFilters, type FilterValues } from "./dashboard-filters";
import { WidgetGrid } from "./widget-grid";

// Picked is what the viewer set the filters to, keyed by filter: a value,
// several for a multiple filter, or "" for one cleared of its default. A
// filter it doesn't mention is at its default. Home keeps it in the URL,
// and the builder's preview in memory.
export type Picked = Record<string, string | string[] | undefined>;

// starting is what a filter starts at for the viewer: its default, as the
// server found it, or null when a lookup default found nothing.
function starting(view: HomeDashboard, filter: DashboardFilter): string[] | null {
  const found = view.defaults[filter.identifier];
  if (found === undefined) return [];
  return found.length === 0 ? null : found;
}

// filterValues is what each filter is set to: what was picked, or what
// the filter starts at when nothing was.
export function filterValues(view: HomeDashboard, picked: Picked): FilterValues {
  return Object.fromEntries(
    view.filters.map((filter) => {
      const raw = picked[filter.identifier];
      if (raw === undefined) return [filter.identifier, starting(view, filter)];
      const values = Array.isArray(raw) ? raw : raw === "" ? [] : [raw];
      return [filter.identifier, filter.multiple ? values : values.slice(0, 1)];
    }),
  );
}

// toPicked is how a filter's values are kept: left out at what it starts
// at, and "" when cleared, which a filter starting at nothing needs to
// show everything.
function toPicked(
  view: HomeDashboard,
  filter: DashboardFilter,
  values: string[],
): string | string[] | undefined {
  const start = starting(view, filter);
  if (start !== null && same(values, start)) return undefined;
  if (values.length === 0) return "";
  return values.length === 1 ? values[0] : values;
}

function same(a: string[], b: string[]) {
  return a.length === b.length && a.every((value, i) => value === b[i]);
}

// DashboardView is a dashboard's filter bar and widgets. Every widget on it
// loads with the same filter values.
export function DashboardView({
  view,
  picked,
  onPick,
  draft,
}: Readonly<{
  view: HomeDashboard;
  picked: Picked;
  onPick: (patch: Picked) => void;
  draft?: boolean;
}>) {
  const values = filterValues(view, picked);
  const changed = view.filters.some((filter) => picked[filter.identifier] !== undefined);
  return (
    <>
      {view.filters.length > 0 && (
        <DashboardFilters
          filters={view.filters}
          values={values}
          problems={view.problems}
          onChange={(filter, next) => onPick({ [filter.identifier]: toPicked(view, filter, next) })}
          onReset={
            changed
              ? () => onPick(Object.fromEntries(view.filters.map((f) => [f.identifier, undefined])))
              : undefined
          }
        />
      )}
      <WidgetGrid
        widgets={view.widgets}
        source={{ dashboard: view.identifier }}
        context={{ filters: values, draft }}
      />
    </>
  );
}
