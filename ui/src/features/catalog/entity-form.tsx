import { type FormEvent, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  type Blueprint,
  type Entity,
  type EntityName,
  fieldErrors,
  type PropertyView,
} from "./api";
import { EntityPicker } from "./entity-picker";
import { useSaveEntity } from "./queries";

type Raw = string | boolean;

type Draft = {
  identifier: string;
  title: string;
  properties: Record<string, Raw>;
  relations: Record<string, EntityName[]>;
};

const notSet = "__not_set";

const inputTypes: Record<string, string> = {
  url: "url",
  email: "email",
  date: "date",
  "date-time": "datetime-local",
};

function pad(value: number) {
  return String(value).padStart(2, "0");
}

function localDateTime(iso: string) {
  const date = new Date(iso);
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function rawValue(property: PropertyView, value: unknown): Raw {
  if (property.type === "boolean") return value === true;
  if (value === undefined || value === null) return "";
  switch (property.type) {
    case "array":
      return (value as unknown[]).join(", ");
    case "object":
      return JSON.stringify(value, null, 2);
  }
  return property.format === "date-time" ? localDateTime(String(value)) : String(value);
}

class InputError extends Error {}

function parseJSON(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

// entityValue turns what the person typed into the value the API takes;
// an empty input means the entity has no value for the property.
function entityValue(property: PropertyView, raw: Raw): unknown {
  if (typeof raw === "boolean") return raw;
  if (raw.trim() === "") return null;
  switch (property.type) {
    case "number": {
      const number = Number(raw);
      if (Number.isNaN(number)) throw new InputError("Enter a number.");
      return number;
    }
    case "array":
      return raw
        .split(",")
        .map((item) => item.trim())
        .filter(Boolean);
    case "object": {
      const parsed = parseJSON(raw);
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        throw new InputError('Enter a JSON object, such as {"key": "value"}.');
      }
      return parsed;
    }
  }
  return property.format === "date-time" ? new Date(raw).toISOString() : raw;
}

function initialDraft(blueprint: Blueprint, entity?: Entity): Draft {
  return {
    identifier: entity?.identifier ?? "",
    title: entity?.title ?? "",
    properties: Object.fromEntries(
      blueprint.properties.map((property) => [
        property.key,
        rawValue(property, entity?.properties[property.key]),
      ]),
    ),
    relations: Object.fromEntries(
      blueprint.relations.map((relation) => [
        relation.key,
        entity?.relations.find((group) => group.key === relation.key)?.entities ?? [],
      ]),
    ),
  };
}

type EntityFormProps = {
  blueprint: Blueprint;
  entity?: Entity;
  onSaved: (entity: Entity) => void;
  onCancel: () => void;
};

export function EntityForm({ blueprint, entity, onSaved, onCancel }: EntityFormProps) {
  const [draft, setDraft] = useState(() => initialDraft(blueprint, entity));
  const [inputErrors, setInputErrors] = useState<Record<string, string>>({});
  const save = useSaveEntity(blueprint.identifier, entity !== undefined);
  const errors = { ...fieldErrors(save.error), ...inputErrors };
  const unexplained = save.isError && Object.keys(fieldErrors(save.error)).length === 0;

  function setProperty(key: string, raw: Raw) {
    setDraft((previous) => ({ ...previous, properties: { ...previous.properties, [key]: raw } }));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const properties: Record<string, unknown> = {};
    const problems: Record<string, string> = {};
    for (const property of blueprint.properties) {
      try {
        properties[property.key] = entityValue(property, draft.properties[property.key] ?? "");
      } catch (error) {
        if (!(error instanceof InputError)) throw error;
        problems[`properties.${property.key}`] = error.message;
      }
    }
    setInputErrors(problems);
    if (Object.keys(problems).length > 0) return;

    save.mutate(
      {
        identifier: draft.identifier.trim(),
        title: draft.title,
        properties,
        relations: Object.fromEntries(
          Object.entries(draft.relations).map(([key, targets]) => [
            key,
            targets.map((target) => target.identifier),
          ]),
        ),
      },
      { onSuccess: onSaved },
    );
  }

  return (
    <form noValidate onSubmit={submit} className="flex max-w-2xl flex-col gap-6">
      <FieldGroup>
        <Field data-invalid={Boolean(errors.title)}>
          <FieldLabel htmlFor="entity-title">Title</FieldLabel>
          <Input
            id="entity-title"
            aria-invalid={Boolean(errors.title)}
            value={draft.title}
            onChange={(event) => setDraft({ ...draft, title: event.target.value })}
          />
          <FieldError>{errors.title}</FieldError>
        </Field>
        <Field data-invalid={Boolean(errors.identifier)}>
          <FieldLabel htmlFor="entity-identifier">Identifier</FieldLabel>
          <Input
            id="entity-identifier"
            aria-invalid={Boolean(errors.identifier)}
            disabled={entity !== undefined}
            className="font-mono"
            value={draft.identifier}
            onChange={(event) => setDraft({ ...draft, identifier: event.target.value })}
          />
          <FieldDescription>
            {entity
              ? "The identifier never changes."
              : "Letters, digits, dots, hyphens and underscores. It never changes."}
          </FieldDescription>
          <FieldError>{errors.identifier}</FieldError>
        </Field>
        {blueprint.properties.map((property) => (
          <PropertyField
            key={property.key}
            property={property}
            raw={draft.properties[property.key] ?? ""}
            error={errors[`properties.${property.key}`]}
            onChange={(raw) => setProperty(property.key, raw)}
          />
        ))}
        {blueprint.relations.map((relation) => {
          const error = errors[`relations.${relation.key}`];
          return (
            <Field key={relation.key} data-invalid={Boolean(error)}>
              <FieldLabel htmlFor={`relation-${relation.key}`}>{relation.title}</FieldLabel>
              <EntityPicker
                id={`relation-${relation.key}`}
                relation={relation}
                invalid={Boolean(error)}
                value={draft.relations[relation.key] ?? []}
                onChange={(targets) =>
                  setDraft({ ...draft, relations: { ...draft.relations, [relation.key]: targets } })
                }
              />
              {relation.description && <FieldDescription>{relation.description}</FieldDescription>}
              <FieldError>{error}</FieldError>
            </Field>
          );
        })}
        {unexplained && <FieldError>{save.error?.message}</FieldError>}
      </FieldGroup>
      <div className="flex gap-2">
        <Button type="submit" disabled={save.isPending}>
          {entity ? "Save changes" : "Create entity"}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

type PropertyFieldProps = {
  property: PropertyView;
  raw: Raw;
  error?: string;
  onChange: (raw: Raw) => void;
};

function PropertyField({ property, raw, error, onChange }: PropertyFieldProps) {
  const id = `property-${property.key}`;
  const invalid = Boolean(error);
  const hint = property.type === "array" ? "Separate the values with commas." : undefined;

  return (
    <Field
      data-invalid={invalid}
      orientation={property.type === "boolean" ? "horizontal" : undefined}
    >
      {property.type === "boolean" ? (
        <Checkbox
          id={id}
          aria-invalid={invalid}
          checked={raw === true}
          onCheckedChange={(checked) => onChange(checked === true)}
        />
      ) : null}
      <FieldLabel htmlFor={id}>{property.title}</FieldLabel>
      {property.type !== "boolean" && (
        <PropertyInput
          id={id}
          property={property}
          raw={String(raw)}
          invalid={invalid}
          onChange={onChange}
        />
      )}
      {(property.description || hint) && (
        <FieldDescription>
          {[property.description, hint].filter(Boolean).join(" ")}
        </FieldDescription>
      )}
      <FieldError>{error}</FieldError>
    </Field>
  );
}

type PropertyInputProps = {
  id: string;
  property: PropertyView;
  raw: string;
  invalid: boolean;
  onChange: (raw: string) => void;
};

function PropertyInput({ id, property, raw, invalid, onChange }: PropertyInputProps) {
  if (property.enum && property.type === "string") {
    return (
      <Select
        value={raw || notSet}
        onValueChange={(value) => onChange(value === notSet ? "" : value)}
      >
        <SelectTrigger id={id} aria-invalid={invalid}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={notSet}>Not set</SelectItem>
          {property.enum.map((value) => (
            <SelectItem key={value} value={value}>
              {value}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  }
  if (property.type === "object") {
    return (
      <Textarea
        id={id}
        aria-invalid={invalid}
        className="font-mono"
        value={raw}
        onChange={(event) => onChange(event.target.value)}
      />
    );
  }

  return (
    <Input
      id={id}
      aria-invalid={invalid}
      type={property.type === "number" ? "number" : (inputTypes[property.format ?? ""] ?? "text")}
      placeholder={
        property.type === "array" && property.enum ? property.enum.join(", ") : undefined
      }
      value={raw}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}
