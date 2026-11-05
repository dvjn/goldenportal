import { Suspense } from "react";
import { CardTitleBar } from "@/components/card-sections";
import { CodeBlock } from "@/components/code";
import { CopyIconButton } from "@/components/copy-button";
import { IngestionTokens } from "@/components/integrations/ingestion-tokens";
import { Card, CardContent } from "@/components/ui/card";
import { ingestUrl } from "@/features/integrations";

// PushPanel is how other systems push to an integration: its endpoint,
// requests to copy into any script or CI job, and the tokens they send.
// The body is whatever JSON the connector config's push side reads.
export function PushPanel({ identifier }: Readonly<{ identifier: string }>) {
  const url = ingestUrl(identifier);
  const request = (target: string) =>
    [
      `curl -X POST "${target}" \\`,
      `  -H "Authorization: Bearer $INGEST_TOKEN" \\`,
      `  -H "Content-Type: application/json" \\`,
      "  --data @data.json",
    ].join("\n");
  const remove = [
    `curl -X DELETE "${url}/entities/<blueprint>/<identifier>" \\`,
    `  -H "Authorization: Bearer $INGEST_TOKEN"`,
  ].join("\n");

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <Card>
        <CardTitleBar title="Endpoint" />
        <CardContent className="grid min-w-0 gap-4 text-sm">
          <Snippet
            title="Incremental Push"
            value={request(`${url}?mode=incremental`)}
            help="Writes what it carries, and deletes only what it marks deleted."
          />
          <Snippet
            title="Complete Push"
            value={request(`${url}?mode=complete`)}
            help="Also deletes what neither it nor the complete run before it saw. Add &resources=a,b to cover only those resources."
          />
          <Snippet title="Delete One" value={remove} />
        </CardContent>
      </Card>
      <Card>
        <CardTitleBar title="Ingestion Tokens" />
        <CardContent>
          <Suspense fallback={null}>
            <IngestionTokens identifier={identifier} />
          </Suspense>
        </CardContent>
      </Card>
    </div>
  );
}

function Snippet({
  title,
  value,
  help,
}: Readonly<{ title: string; value: string; help?: string }>) {
  return (
    <div className="grid min-w-0 gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{title}</span>
        <CopyIconButton value={value} label={`${title.toLowerCase()} request`} />
      </div>
      <CodeBlock className="overflow-x-auto break-normal whitespace-pre">{value}</CodeBlock>
      {help && <p className="text-xs text-muted-foreground">{help}</p>}
    </div>
  );
}
