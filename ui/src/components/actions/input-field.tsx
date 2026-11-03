import { X } from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { Property } from "@/features/blueprints";
import { EntityPicker } from "./entity-picker";
import { controlFor, type Draft } from "./input-values";

type InputFieldProps = {
  input: Property;
  draft: Draft;
  onChange: (draft: Draft) => void;
  problems?: string[];
};

// InputField is one input of an action's form: its label, marked when
// required, the control its type asks for, its description and what's
// wrong with what was entered.
export function InputField({ input, draft, onChange, problems = [] }: InputFieldProps) {
  const id = `input-${input.identifier}`;
  const invalid = problems.length > 0;
  return (
    <Field data-invalid={invalid || undefined}>
      <FieldLabel htmlFor={id}>
        {input.title}
        {input.required && (
          <span className="text-destructive" title="Required" aria-hidden>
            *
          </span>
        )}
      </FieldLabel>
      <Control id={id} input={input} draft={draft} onChange={onChange} invalid={invalid} />
      {input.description && <FieldDescription>{input.description}</FieldDescription>}
      <FieldError errors={problems.map((problem) => ({ message: problem }))} />
    </Field>
  );
}

// range writes the bounds of a number, as blueprints show them.
function range(min?: number, max?: number) {
  if (min !== undefined && max !== undefined) return `${min}-${max}`;
  if (min !== undefined) return `≥ ${min}`;
  if (max !== undefined) return `≤ ${max}`;
  return undefined;
}

function Control({
  id,
  input,
  draft,
  onChange,
  invalid,
}: Readonly<{
  id: string;
  input: Property;
  draft: Draft;
  onChange: (draft: Draft) => void;
  invalid: boolean;
}>) {
  const text = typeof draft === "string" ? draft : "";
  const values = Array.isArray(draft) ? draft : [];
  const common = {
    id,
    "aria-invalid": invalid || undefined,
    "aria-required": input.required || undefined,
  };
  const textProps = {
    ...common,
    value: text,
    onChange: (event: { target: { value: string } }) =>
      onChange(event.target.value === "" ? undefined : event.target.value),
    minLength: input.minLength,
    maxLength: input.maxLength,
  };

  switch (controlFor(input)) {
    case "relation":
      return (
        <EntityPicker
          {...common}
          blueprint={input.relation ?? ""}
          many={input.many ?? false}
          value={typeof draft === "string" ? [draft] : values}
          onChange={(next) => onChange(next.length > 0 ? next : undefined)}
          invalid={invalid}
          required={input.required}
        />
      );
    case "enum":
      return (
        <div className="flex items-center gap-2">
          <Select value={text} onValueChange={(next) => onChange(next || undefined)}>
            <SelectTrigger {...common} className="w-full">
              <SelectValue placeholder="Choose…" />
            </SelectTrigger>
            <SelectContent>
              {(input.enum ?? []).map((value) => (
                <SelectItem key={String(value)} value={String(value)}>
                  {String(value)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {!input.required && text !== "" && (
            <IconButton label={`Clear ${input.title}`} onClick={() => onChange(undefined)}>
              <X />
            </IconButton>
          )}
        </div>
      );
    case "boolean":
      return (
        <ToggleGroup
          {...common}
          type="single"
          variant="outline"
          spacing={0}
          value={text}
          onValueChange={(next) => onChange(next || undefined)}
          aria-label={input.title}
          className="w-fit"
        >
          <ToggleGroupItem value="true">Yes</ToggleGroupItem>
          <ToggleGroupItem value="false">No</ToggleGroupItem>
        </ToggleGroup>
      );
    case "picks":
      return (
        <ToggleGroup
          {...common}
          type="multiple"
          variant="outline"
          value={values}
          onValueChange={(next) => onChange(next.length > 0 ? next : undefined)}
          aria-label={input.title}
          className="w-fit flex-wrap"
        >
          {(input.items?.enum ?? []).map((value) => (
            <ToggleGroupItem key={String(value)} value={String(value)}>
              {String(value)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      );
    case "number":
      return (
        <Input
          {...textProps}
          type="number"
          min={input.minimum}
          max={input.maximum}
          step={input.type === "integer" ? 1 : "any"}
          placeholder={range(input.minimum, input.maximum)}
          className="tabular-nums"
        />
      );
    case "date":
      return <Input {...textProps} type="date" />;
    case "date-time":
      return <Input {...textProps} type="datetime-local" />;
    case "url":
      return <Input {...textProps} type="url" placeholder="https://" />;
    case "email":
      return <Input {...textProps} type="email" />;
    case "markdown":
      return <Textarea {...textProps} rows={4} />;
    case "list":
      return <Textarea {...textProps} rows={3} placeholder="One per line" />;
    case "json":
      return <Textarea {...textProps} rows={4} className="font-mono text-xs" />;
    default:
      return <Input {...textProps} />;
  }
}
