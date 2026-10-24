import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { CircleCheck, CircleX, Loader2, TriangleAlert } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import type { ConfigDocs, Issue } from "@/features/config-docs";

function useDebounced<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

// useLiveValidation checks the source on the server shortly after typing stops.
export function useLiveValidation<S>(docs: ConfigDocs<S>, source: string) {
  const debounced = useDebounced(source, 400);
  const query = useQuery({
    ...docs.queries.validation(debounced),
    enabled: debounced.trim() !== "",
    placeholderData: keepPreviousData,
  });
  const settled = debounced === source && !query.isFetching;

  return { validation: query.data, checking: !settled, error: query.error };
}

type DocEditorProps = {
  value: string;
  onChange: (value: string) => void;
  issues: Issue[];
  checking: boolean;
};

// DocEditor edits a document's YAML and lists the problems found in it.
export function DocEditor({ value, onChange, issues, checking }: DocEditorProps) {
  const textarea = useRef<HTMLTextAreaElement>(null);

  function goToLine(line: number) {
    const element = textarea.current;
    if (!element) return;
    const lines = value.split("\n");
    const start = lines.slice(0, line - 1).reduce((offset, text) => offset + text.length + 1, 0);
    element.focus();
    element.setSelectionRange(start, start + (lines[line - 1]?.length ?? 0));
  }

  return (
    <div className="grid gap-3">
      <Textarea
        ref={textarea}
        aria-label="YAML"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        className="min-h-112 resize-y font-mono text-xs leading-5"
      />
      <IssueList issues={issues} checking={checking} onGoToLine={goToLine} />
    </div>
  );
}

function IssueList({
  issues,
  checking,
  onGoToLine,
}: Readonly<{ issues: Issue[]; checking: boolean; onGoToLine: (line: number) => void }>) {
  if (checking) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden />
        Checking…
      </p>
    );
  }
  if (issues.length === 0) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <CircleCheck className="size-4 text-emerald-600 dark:text-emerald-400" aria-hidden />
        No problems found
      </p>
    );
  }

  return (
    <ul className="grid gap-1.5" aria-label="Problems">
      {issues.map((issue) => (
        <li
          key={`${issue.severity}:${issue.line}:${issue.path}:${issue.message}`}
          className="flex items-start gap-2 text-sm"
        >
          {issue.severity === "error" ? (
            <CircleX className="mt-0.5 size-4 shrink-0 text-destructive" aria-label="Error" />
          ) : (
            <TriangleAlert
              className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400"
              aria-label="Warning"
            />
          )}
          {issue.line ? (
            <Badge asChild variant="outline" className="shrink-0 font-mono">
              <button type="button" onClick={() => onGoToLine(issue.line ?? 1)}>
                Line {issue.line}
              </button>
            </Badge>
          ) : null}
          <span>
            {issue.path && (
              <code className="mr-1.5 font-mono text-xs text-muted-foreground">{issue.path}</code>
            )}
            {issue.message}
          </span>
        </li>
      ))}
    </ul>
  );
}
