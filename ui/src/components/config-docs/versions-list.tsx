import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Eye, Loader2 } from "lucide-react";
import { useState } from "react";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { ConfigDocs } from "@/features/config-docs";
import { formatAbsolute, formatRelative } from "@/lib/format";

type VersionsListProps<S> = {
  docs: ConfigDocs<S>;
  identifier: string;
  // onRestore puts an old version's YAML in the editor, to save as the draft.
  onRestore: (source: string) => void;
};

export function VersionsList<S>({ docs, identifier, onRestore }: VersionsListProps<S>) {
  const { data: versions } = useSuspenseQuery(docs.queries.versions(identifier));
  const [viewing, setViewing] = useState<number | null>(null);

  if (versions.length === 0) {
    return <p className="text-sm text-muted-foreground">Nothing is published yet.</p>;
  }

  return (
    <>
      <ul className="grid gap-2">
        {versions.map((version) => (
          <li key={version.version} className="flex items-center justify-between gap-3 text-sm">
            <div className="grid min-w-0">
              <span className="font-medium">Version {version.version}</span>
              <span className="truncate text-xs text-muted-foreground">
                <span title={formatAbsolute(version.publishedAt)}>
                  {formatRelative(version.publishedAt)}
                </span>
                {version.publishedBy && ` by ${version.publishedBy}`}
              </span>
            </div>
            <IconButton
              label={`View version ${version.version}`}
              onClick={() => setViewing(version.version)}
            >
              <Eye />
            </IconButton>
          </li>
        ))}
      </ul>
      <Dialog open={viewing !== null} onOpenChange={(open) => !open && setViewing(null)}>
        <DialogContent className="sm:max-w-2xl">
          {viewing !== null && (
            <VersionSource
              docs={docs}
              identifier={identifier}
              version={viewing}
              onRestore={(source) => {
                onRestore(source);
                setViewing(null);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function VersionSource<S>({
  docs,
  identifier,
  version,
  onRestore,
}: Readonly<{
  docs: ConfigDocs<S>;
  identifier: string;
  version: number;
  onRestore: (source: string) => void;
}>) {
  const { data, isPending, error } = useQuery(docs.queries.version(identifier, version));

  return (
    <>
      <DialogHeader>
        <DialogTitle>
          {identifier} version {version}
        </DialogTitle>
        <DialogDescription>
          Open it in the editor to save it as the draft, then publish to roll back.
        </DialogDescription>
      </DialogHeader>
      {isPending ? (
        <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />
      ) : error ? (
        <p className="text-sm text-destructive">{error.message}</p>
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-5">
          {data.source}
        </pre>
      )}
      <DialogFooter>
        <Button disabled={!data?.source} onClick={() => data?.source && onRestore(data.source)}>
          Open in Editor
        </Button>
      </DialogFooter>
    </>
  );
}
