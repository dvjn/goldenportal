import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ChevronDown, Download, FileClock, Plus, Upload } from "lucide-react";
import { useState } from "react";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  create,
  type DefinitionDocument,
  type Draft,
  definitionsQueryOptions,
  discardAll,
  edit,
  exportDefinitions,
  heading,
  type Kind,
  LazyYamlEditor,
  type Plan,
  PlanDialog,
  type Problem,
  ProblemList,
  planRequest,
  problemsOf,
  refKey,
  remove,
  revert,
  schemaQueryOptions,
  UploadDialog,
  useDefinitions,
  useDrafts,
  usePlanDefinitions,
} from "@/features/definitions";

export const Route = createFileRoute("/_authenticated/definitions/")({
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(definitionsQueryOptions),
      context.queryClient.ensureQueryData(schemaQueryOptions),
    ]),
  component: DefinitionsPage,
});

const kinds: { kind: Kind; title: string }[] = [
  { kind: "Blueprint", title: "Blueprints" },
  { kind: "Integration", title: "Integrations" },
];

type Item = {
  key: string;
  kind: string;
  identifier: string;
  title: string;
  document?: DefinitionDocument;
  draft?: Draft;
};

function items(documents: DefinitionDocument[], drafts: Draft[]): Item[] {
  const byKey = new Map(drafts.map((draft) => [draft.key, draft]));
  const existing = documents.map((document) => ({
    key: refKey(document),
    kind: document.kind,
    identifier: document.identifier,
    title: document.title,
    document,
    draft: byKey.get(refKey(document)),
  }));
  const created = drafts
    .filter((draft) => !draft.original)
    .map((draft) => {
      const declared = heading(draft.text);
      return {
        key: draft.key,
        kind: declared.kind ?? "",
        identifier: declared.identifier ?? "",
        title: declared.title ?? "Untitled",
        draft,
      };
    });

  return [...existing, ...created];
}

// groups lists the documents by kind. A new draft whose kind is not one of
// the kinds yet stays reachable under Other.
function groups(list: Item[]) {
  const known = kinds.map(({ kind, title }) => ({
    title,
    items: list.filter((item) => item.kind === kind),
  }));
  const other = list.filter((item) => !kinds.some(({ kind }) => kind === item.kind));

  return other.length > 0 ? [...known, { title: "Other", items: other }] : known;
}

function status(item: Item) {
  if (!item.draft) return undefined;
  if (item.draft.removed) return "Removed";
  return item.document ? "Edited" : "New";
}

type Planned = { plan: Plan; fromDrafts: boolean };

