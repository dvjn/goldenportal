import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, FilePen, Loader2, Save, Undo2 } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { DeleteDocDialog } from "@/components/config-docs/delete-doc-dialog";
import { DocEditor, useLiveValidation } from "@/components/config-docs/doc-editor";
import { PublishDialog } from "@/components/config-docs/publish-dialog";
import { StatusBadges } from "@/components/config-docs/status-badges";
import { VersionsList } from "@/components/config-docs/versions-list";
import { ConfirmIconButton } from "@/components/confirm-icon-button";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageHeader } from "@/components/page-header";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { ConfigDoc, ConfigDocs } from "@/features/config-docs";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const docTabs = ["overview", "definition", "versions"] as const;
export type DocTab = (typeof docTabs)[number];

// validateDocSearch reads the open tab from the URL, so tabs can be linked to.
export function validateDocSearch(search: Record<string, unknown>): { tab?: DocTab } {
  return docTabs.includes(search.tab as DocTab) ? { tab: search.tab as DocTab } : {};
}

type DocPageProps<S> = {
  docs: ConfigDocs<S>;
  doc: ConfigDoc<S>;
  // noun names the kind in titles and buttons, e.g. "Blueprint".
  noun: string;
  // parent links to the list the document belongs to, for the breadcrumbs.
  parent: ReactNode;
  deleteDescription: string;
  onDeleted: () => Promise<void> | void;
  tab: DocTab | undefined;
  onTabChange: (tab: DocTab) => void;
  // overview renders the document read-only: the published version, or the
  // draft until there is one.
  overview: (spec: S, doc: ConfigDoc<S>) => ReactNode;
  // editable is false for people who can read the document but not change
  // it: they see only the overview.
  editable?: boolean;
};

// DocPage is the page of one configuration document: a read-only overview,
// the YAML definition with its draft and publish actions, and its versions.
export function DocPage<S>(props: DocPageProps<S>) {
  const { doc } = props;
  if (props.editable === false) {
    const shown = doc.published?.spec;
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title={doc.title}
          description={doc.description}
          breadcrumbs={<PageBreadcrumbs page={doc.title}>{props.parent}</PageBreadcrumbs>}
        />
        {shown && props.overview(shown, doc)}
      </div>
    );
  }
  const saved = doc.draft?.source ?? doc.published?.source ?? "";

  // Remount whenever the saved source changes, so the editor starts from it.
  return <DocWorkspace key={saved} saved={saved} {...props} />;
}

