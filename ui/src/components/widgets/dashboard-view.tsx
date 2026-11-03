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

type DashboardProps = Readonly<{
  view: HomeDashboard;
  picked: Picked;
  onPick: (patch: Picked) => void;
  draft?: boolean;
}>;

// useDashboard is what a dashboard's inputs and widgets load against: the
// inputs' values, and in the same request the widgets make, what relation
// inputs are set to.
function useDashboard(view: HomeDashboard, picked: Picked, draft?: boolean) {
  const values = inputValues(view, picked);
  const source = { dashboard: view.identifier };
  const context = { inputs: values, draft };
  const { data: links } = useQuery({
    ...widgetsQueryOptions(source, context),
    enabled: view.inputs.some((input) => input.type === "relation"),
    select: (data) => data.inputs,
  });
  return { values, source, context, links };
}

// DashboardInputBar is a dashboard's inputs, with Reset while any is away
// from its default. Compact, it fits the page bar, as the page's tabs do.
export function DashboardInputBar({
  view,
  picked,
  onPick,
  draft,
  compact,
}: DashboardProps & { compact?: boolean }) {
  const { values, links } = useDashboard(view, picked, draft);
  if (view.inputs.length === 0) return null;
  const changed = view.inputs.some((input) => picked[input.identifier] !== undefined);
  return (
    <DashboardInputs
      inputs={view.inputs}
      values={values}
      problems={view.problems}
      links={links}
      compact={compact}
      onChange={(input, next) => onPick({ [input.identifier]: toPicked(view, input, next) })}
      onReset={
        changed
          ? () => onPick(Object.fromEntries(view.inputs.map((f) => [f.identifier, undefined])))
          : undefined
      }
    />
  );
}

// DashboardView is a dashboard's widgets, after its inputs unless the page
// shows them itself, as Home does in its header. Every widget on it loads
// with the same input values.
export function DashboardView({
  withInputs = true,
  ...props
}: DashboardProps & { withInputs?: boolean }) {
  const { source, context } = useDashboard(props.view, props.picked, props.draft);
  return (
    <>
      {withInputs && <DashboardInputBar {...props} />}
      <WidgetGrid widgets={props.view.widgets} source={source} context={context} />
    </>
  );
}
