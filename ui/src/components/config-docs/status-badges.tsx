import { Badge } from "@/components/ui/badge";
import type { DocSummary } from "@/features/config-docs";

export function StatusBadges({
  doc,
}: Readonly<{ doc: Pick<DocSummary, "publishedVersion" | "hasDraft"> }>) {
  return (
    <div className="flex items-center gap-1.5">
      {doc.publishedVersion > 0 && (
        <Badge variant="secondary">Version {doc.publishedVersion}</Badge>
      )}
      {doc.hasDraft && <Badge variant="outline">Draft</Badge>}
    </div>
  );
}
