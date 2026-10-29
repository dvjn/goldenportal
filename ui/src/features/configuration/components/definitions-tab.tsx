import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronDown, Download, FileClock, Plus, Upload } from "lucide-react";
import { useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { StateCard } from "@/components/common/state-card";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Blueprint } from "@/features/catalog";
import {
  create,
  type DefinitionDocument,
  type Draft,
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
import type { IntegrationSummary } from "@/features/integrations";
import { type DocumentFacts, documentFacts } from "../document-counts";
import { entityCountLabel } from "../format";
import { BlueprintTile, IntegrationIcon } from "./icons";
import { DetailHeader, MasterDetail, SidebarHeading, SidebarItem } from "./master-detail";

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

function ItemIcon({ item, facts }: Readonly<{ item: Item; facts: DocumentFacts | undefined }>) {
  if (item.kind === "Integration") {
    return (
      <span className="inline-flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted/50">
        <IntegrationIcon icon={facts?.icon ?? ""} className="size-3.5" />
      </span>
    );
  }
  return <BlueprintTile icon={facts?.icon} />;
}

type Planned = { plan: Plan; fromDrafts: boolean };

export function DefinitionsTab({
  blueprints,
  integrations,
  selected: requested,
  onSelect,
}: Readonly<{
  blueprints: Blueprint[] | undefined;
  integrations: IntegrationSummary[] | undefined;
  selected: string | undefined;
  onSelect: (key: string) => void;
}>) {
  const definitions = useDefinitions();
  const schema = useQuery(schemaQueryOptions);

  if (definitions.isError || schema.isError) {
    return (
      <StateCard
        title="Definitions unavailable"
        message="We could not load the definitions. Try again."
      />
    );
  }
  if (!definitions.data || !schema.data) return <ListSkeleton />;

  return (
    <DefinitionsEditor
      documents={definitions.data.documents}
      schema={schema.data}
      facts={documentFacts(blueprints, integrations)}
      requested={requested}
      onSelect={onSelect}
    />
  );
}

function DefinitionsEditor({
  documents,
  schema,
  facts,
  requested,
  onSelect,
}: Readonly<{
  documents: DefinitionDocument[];
  schema: object;
  facts: Map<string, DocumentFacts>;
  requested: string | undefined;
  onSelect: (key: string) => void;
}>) {
  const drafts = useDrafts();
  const list = items(documents, drafts);
  const selected = list.find((item) => item.key === requested) ?? list[0];
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

  return (
    <>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm text-muted-foreground">
          Your changes stay in this browser until you apply them.
        </p>
        <div className="flex shrink-0 flex-wrap gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <Plus />
                New
                <ChevronDown />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {kinds.map(({ kind }) => (
                <DropdownMenuItem key={kind} onSelect={() => onSelect(create(kind))}>
                  {kind}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="outline" size="sm" onClick={() => setUploading(true)}>
            <Upload />
            Upload YAML
          </Button>
          <Button variant="outline" size="sm" onClick={download}>
            <Download />
            Export
          </Button>
          <Button variant="outline" size="sm" asChild>
            <Link to="/settings/catalog-setup/history">
              <FileClock />
              History
            </Link>
          </Button>
        </div>
      </div>
      {applied !== undefined && (
        <Alert>
          <AlertTitle>Applied revision {applied}.</AlertTitle>
          <AlertAction>
            <Button size="sm" variant="outline" asChild>
              <Link to="/settings/catalog-setup/history/$revision" params={{ revision: applied }}>
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
      <MasterDetail
        label="Documents"
        sidebar={groups(list).map((group) => (
          <div key={group.title} className="space-y-0.5">
            <SidebarHeading>{group.title}</SidebarHeading>
            {group.items.map((item) => {
              const itemStatus = status(item);
              return (
                <SidebarItem
                  key={item.key}
                  selected={item.key === selected?.key}
                  onSelect={() => onSelect(item.key)}
                  primary={
                    <>
                      <ItemIcon item={item} facts={facts.get(item.key)} />
                      <span className="truncate">{item.title}</span>
                    </>
                  }
                  secondary={
                    itemStatus ? (
                      <Badge variant="outline">{itemStatus}</Badge>
                    ) : (
                      facts.get(item.key)?.count.toLocaleString("en")
                    )
                  }
                />
              );
            })}
          </div>
        ))}
      >
        {selected ? (
          <section aria-label={selected.title} className="flex flex-col gap-4 p-5">
            <DetailHeader
              icon={<ItemIcon item={selected} facts={facts.get(selected.key)} />}
              title={selected.title}
              meta={
                <>
                  {selected.identifier ? (
                    <Badge variant="outline" className="font-mono">
                      {selected.identifier}
                    </Badge>
                  ) : null}
                  {facts.has(selected.key) ? (
                    <span className="text-sm text-muted-foreground">
                      {entityCountLabel(facts.get(selected.key)?.count ?? 0)}
                    </span>
                  ) : null}
                </>
              }
              actions={
                <>
                  <OpenTarget item={selected} />
                  <DocumentActions item={selected} />
                </>
              }
            />
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
          <p className="py-12 text-center text-sm text-muted-foreground">
            No definitions yet. Start with New or Upload YAML.
          </p>
        )}
      </MasterDetail>
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

function OpenTarget({ item }: Readonly<{ item: Item }>) {
  if (!item.document) return null;
  if (item.kind === "Blueprint") {
    return (
      <Button size="sm" variant="outline" asChild>
        <Link to="/catalog/$blueprint" params={{ blueprint: item.identifier }}>
          Open catalog
        </Link>
      </Button>
    );
  }
  return (
    <Button size="sm" variant="outline" asChild>
      <Link
        to="/settings/catalog-setup"
        search={{ tab: "integrations", integration: item.identifier }}
      >
        Open integration
      </Link>
    </Button>
  );
}

function DocumentActions({ item }: Readonly<{ item: Item }>) {
  if (item.draft?.removed) {
    return (
      <Button size="sm" variant="outline" onClick={() => revert(item.key)}>
        Restore
      </Button>
    );
  }

  return (
    <>
      {item.draft && item.document && (
        <Button size="sm" variant="outline" onClick={() => revert(item.key)}>
          Revert changes
        </Button>
      )}
      <Button size="sm" variant="outline" onClick={() => remove(item.key, item.document)}>
        Remove
      </Button>
    </>
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