function DocWorkspace<S>({
  docs,
  doc,
  noun,
  parent,
  deleteDescription,
  onDeleted,
  tab,
  onTabChange,
  overview,
  saved,
}: DocPageProps<S> & { saved: string }) {
  const { identifier } = doc;
  const activeTab = tab ?? (doc.published ? "overview" : "definition");
  const [source, setSource] = useState(saved);
  const dirty = source !== saved;
  const live = useLiveValidation(docs, source);
  const issues = dirty ? (live.validation?.issues ?? []) : (doc.draft?.issues ?? []);
  const hasErrors = issues.some((issue) => issue.severity === "error");
  const shown = doc.published?.spec ?? doc.draft?.spec;

  const queryClient = useQueryClient();
  const save = useMutation({
    mutationFn: () => docs.saveDraft(identifier, source),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: docs.keys.all });
      toast.success("Saved the draft");
    },
    onError: (error) => toast.error(error.message),
  });
  const discard = useMutation({
    mutationFn: () => docs.discardDraft(identifier),
    onSuccess: async () => {
      if (!doc.published) {
        queryClient.removeQueries({ queryKey: docs.keys.detail(identifier) });
        await queryClient.invalidateQueries({ queryKey: docs.keys.all });
        toast.success(`Discarded ${identifier}, which was never published`);
        await onDeleted();
        return;
      }
      await queryClient.invalidateQueries({ queryKey: docs.keys.all });
      toast.success("Discarded the draft");
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={doc.title}
        breadcrumbs={<PageBreadcrumbs page={doc.title}>{parent}</PageBreadcrumbs>}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <code className="font-mono">{identifier}</code>
            <StatusBadges
              doc={{ publishedVersion: doc.published?.version ?? 0, hasDraft: doc.draft !== null }}
            />
          </span>
        }
        actions={
          <>
            {doc.published && (
              <Button variant="outline" asChild>
                <a href={docs.exportUrl(identifier)} download={`${identifier}.yaml`}>
                  <Download />
                  Export
                </a>
              </Button>
            )}
            <DeleteDocDialog
              docs={docs}
              identifier={identifier}
              noun={noun}
              description={deleteDescription}
              onDeleted={onDeleted}
            />
          </>
        }
      />

      <Tabs value={activeTab} onValueChange={(value) => onTabChange(value as DocTab)}>
        <TabsList variant="line" aria-label={noun}>
          {docTabs.map((value) => (
            <TabsTrigger
              key={value}
              value={value}
              className="capitalize focus-visible:border-transparent focus-visible:bg-muted focus-visible:ring-0 focus-visible:outline-none"
            >
              {value}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="overview" className="flex flex-col gap-4 pt-2">
          {doc.draft && (
            <Alert>
              <FilePen />
              <AlertTitle>
                {doc.published ? "There's an unpublished draft" : "Not published yet"}
              </AlertTitle>
              <AlertDescription>
                {doc.published
                  ? `This shows version ${doc.published.version}. The draft was saved ${formatRelative(doc.draft.updatedAt)}.`
                  : "This shows the draft. Publish it to put it to use."}
              </AlertDescription>
              <AlertAction>
                <Button size="sm" variant="outline" onClick={() => onTabChange("definition")}>
                  Open Draft
                </Button>
              </AlertAction>
            </Alert>
          )}
          {shown && overview(shown, doc)}
        </TabsContent>

        <TabsContent value="definition" className="pt-2">
          <Card>
            <CardHeader className="border-b">
              <CardTitle>Definition</CardTitle>
              <CardAction className="text-xs text-muted-foreground">
                <SavedState doc={doc} dirty={dirty} />
              </CardAction>
            </CardHeader>
            <CardContent>
              <DocEditor
                value={source}
                onChange={setSource}
                issues={issues}
                checking={dirty && live.checking}
              />
            </CardContent>
            <CardFooter className="flex flex-wrap justify-end gap-2 border-t">
              {doc.draft && !dirty && (
                <ConfirmIconButton
                  label="Discard draft"
                  icon={<Undo2 />}
                  title="Discard the draft?"
                  description={
                    doc.published
                      ? `It goes back to version ${doc.published.version}.`
                      : `This ${noun.toLowerCase()} was never published, so it's deleted.`
                  }
                  confirm="Discard Draft"
                  onConfirm={() => discard.mutate()}
                />
              )}
              {dirty && (
                <Button variant="ghost" onClick={() => setSource(saved)}>
                  Revert
                </Button>
              )}
              <Button
                variant={doc.draft && !dirty ? "outline" : "default"}
                onClick={() => save.mutate()}
                disabled={!dirty || hasErrors || save.isPending}
              >
                {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
                Save Draft
              </Button>
              {doc.draft && !dirty && (
                <PublishDialog
                  docs={docs}
                  identifier={identifier}
                  disabled={doc.draft.issues.some((issue) => issue.severity === "error")}
                />
              )}
            </CardFooter>
          </Card>
        </TabsContent>

        <TabsContent value="versions" className="pt-2">
          <Card>
            <CardContent>
              <VersionsList
                docs={docs}
                identifier={identifier}
                onRestore={(restored) => {
                  setSource(restored);
                  onTabChange("definition");
                }}
              />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function SavedState<S>({ doc, dirty }: Readonly<{ doc: ConfigDoc<S>; dirty: boolean }>) {
  if (dirty) return <span>Unsaved changes</span>;
  if (doc.draft) {
    return (
      <span title={formatAbsolute(doc.draft.updatedAt)}>
        Draft saved {formatRelative(doc.draft.updatedAt)}
        {doc.draft.updatedBy && ` by ${doc.draft.updatedBy}`}
      </span>
    );
  }
  if (!doc.published) return null;
  return (
    <span title={formatAbsolute(doc.published.publishedAt)}>
      Version {doc.published.version} published {formatRelative(doc.published.publishedAt)}
    </span>
  );
}