function DefinitionsPage() {
  const definitions = useDefinitions();
  const schema = useSuspenseQuery(schemaQueryOptions).data;
  const drafts = useDrafts();
  const list = items(definitions.documents, drafts);
  const [selectedKey, setSelectedKey] = useState(list[0]?.key);
  const selected = list.find((item) => item.key === selectedKey) ?? list[0];
  const [planned, setPlanned] = useState<Planned>();
  const [uploading, setUploading] = useState(false);
  const [applied, setApplied] = useState<number>();
  const plan = usePlanDefinitions();
  const { request, parts } = planRequest(drafts);
  const problems = problemsOf(plan.error);
  const located = locate(problems, parts);

  function planDrafts() {
    setApplied(undefined);
    plan.mutate(request, { onSuccess: (result) => setPlanned({ plan: result, fromDrafts: true }) });
  }

  function newDocument(kind: Kind) {
    setSelectedKey(create(kind));
  }

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold">Definitions</h1>
          <p className="text-muted-foreground text-sm">
            The blueprints and integrations that shape the catalog, at revision{" "}
            {definitions.revision}. Your changes stay in this browser until you apply them.
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline">
                <Plus />
                New
                <ChevronDown />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {kinds.map(({ kind }) => (
                <DropdownMenuItem key={kind} onSelect={() => newDocument(kind)}>
                  {kind}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="outline" onClick={() => setUploading(true)}>
            <Upload />
            Upload YAML
          </Button>
          <Button variant="outline" onClick={download}>
            <Download />
            Export
          </Button>
          <Button variant="outline" asChild>
            <Link to="/definitions/history">
              <FileClock />
              History
            </Link>
          </Button>
        </div>
      </header>
      {applied !== undefined && (
        <Alert>
          <AlertTitle>Applied revision {applied}.</AlertTitle>
          <AlertAction>
            <Button size="sm" variant="outline" asChild>
              <Link to="/definitions/history/$revision" params={{ revision: applied }}>
                View revision {applied}
              </Link>
            </Button>
          </AlertAction>
        </Alert>
      )}
      {drafts.length > 0 && (
        <Alert>
          <AlertTitle>
            {drafts.length === 1 ? "1 document changed" : `${drafts.length} documents changed`}
          </AlertTitle>
          <AlertDescription>
            Plan the changes to see what they do before you apply them.
          </AlertDescription>
          <AlertAction className="flex gap-2">
            <Button size="sm" variant="outline" onClick={discardAll}>
              Discard
            </Button>
            <Button size="sm" disabled={plan.isPending} onClick={planDrafts}>
              Plan
            </Button>
          </AlertAction>
        </Alert>
      )}
      {plan.isError && (
        <Alert variant="destructive">
          <AlertTitle>{plan.error.message}</AlertTitle>
          {problems.length > 0 && (
            <AlertDescription>
              <ProblemList problems={located.unplaced} />
              {located.placed > 0 && <p>The editor marks the problems at their lines.</p>}
            </AlertDescription>
          )}
        </Alert>
      )}
      <div className="flex flex-col gap-6 md:flex-row">
        <nav aria-label="Documents" className="flex shrink-0 flex-col gap-4 md:w-64">
          {groups(list).map((group) => (
            <section key={group.title} className="flex flex-col gap-1">
              <h2 className="px-3 text-sm font-semibold">{group.title}</h2>
              {group.items.map((item) => (
                <Button
                  key={item.key}
                  variant={item.key === selected?.key ? "secondary" : "ghost"}
                  className="h-auto justify-between py-1.5"
                  onClick={() => setSelectedKey(item.key)}
                >
                  <span className="flex min-w-0 flex-col items-start">
                    <span className="truncate">{item.title}</span>
                    <span className="text-muted-foreground truncate font-mono text-xs">
                      {item.identifier}
                    </span>
                  </span>
                  {status(item) && <Badge variant="outline">{status(item)}</Badge>}
                </Button>
              ))}
            </section>
          ))}
        </nav>
        {selected ? (
          <section aria-label={selected.title} className="flex min-w-0 flex-1 flex-col gap-2">
            <div className="flex items-center justify-between gap-2">
              <h2 className="font-semibold">{selected.title}</h2>
              <DocumentActions item={selected} />
            </div>
            <div className="h-[60svh] overflow-hidden rounded-lg border">
              <LazyYamlEditor
                path={`file:///definitions/${selected.key}.yaml`}
                value={selected.draft?.text ?? selected.document?.yaml ?? ""}
                readOnly={selected.draft?.removed ?? false}
                markers={located.markers[selected.key] ?? []}
                schema={schema}
                onChange={(text) => edit(selected.key, text, selected.document)}
              />
            </div>
          </section>
        ) : (
          <p className="text-muted-foreground text-sm">
            No definitions yet. Start with New or Upload YAML.
          </p>
        )}
      </div>
      <UploadDialog
        open={uploading}
        onOpenChange={setUploading}
        onPlanned={(result) => {
          setApplied(undefined);
          setPlanned({ plan: result, fromDrafts: false });
        }}
      />
      <PlanDialog
        plan={planned?.plan}
        onOpenChange={(open) => !open && setPlanned(undefined)}
        onApplied={(revision) => {
          if (planned?.fromDrafts) discardAll();
          plan.reset();
          setApplied(revision.number);
        }}
        onReplan={() => (planned?.fromDrafts ? planDrafts() : setUploading(true))}
      />
    </>
  );
}

function DocumentActions({ item }: { item: Item }) {
  if (item.draft?.removed) {
    return (
      <Button size="sm" variant="outline" onClick={() => revert(item.key)}>
        Restore
      </Button>
    );
  }

  return (
    <div className="flex gap-2">
      {item.draft && item.document && (
        <Button size="sm" variant="outline" onClick={() => revert(item.key)}>
          Revert changes
        </Button>
      )}
      <Button size="sm" variant="outline" onClick={() => remove(item.key, item.document)}>
        Remove
      </Button>
    </div>
  );
}

type Located = {
  markers: Record<string, { line: number; column: number; message: string }[]>;
  unplaced: Problem[];
  placed: number;
};

// locate places each problem in the draft it came from, at the line within
// that draft; a problem in a document no draft holds stays in the list.
function locate(problems: Problem[], parts: ReturnType<typeof planRequest>["parts"]): Located {
  const located: Located = { markers: {}, unplaced: [], placed: 0 };
  for (const problem of problems) {
    const part = parts.find(
      (candidate) =>
        problem.document !== undefined &&
        problem.document >= candidate.firstDocument &&
        problem.document < candidate.firstDocument + candidate.documents,
    );
    if (!part || !problem.line) {
      located.unplaced.push(problem);
      continue;
    }
    const prefix = problem.path ? `${problem.path}: ` : "";
    located.markers[part.key] = [
      ...(located.markers[part.key] ?? []),
      {
        line: problem.line - part.firstLine + 1,
        column: problem.column ?? 1,
        message: prefix + problem.message,
      },
    ];
    located.placed += 1;
  }

  return located;
}

async function download() {
  const exported = await exportDefinitions();
  const link = document.createElement("a");
  link.href = URL.createObjectURL(new Blob([exported.yaml], { type: "application/yaml" }));
  link.download = `definitions-revision-${exported.revision}.yaml`;
  link.click();
  URL.revokeObjectURL(link.href);
}
