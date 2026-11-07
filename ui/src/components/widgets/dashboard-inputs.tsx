import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ListFilter, X } from "lucide-react";
import { type ReactNode, useState } from "react";
import { DataTableFacetedOptions, type DataTableOption } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from "@/components/ui/input-group";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { entitiesQueryOptions, type Link } from "@/features/catalog";
import type { DashboardInput } from "@/features/dashboards";

// InputValues is what each of a dashboard's inputs is set to, by
// identifier: the values picked, none when cleared to show everything, or
// null when its default query found nothing, which shows nothing.
export type InputValues = Record<string, string[] | null>;

// relationOptions is how many entities a relation input offers to pick from.
const relationOptions = 100;

// DashboardInputs is the row above a dashboard's widgets where its inputs
// are set. Each input shows its label and value. Reset shows while any
// input is away from its default. An input set to nothing says so, and
// offers to show everything instead; one whose default query failed says
// why, since problems is what went wrong by input. links name the entities
// relation inputs are set to, by input. Compact, it's the page bar's, where
// it keeps what the widgets are narrowed by in sight while scrolling; when
// the bar is too narrow for every control, they fold into one button.
export function DashboardInputs({
  inputs,
  values,
  problems,
  links,
  onChange,
  onReset,
  compact = false,
}: Readonly<{
  inputs: DashboardInput[];
  values: InputValues;
  problems?: Record<string, string>;
  links?: Record<string, Link[]>;
  onChange: (input: DashboardInput, values: string[]) => void;
  onReset?: () => void;
  compact?: boolean;
}>) {
  const controls = (
    <>
      {inputs.map((input) => (
        <InputControl
          key={input.identifier}
          input={input}
          values={values[input.identifier]}
          links={links?.[input.identifier]}
          onChange={(next) => onChange(input, next)}
        />
      ))}
      {onReset && (
        <Button variant="ghost" onClick={onReset}>
          Reset
          <X />
        </Button>
      )}
    </>
  );

  if (compact) {
    const set = inputs.filter((input) => (values[input.identifier]?.length ?? 0) > 0).length;
    return (
      <>
        <div className="hidden items-center gap-2 xl:flex" role="group" aria-label="Inputs">
          {controls}
        </div>
        <Popover>
          <PopoverTrigger asChild>
            <Button variant="outline" className="xl:hidden">
              <ListFilter />
              Inputs
              {set > 0 && (
                <Badge variant="secondary" className="tabular-nums" aria-label={`${set} set`}>
                  {set}
                </Badge>
              )}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-auto">
            <div className="flex flex-col items-start gap-2" role="group" aria-label="Inputs">
              {controls}
            </div>
          </PopoverContent>
        </Popover>
      </>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Inputs">
      {controls}
      {inputs
        .filter((input) => values[input.identifier] === null)
        .map((input) => (
          <p key={input.identifier} className="basis-full text-sm text-muted-foreground">
            {problems?.[input.identifier]
              ? `For ${input.title}, ${problems[input.identifier]}, so the widgets it narrows show nothing.`
              : `Nothing matched you for ${input.title}, so the widgets it narrows show nothing.`}{" "}
            <Button
              variant="link"
              className="h-auto p-0 text-sm"
              onClick={() => onChange(input, [])}
            >
              Show everything
            </Button>
          </p>
        ))}
    </div>
  );
}

function InputControl({
  input,
  values,
  links,
  onChange,
}: Readonly<{
  input: DashboardInput;
  values: string[] | null;
  links?: Link[];
  onChange: (values: string[]) => void;
}>) {
  switch (input.type) {
    case "enum": {
      const options = (input.values ?? []).map((value) => ({ value, label: value }));
      return (
        <LabelledPicker input={input} labels={options} values={values}>
          <InputOptions input={input} options={options} values={values} onChange={onChange} />
        </LabelledPicker>
      );
    }
    case "relation":
      return <RelationInput input={input} values={values} links={links} onChange={onChange} />;
    case "string":
      // Remounting on the value keeps the draft in step with the URL.
      return (
        <TextInput
          key={values?.[0] ?? ""}
          input={input}
          value={values?.[0] ?? ""}
          onChange={onChange}
        />
      );
  }
}

type InputProps = Readonly<{
  input: DashboardInput;
  values: string[] | null;
  onChange: (values: string[]) => void;
}>;

// RelationInput loads its entities to pick from only once it's opened.
// Until then it names what it's set to by links, or by the entities, once
// they've loaded, for a value just picked.
function RelationInput({ input, values, links, onChange }: InputProps & { links?: Link[] }) {
  const entities = useQuery({ ...relationQueryOptions(input), enabled: false });
  const named = (links ?? []).map((link) => ({
    value: link.identifier,
    label: link.title ?? link.identifier,
  }));
  return (
    <LabelledPicker
      input={input}
      labels={[...toOptions(entities.data?.items), ...named]}
      values={values}
    >
      <RelationOptions input={input} values={values} onChange={onChange} />
    </LabelledPicker>
  );
}

function RelationOptions({ input, values, onChange }: InputProps) {
  const { data, isPending } = useQuery(relationQueryOptions(input));
  return (
    <InputOptions
      input={input}
      options={toOptions(data?.items)}
      values={values}
      onChange={onChange}
      loading={isPending}
    />
  );
}

function relationQueryOptions(input: DashboardInput) {
  return entitiesQueryOptions({ blueprint: input.relation ?? "", size: relationOptions });
}

function toOptions(entities?: { identifier: string; title: string }[]): DataTableOption[] {
  return (entities ?? []).map((entity) => ({ value: entity.identifier, label: entity.title }));
}

// valueText is what an input's control says it's set to. Nothing picked
// means every value, while null means a default that found nothing.
function valueText(options: DataTableOption[], values: string[] | null) {
  if (values === null) return "None";
  if (values.length === 0) return "Any";
  if (values.length > 2) return `${values.length} selected`;
  return values
    .map((value) => options.find((option) => option.value === value)?.label ?? value)
    .join(", ");
}

// LabelledPicker is an input's control: its label and what it's set to,
// by labels, opening children, the values to pick from, which mount only
// while it's open.
function LabelledPicker({
  input,
  labels,
  values,
  children,
}: Readonly<{
  input: DashboardInput;
  labels: DataTableOption[];
  values: string[] | null;
  children: ReactNode;
}>) {
  const [open, setOpen] = useState(false);
  const shown = valueText(labels, values);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" aria-label={`${input.title}: ${shown}`}>
          <span className="text-muted-foreground">{input.title}</span>
          <span className="max-w-48 truncate">{shown}</span>
          <ChevronDown className="text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-60 gap-0 p-0" align="start">
        {children}
      </PopoverContent>
    </Popover>
  );
}

function InputOptions({
  input,
  options,
  values,
  onChange,
  loading,
}: InputProps & { options: DataTableOption[]; loading?: boolean }) {
  return (
    <DataTableFacetedOptions
      title={input.title}
      options={options}
      selected={values ?? []}
      onChange={onChange}
      // An input takes one value unless it says it takes several.
      multiple={input.multiple ?? false}
      loading={loading}
    />
  );
}

// TextInput applies its text when it's left or Enter is pressed, so
// widgets don't reload on every keystroke.
function TextInput({
  input,
  value,
  onChange,
}: Readonly<{ input: DashboardInput; value: string; onChange: (values: string[]) => void }>) {
  const [draft, setDraft] = useState(value);
  function commit() {
    const text = draft.trim();
    if (text !== value) onChange(text === "" ? [] : [text]);
  }
  return (
    <InputGroup className="w-56">
      <InputGroupAddon>
        <InputGroupText>{input.title}</InputGroupText>
      </InputGroupAddon>
      <InputGroupInput
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") commit();
        }}
        placeholder="Any"
        aria-label={input.title}
      />
    </InputGroup>
  );
}
