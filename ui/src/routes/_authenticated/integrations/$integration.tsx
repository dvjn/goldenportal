import { createFileRoute, Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { meQueryOptions, useCan } from "@/features/auth";
import {
  connectorLabels,
  IngestTokens,
  type IntegrationDetail,
  ingestTokensQueryOptions,
  integrationQueryOptions,
  RunHistory,
  runsQueryOptions,
  useIntegration,
  useRuns,
  useSyncIntegration,
} from "@/features/integrations";
import { formatDateTime, formatRelative } from "@/lib/time";

export const Route = createFileRoute("/_authenticated/integrations/$integration")({
  loader: async ({ context: { queryClient }, params }) => {
    const [integration, , me] = await Promise.all([
      queryClient.ensureQueryData(integrationQueryOptions(params.integration)),
      queryClient.ensureQueryData(runsQueryOptions(params.integration)),
      queryClient.ensureQueryData(meQueryOptions),
    ]);
    if (takesTokens(integration) && me.permissions.includes("integrations.tokens")) {
      await queryClient.ensureQueryData(ingestTokensQueryOptions(params.integration));
    }
    return { breadcrumb: integration.title };
  },
  component: IntegrationPage,
});

function takesTokens(integration: IntegrationDetail) {
  return integration.connector === "push";
}

function IntegrationPage() {
  const params = Route.useParams();
  const integration = useIntegration(params.integration);
  const can = useCan();

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            {integration.title}
            <Badge variant="outline">
              {connectorLabels[integration.connector] ?? integration.connector}
            </Badge>
          </h1>
          {integration.description && (
            <p className="text-muted-foreground text-sm">{integration.description}</p>
          )}
        </div>
        {integration.pull && <SyncNow integration={integration} />}
      </header>
      <div className="grid gap-4 lg:grid-cols-2">
        <DefinitionCard integration={integration} />
        <EntitiesCard integration={integration} />
      </div>
      {takesTokens(integration) && can("integrations.tokens") && (
        <IngestTokens integration={integration} />
      )}
      {!integration.manual && <Runs identifier={integration.identifier} />}
    </>
  );
}

function SyncNow({ integration }: { integration: IntegrationDetail }) {
  const can = useCan();
  const sync = useSyncIntegration(integration.identifier);

  if (!can("integrations.sync")) return null;
  const running = integration.last_run?.status === "running";

  return (
    <div className="flex flex-col items-end gap-1">
      {running ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span>
              <Button disabled>Sync now</Button>
            </span>
          </TooltipTrigger>
          <TooltipContent>A sync is already running</TooltipContent>
        </Tooltip>
      ) : (
        <Button disabled={sync.isPending} onClick={() => sync.mutate()}>
          Sync now
        </Button>
      )}
      {sync.isError && <p className="text-destructive text-sm">{sync.error.message}</p>}
    </div>
  );
}

function DefinitionCard({ integration }: { integration: IntegrationDetail }) {
  const can = useCan();

  return (
    <Card>
      <CardHeader>
        <CardTitle>Definition</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        {integration.manual ? (
          <p className="text-muted-foreground">
            Built in. It owns the entities people create in the portal, and no document defines it.
          </p>
        ) : (
          <dl className="grid gap-x-4 gap-y-3 sm:grid-cols-[minmax(8rem,auto)_1fr]">
            <dt className="text-muted-foreground">Identifier</dt>
            <dd className="font-mono text-xs">{integration.identifier}</dd>
            <dt className="text-muted-foreground">Schedule</dt>
            <dd>{schedule(integration)}</dd>
            <dt className="text-muted-foreground">Kinds</dt>
            <dd>
              <ul className="flex flex-col gap-1">
                {integration.kinds.map((kind) => (
                  <li key={kind.kind}>
                    <span className="font-mono text-xs">{kind.kind}</span> to {kind.blueprint_title}
                    {kind.canonical && <span className="text-muted-foreground"> as entities</span>}
                  </li>
                ))}
                {integration.kinds.length === 0 && (
                  <li className="text-muted-foreground">No mappings</li>
                )}
              </ul>
            </dd>
            {integration.secrets.length > 0 && (
              <>
                <dt className="text-muted-foreground">Secrets</dt>
                <dd>
                  <ul className="flex flex-col gap-1">
                    {integration.secrets.map((secret) => (
                      <li key={secret.name}>
                        {secret.name} from{" "}
                        <span className="font-mono text-xs">{secret.variable}</span>
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            )}
          </dl>
        )}
        {!integration.manual && can("catalog.model") && (
          <Link
            to="/definitions"
            search={{ document: `Integration/${integration.identifier}` }}
            className="underline-offset-4 hover:underline"
          >
            Open in the definitions editor
          </Link>
        )}
      </CardContent>
    </Card>
  );
}

function schedule(integration: IntegrationDetail) {
  if (!integration.pull) return "Receives pushes";
  if (!integration.interval) return "Syncs only when someone syncs it";
  const next = integration.next_run_at;
  return (
    <>
      Every {integration.interval}
      {next && (
        <span className="text-muted-foreground" title={formatDateTime(next)}>
          , next {formatRelative(next)}
        </span>
      )}
    </>
  );
}

function EntitiesCard({ integration }: { integration: IntegrationDetail }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {integration.entity_count === 1
            ? "1 entity"
            : `${integration.entity_count.toLocaleString("en")} entities`}
        </CardTitle>
      </CardHeader>
      <CardContent className="text-sm">
        {integration.entities.length === 0 ? (
          <p className="text-muted-foreground">The integration owns no entities yet.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {integration.entities.map((group) => (
              <li key={group.blueprint} className="flex justify-between gap-4">
                <Link
                  to="/catalog/$blueprint"
                  params={{ blueprint: group.blueprint }}
                  search={{ integration: integration.identifier }}
                  className="underline-offset-4 hover:underline"
                >
                  {group.title}
                </Link>
                <span className="tabular-nums">{group.count.toLocaleString("en")}</span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Runs({ identifier }: { identifier: string }) {
  const runs = useRuns(identifier);

  return (
    <section aria-label="Runs" className="flex flex-col gap-2">
      <h2 className="font-semibold">Runs</h2>
      <p className="text-muted-foreground text-sm">The last 100 syncs and pushes.</p>
      <RunHistory runs={runs} />
    </section>
  );
}
