import type { Property } from "@/features/blueprints";

// Control is how an input is entered, picked by its type, format and values.
export type Control =
  | "relation"
  | "enum"
  | "boolean"
  | "number"
  | "date"
  | "date-time"
  | "markdown"
  | "url"
  | "email"
  | "text"
  | "picks"
  | "list"
  | "json";

export function controlFor(input: Property): Control {
  if (input.relation) return "relation";
  if (input.enum?.length) return "enum";
  switch (input.type) {
    case "boolean":
      return "boolean";
    case "number":
    case "integer":
      return "number";
    case "array":
      return input.items?.enum?.length ? "picks" : "list";
    case "object":
      return "json";
  }
  switch (input.format) {
    case "date":
      return "date";
    case "date-time":
      return "date-time";
    case "markdown":
      return "markdown";
    case "uri":
      return "url";
    case "email":
      return "email";
  }
  return "text";
}

// Draft is what a control holds: text, or the values picked. Nothing
// entered is undefined, so the input is left out rather than sent empty.
export type Draft = string | string[] | undefined;

const pad = (n: number) => String(n).padStart(2, "0");

// localDateTime writes a moment as a datetime-local control shows it.
function localDateTime(value: string) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// toDraft turns a value given before, such as a run's, into what its
// control holds.
export function toDraft(input: Property, value: unknown): Draft {
  if (value === undefined || value === null) return undefined;
  const control = controlFor(input);
  switch (control) {
    case "relation":
      if (input.many) return Array.isArray(value) ? value.map(String) : [String(value)];
      return String(value);
    case "picks":
      return Array.isArray(value) ? value.map(String) : undefined;
    case "list":
      return Array.isArray(value) ? value.map(String).join("\n") : undefined;
    case "json":
      return JSON.stringify(value, null, 2);
    case "date-time":
      return localDateTime(String(value));
    default:
      return String(value);
  }
}

// Parsed is an input's value to send, or what's wrong with what was entered.
type Parsed = { value?: unknown; problem?: string };

function number(text: string): Parsed {
  const n = Number(text);
  if (text.trim() === "" || Number.isNaN(n)) return { problem: "must be a number" };
  return { value: n };
}

// enumValue finds the declared value a control's text stands for, keeping
// its type.
function enumValue(values: (string | number)[], text: string) {
  return values.find((value) => String(value) === text) ?? text;
}

// fromDraft turns what a control holds into the input's value. Empty
// controls give no value.
export function fromDraft(input: Property, draft: Draft): Parsed {
  if (draft === undefined) return {};
  if (Array.isArray(draft)) {
    if (draft.length === 0) return {};
    if (input.relation) return { value: input.many ? draft : draft[0] };
    return { value: draft.map((text) => enumValue(input.items?.enum ?? [], text)) };
  }
  if (draft.trim() === "") return {};
  switch (controlFor(input)) {
    case "relation":
      return { value: input.many ? [draft] : draft };
    case "enum":
      return { value: enumValue(input.enum ?? [], draft) };
    case "boolean":
      return { value: draft === "true" };
    case "number":
      return number(draft);
    case "date-time": {
      const d = new Date(draft);
      if (Number.isNaN(d.getTime())) return { problem: "must be a date and time" };
      return { value: d.toISOString().replace(/\.000Z$/, "Z") };
    }
    case "list": {
      const lines = draft
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line !== "");
      const itemType = input.items?.type;
      if (itemType === "number" || itemType === "integer") {
        const items = lines.map(number);
        const wrong = items.findIndex((item) => item.problem);
        if (wrong >= 0) return { problem: `item ${wrong + 1} must be a number` };
        return { value: items.map((item) => item.value) };
      }
      if (itemType === "boolean") return { value: lines.map((line) => line === "true") };
      return { value: lines };
    }
    case "json":
      try {
        return { value: JSON.parse(draft) };
      } catch {
        return { problem: 'must be JSON, such as {"key": "value"}' };
      }
    default:
      return { value: draft };
  }
}
