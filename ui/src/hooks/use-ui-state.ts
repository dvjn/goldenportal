import { useCallback, useMemo, useSyncExternalStore } from "react";
import type { Theme } from "@/components/theme-provider";

// UiState is each viewer's own choices about how the app looks, kept in
// this browser. Every key is listed here with its type, so a key never
// holds two shapes, and what's stored is all in one place.
export type UiState = {
  theme: Theme;
  sidebarOpen: boolean;
  // pageSize is how many rows a table shows, unless its URL says.
  pageSize: number;
  // entityTabs are the tabs last opened on entity pages, by blueprint.
  entityTabs: Record<string, string>;
};

const prefix = "gp-ui-";

// memory stands in for storage when it's blocked, as in some private
// windows, so state still works in that tab until a reload.
const memory = new Map<string, string>();
const listeners = new Set<() => void>();

function readRaw(key: keyof UiState): string | null {
  try {
    return localStorage.getItem(prefix + key);
  } catch {
    return memory.get(key) ?? null;
  }
}

// subscribe hears this tab's changes and other tabs' through storage events.
function subscribe(onChange: () => void) {
  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key.startsWith(prefix)) onChange();
  };
  listeners.add(onChange);
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(onChange);
    window.removeEventListener("storage", onStorage);
  };
}

// readUiState reads a piece of UI state outside a component, such as in a
// route's loader. It doesn't rerender on change; useUiState does.
export function readUiState<K extends keyof UiState>(key: K, fallback: UiState[K]): UiState[K] {
  return parse(readRaw(key), fallback);
}

// writeUiState sets a piece of UI state, for every component using it.
export function writeUiState<K extends keyof UiState>(key: K, value: UiState[K]) {
  const text = JSON.stringify(value);
  try {
    localStorage.setItem(prefix + key, text);
  } catch {
    memory.set(key, text);
  }
  for (const listener of listeners) listener();
}

// useUiState is a piece of UI state that outlasts a reload, like useState
// with a key: every component using the same key shares one value.
export function useUiState<K extends keyof UiState>(
  key: K,
  fallback: UiState[K],
): readonly [UiState[K], (value: UiState[K]) => void] {
  // The snapshot is the stored text, which stays equal between reads; the
  // value parsed from it is what's handed out.
  const raw = useSyncExternalStore(
    subscribe,
    () => readRaw(key),
    () => null,
  );
  const value = useMemo(() => parse(raw, fallback), [raw, fallback]);

  const setValue = useCallback((next: UiState[K]) => writeUiState(key, next), [key]);

  return [value, setValue] as const;
}

function parse<T>(raw: string | null, fallback: T): T {
  if (raw === null) return fallback;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}
