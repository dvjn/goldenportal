import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { Detail, DetailList } from "@/components/detail-list";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  type ConnectionSpec,
  checkConnection,
  connectorsQueryOptions,
  statusQueryOptions,
} from "@/features/connections";

// ConnectionOverview shows a connection read-only: its connector and what
// blueprints read from it. Pushing has its own tab, and syncing sits in the page header.
export function ConnectionOverview({ spec }: Readonly<{ spec: ConnectionSpec }>) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <ConnectorCard spec={spec} />
      <ReadsCard identifier={spec.identifier} />
    </div>
  );
}

// ReadsCard lists the resources blueprints' published sources read from
// the connection, each with the blueprints that map it and how many of
// their entities the connection wrote.
function ReadsCard({ identifier }: Readonly<{ identifier: string }>) {
  const { data } = useQuery(statusQueryOptions);
  const status = data?.find((s) => s.connection === identifier);
  if (!status) return null;
  const resources = new Map<string, typeof status.sources>();
  for (const source of status.sources) {
    resources.set(source.resource, [...(resources.get(source.resource) ?? []), source]);
  }
  const entities = (blueprint: string) =>
    status.feeds.find((f) => f.blueprint === blueprint)?.entities ?? 0;

  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Reads" />
      <CardContent className={resources.size > 0 ? "grid px-0" : "pb-6"}>
        {resources.size === 0 ? (
          <EmptyState
            compact
            title="Reads Nothing"
            description={`No blueprint's sources read ${identifier} yet.`}
          />
        ) : (
          [...resources].map(([resource, sources]) => (
            <CardSection key={resource} heading={<code className="font-mono">{resource}</code>}>
              <ul className="grid gap-2 text-sm">
                {sources.map((source, index) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: a blueprint may read one resource twice, with two filters.
                  <li key={index} className="flex items-center justify-between gap-3">
                    <Link
                      to="/blueprints/$identifier"
                      params={{ identifier: source.blueprint }}
                      className="min-w-0 hover:underline"
                    >
                      <BlueprintLabel blueprint={source.blueprint} />
                    </Link>
                    <span className="flex items-center gap-3">
                      <Badge variant={source.mode === "upsert" ? "outline" : "secondary"}>
                        {source.mode === "upsert" ? "Upsert" : "Reconcile"}
                      </Badge>
                      <span
                        className="font-medium tabular-nums"
                        title={`${entities(source.blueprint)} ${entities(source.blueprint) === 1 ? "entity" : "entities"}`}
                      >
                        {entities(source.blueprint)}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            </CardSection>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// ConnectorCard says what the connection connects to and what it does
// with it. One that pulls syncs incrementally every interval and completely,
// deleting what neither it nor the complete run before it saw, every complete interval; without
// one, every sync is complete.
function ConnectorCard({ spec }: Readonly<{ spec: ConnectionSpec }>) {
  const { data: connectors } = useQuery(connectorsQueryOptions);
  const known = connectors?.find((c) => c.id === spec.connector.type);
  const interval = spec.sync?.interval ?? "15m";
  const check = useMutation({
    mutationFn: () => checkConnection(spec.identifier),
    onError: (error) => toast.error(error.message),
  });

  return (
    <Card>
      <CardTitleBar
        title="About"
        action={
          spec.checks && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => check.mutate()}
              disabled={check.isPending}
            >
              {check.isPending && <Loader2 className="animate-spin" aria-hidden />}
              Test Connection
            </Button>
          )
        }
      />
      <CardContent className="grid gap-3 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <div className="flex items-center gap-2">
          <IconTile icon={known?.icon} color={known?.color} size="sm" />
          <span className="font-medium">{known?.title ?? spec.connector.type} Connector</span>
          {spec.pulls && <Badge variant="secondary">Pull</Badge>}
          {spec.receives && <Badge variant="secondary">{spec.events ? "Events" : "Push"}</Badge>}
        </div>
        {known && <p className="text-muted-foreground">{known.description}</p>}
        {(spec.pulls || check.data) && (
          <DetailList>
            {spec.pulls && (
              <>
                <Detail label="Syncs Every">
                  <code className="font-mono">{interval}</code>
                </Detail>
                <Detail label="Complete Every">
                  <code className="font-mono">{spec.sync?.complete ?? interval}</code>
                </Detail>
              </>
            )}
            {check.data && (
              <Detail
                label="Connection"
                help={[check.data.error, check.data.warning].filter(Boolean).join(" ")}
              >
                {check.data.passed && check.data.warning ? (
                  <Badge variant="warning">Passed with Warnings</Badge>
                ) : check.data.passed ? (
                  <Badge variant="success">Passed</Badge>
                ) : (
                  <Badge variant="destructive">Failed</Badge>
                )}
              </Detail>
            )}
          </DetailList>
        )}
      </CardContent>
    </Card>
  );
}
