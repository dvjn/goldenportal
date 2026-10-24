import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Save } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { DocEditor, useLiveValidation } from "@/components/config-docs/doc-editor";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import type { ConfigDocs } from "@/features/config-docs";

type NewDocPageProps<S extends { identifier: string }> = {
  docs: ConfigDocs<S>;
  title: string;
  template: string;
  onCreated: (identifier: string) => Promise<void> | void;
  // preview renders the parsed document beside the editor while it's valid.
  preview: (spec: S) => ReactNode;
};

// NewDocPage writes a new configuration document and saves it as a draft.
export function NewDocPage<S extends { identifier: string }>({
  docs,
  title,
  template,
  onCreated,
  preview,
}: NewDocPageProps<S>) {
  const [source, setSource] = useState(template);
  const live = useLiveValidation(docs, source);
  const issues = live.validation?.issues ?? [];
  const valid =
    !live.checking && live.validation && !issues.some((issue) => issue.severity === "error");
  const identifier = live.validation?.spec.identifier ?? "";

  const queryClient = useQueryClient();
  const save = useMutation({
    mutationFn: () => docs.saveDraft(identifier, source),
    onSuccess: async ({ created }) => {
      await queryClient.invalidateQueries({ queryKey: docs.keys.all });
      toast.success(
        created ? `Created ${identifier} as a draft` : `Saved over the draft of ${identifier}`,
      );
      await onCreated(identifier);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={title}
        description="Write it in YAML. It's saved as a draft until you publish it."
      />
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardHeader className="border-b">
            <CardTitle>Definition</CardTitle>
          </CardHeader>
          <CardContent>
            <DocEditor
              value={source}
              onChange={setSource}
              issues={issues}
              checking={live.checking}
            />
          </CardContent>
          <CardFooter className="justify-end border-t">
            <Button onClick={() => save.mutate()} disabled={!valid || save.isPending}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              Save Draft
            </Button>
          </CardFooter>
        </Card>
        <Card>
          <CardHeader className="border-b">
            <CardTitle>Preview</CardTitle>
          </CardHeader>
          <CardContent>
            {valid && live.validation ? (
              preview(live.validation.spec)
            ) : (
              <p className="text-sm text-muted-foreground">Fix the problems to see a preview.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
