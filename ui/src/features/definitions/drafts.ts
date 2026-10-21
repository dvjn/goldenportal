import { useSyncExternalStore } from "react";
import type { DefinitionDocument, Kind, Ref } from "./api";

// A draft is a document changed in the browser and not applied yet. A draft
// of an existing document keeps the document it started from in original.
export type Draft = {
  key: string;
  original?: Ref;
  text: string;
  removed: boolean;
};

const storageKey = "goldport-definition-drafts";

export const templates: Record<Kind, string> = {
  Blueprint: `kind: Blueprint
identifier: new-blueprint
title: New blueprint
icon: box
properties:
  name:
    title: Name
    type: string
`,
  Integration: `kind: Integration
identifier: new-integration
title: New integration
connector: push
`,
};

function load(): Draft[] {
  try {
    const stored = JSON.parse(localStorage.getItem(storageKey) ?? "[]");
    return Array.isArray(stored) ? stored : [];
  } catch {
    return [];
  }
}

let drafts: Draft[] = load();
const listeners = new Set<() => void>();

function publish(next: Draft[]) {
  drafts = next;
  try {
    localStorage.setItem(storageKey, JSON.stringify(next));
  } catch {
    // The drafts still live in memory for this tab.
  }
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useDrafts() {
  return useSyncExternalStore(subscribe, () => drafts);
}

export function refKey(ref: Ref) {
  return `${ref.kind}/${ref.identifier}`;
}

function without(key: string) {
  return drafts.filter((draft) => draft.key !== key);
}

// edit keeps the text as a draft, or drops the draft when the text is back
// to the document as applied.
export function edit(key: string, text: string, document?: DefinitionDocument) {
  if (document && text === document.yaml) {
    publish(without(key));
    return;
  }
  const existing = drafts.find((draft) => draft.key === key);
  const draft: Draft = {
    key,
    original: document ? { kind: document.kind, identifier: document.identifier } : undefined,
    removed: false,
    ...existing,
    text,
  };
  publish(
    existing ? drafts.map((other) => (other.key === key ? draft : other)) : [...drafts, draft],
  );
}

export function create(kind: Kind) {
  const key = `new/${Date.now()}`;
  publish([...drafts, { key, text: templates[kind], removed: false }]);
  return key;
}

export function remove(key: string, document?: DefinitionDocument) {
  if (!document) {
    publish(without(key));
    return;
  }
  const existing = drafts.find((draft) => draft.key === key);
  publish([
    ...without(key),
    {
      key,
      original: { kind: document.kind, identifier: document.identifier },
      text: existing?.text ?? document.yaml,
      removed: true,
    },
  ]);
}

export function revert(key: string) {
  publish(without(key));
}

export function discardAll() {
  publish([]);
}

// heading reads the kind, identifier and title a draft's text declares at
// its top level, for labels in the document list.
export function heading(text: string) {
  const field = (name: string) => text.match(new RegExp(`^${name}:\\s*(.+?)\\s*$`, "m"))?.[1];
  return { kind: field("kind"), identifier: field("identifier"), title: field("title") };
}

export type PlannedDraft = {
  key: string;
  firstLine: number;
  firstDocument: number;
  documents: number;
};

// planRequest joins the drafts into one source. Every draft of an existing
// document removes the original, so a draft that renames a document drops
// the old one, and one that keeps the name replaces it.
export function planRequest(pending: Draft[]) {
  const parts: PlannedDraft[] = [];
  const texts: string[] = [];
  let line = 1;
  let document = 1;
  for (const draft of pending.filter((draft) => !draft.removed)) {
    const text = `${draft.text.replace(/\s+$/, "")}\n`;
    const documents = 1 + (text.match(/^---\s*$/gm)?.length ?? 0);
    parts.push({ key: draft.key, firstLine: line, firstDocument: document, documents });
    texts.push(text);
    line += text.split("\n").length;
    document += documents;
  }
  const remove = pending.flatMap((draft) => (draft.original ? [draft.original] : []));

  return { request: { source: texts.join("---\n"), partial: true, remove }, parts };
}
