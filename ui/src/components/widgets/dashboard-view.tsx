import { useQuery } from "@tanstack/react-query";
import type { DashboardInput, HomeDashboard } from "@/features/dashboards";
import { widgetsQueryOptions } from "@/features/widgets";
import { DashboardInputs, type InputValues } from "./dashboard-inputs";
import { WidgetGrid } from "./widget-grid";

// Picked is what the viewer set the inputs to, keyed by input: a value,
// several for a multiple input, or "" for one cleared of its default. An
// input it doesn't mention is at its default. Home keeps it in the URL,
// and the builder's preview in memory.
export type Picked = Record<string, string | string[] | undefined>;

// starting is what an input starts at for the viewer: its default, as the
// server found it, or null when a default query found nothing.
function starting(view: HomeDashboard, input: DashboardInput): string[] | null {
  const found = view.defaults[input.identifier];
  if (found === undefined) return [];
  return found.length === 0 ? null : found;
}

// inputValues is what each input is set to: what was picked, or what
// the input starts at when nothing was.
export function inputValues(view: HomeDashboard, picked: Picked): InputValues {
  return Object.fromEntries(
    view.inputs.map((input) => {
      const raw = picked[input.identifier];
      if (raw === undefined) return [input.identifier, starting(view, input)];
      const values = Array.isArray(raw) ? raw : raw === "" ? [] : [raw];
      return [input.identifier, input.multiple ? values : values.slice(0, 1)];
    }),
  );
}

// toPicked is how an input's values are kept: left out at what it starts
// at, and "" when cleared, which an input starting at nothing needs to
// show everything.
function toPicked(
  view: HomeDashboard,
  input: DashboardInput,
  values: string[],
): string | string[] | undefined {
  const start = starting(view, input);
  if (start !== null && same(values, start)) return undefined;
  if (values.length === 0) return "";
  return values.length === 1 ? values[0] : values;
}

function same(a: string[], b: string[]) {
  return a.length === b.length && a.every((value, i) => value === b[i]);
}

// DashboardView is a dashboard's input bar and widgets. Every widget on it
// loads with the same input values, in the request that also names what
// relation inputs are set to.
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
  const values = inputValues(view, picked);
  const changed = view.inputs.some((input) => picked[input.identifier] !== undefined);
  const source = { dashboard: view.identifier };
  const context = { inputs: values, draft };
  const { data: links } = useQuery({
    ...widgetsQueryOptions(source, context),
    enabled: view.inputs.some((input) => input.type === "relation"),
    select: (data) => data.inputs,
  });
  return (
    <>
      {view.inputs.length > 0 && (
        <DashboardInputs
          inputs={view.inputs}
          values={values}
          problems={view.problems}
          links={links}
          onChange={(input, next) => onPick({ [input.identifier]: toPicked(view, input, next) })}
          onReset={
            changed
              ? () => onPick(Object.fromEntries(view.inputs.map((f) => [f.identifier, undefined])))
              : undefined
          }
        />
      )}
      <WidgetGrid widgets={view.widgets} source={source} context={context} />
    </>
  );
}
