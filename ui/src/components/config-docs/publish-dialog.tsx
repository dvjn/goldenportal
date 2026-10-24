import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Upload } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import type { ConfigDocs } from "@/features/config-docs";

const sectionLabels: Record<string, string> = {
  blueprint: "Blueprint",
  integration: "Integration",
  properties: "Property",
  mappings: "Mapping",
};

const changeVariants = {
  added: "secondary",
  changed: "outline",
  removed: "destructive",
} as const;

// PublishDialog reviews what publishing the draft changes, then publishes it.
export function PublishDialog<S>({
  docs,
  identifier,
  disabled,
}: Readonly<{ docs: ConfigDocs<S>; identifier: string; disabled?: boolean }>) {
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button disabled={disabled}>
          <Upload />
          Publish
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        {open && (
          <PublishReview docs={docs} identifier={identifier} onDone={() => setOpen(false)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function PublishReview<S>({
  docs,
  identifier,
  onDone,
}: Readonly<{ docs: ConfigDocs<S>; identifier: string; onDone: () => void }>) {
  const queryClient = useQueryClient();
  const impact = useQuery(docs.queries.impact(identifier));
  const [acknowledged, setAcknowledged] = useState(false);
  const publish = useMutation({
    mutationFn: () => docs.publish(identifier, acknowledged),
    onSuccess: async ({ version }) => {
      await queryClient.invalidateQueries({ queryKey: docs.keys.all });
      toast.success(`Published ${identifier} version ${version}`);
      onDone();
    },
    onError: (error) => toast.error(error.message),
  });

  const data = impact.data;
  const hasErrors = data?.issues.some((issue) => issue.severity === "error") ?? false;
  const warnings = data?.issues.filter((issue) => issue.severity === "warning") ?? [];

  return (
    <>
      <DialogHeader>
        <DialogTitle>Publish {identifier}</DialogTitle>
        <DialogDescription>
          {data?.firstVersion
            ? "This publishes the first version, so entities can be ingested for it."
            : "Review what changes before you publish."}
        </DialogDescription>
      </DialogHeader>

      {impact.isPending ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" aria-hidden />
          Working out the impact…
        </p>
      ) : impact.isError ? (
        <p className="text-sm text-destructive">{impact.error.message}</p>
      ) : (
        <div className="grid max-h-[50vh] gap-4 overflow-y-auto">
          {data?.firstVersion ? null : data && data.changes.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              The draft is the same as the published version.
            </p>
          ) : (
            <ul className="grid gap-2" aria-label="Changes">
              {data?.changes.map((change) => (
                <li
                  key={`${change.section}:${change.identifier}`}
                  className="flex items-start gap-2 text-sm"
                >
                  <Badge variant={changeVariants[change.change]} className="w-18 capitalize">
                    {change.change}
                  </Badge>
                  <div className="grid min-w-0 flex-1">
                    <span>
                      <span className="text-muted-foreground">
                        {sectionLabels[change.section] ?? change.section}
                      </span>
                      {change.identifier && (
                        <code className="ml-1.5 font-mono text-xs">{change.identifier}</code>
                      )}
                    </span>
                    <span className="text-muted-foreground">{change.detail}</span>
                  </div>
                  {change.breaking && <Badge variant="destructive">Breaking</Badge>}
                </li>
              ))}
            </ul>
          )}
          {warnings.length > 0 && (
            <ul className="grid gap-1 text-sm text-amber-700 dark:text-amber-400">
              {warnings.map((warning) => (
                <li key={`${warning.path}:${warning.message}`}>{warning.message}</li>
              ))}
            </ul>
          )}
          {data?.breaking && (
            <div className="flex items-start gap-2">
              <Checkbox
                id="acknowledge-breaking"
                checked={acknowledged}
                onCheckedChange={(checked) => setAcknowledged(checked === true)}
              />
              <Label htmlFor="acknowledge-breaking" className="leading-snug font-normal">
                {data.affected > 0
                  ? `I understand this affects ${data.affected} ${data.affected === 1 ? "entity" : "entities"}, and values the changes remove are dropped.`
                  : "I understand these changes are breaking."}
              </Label>
            </div>
          )}
        </div>
      )}

      <DialogFooter>
        <Button
          onClick={() => publish.mutate()}
          disabled={!data || hasErrors || (data.breaking && !acknowledged) || publish.isPending}
        >
          {publish.isPending && <Loader2 className="animate-spin" />}
          Publish Version
        </Button>
      </DialogFooter>
    </>
  );
}
