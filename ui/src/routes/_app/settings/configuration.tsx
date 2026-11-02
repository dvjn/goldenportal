import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { CircleAlert, CircleCheck, ImageOff, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { CardTitleBar } from "@/components/card-sections";
import { CopyIconButton } from "@/components/copy-button";
import { Detail, DetailList } from "@/components/detail-list";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import {
  configurationQueryOptions,
  runConfigAction,
  type ServerConfiguration,
  useConfiguration,
} from "@/features/configuration";

export const Route = createFileRoute("/_app/settings/configuration")({
  loader: ({ context }) => context.queryClient.ensureQueryData(configurationQueryOptions),
  component: ConfigurationPage,
});

function ConfigurationPage() {
  const config = useConfiguration();

  return (
    <div className="flex flex-col gap-4">
      {/* Two columns that stack their own cards, so a tall card never stretches its neighbour. */}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <div className="grid gap-4">
          <PortalCard portal={config.portal} />
          <AuthenticationCard oidc={config.oidc} scim={config.scim} />
        </div>
        <div className="grid gap-4">
          <SessionsCard sessions={config.sessions} />
          <PlatformCard database={config.database} server={config.server} />
        </div>
      </div>
    </div>
  );
}

function PortalCard({ portal }: Readonly<{ portal: ServerConfiguration["portal"] }>) {
  return (
    <Card>
      <CardTitleBar title="Portal" />
      <CardContent className="flex items-center gap-4">
        <div className="flex shrink-0 gap-2">
          <PortalImage label="Logo" src={portal.logoUrl} />
          {!portal.faviconIsLogo && <PortalImage label="Favicon" src={portal.faviconUrl} />}
        </div>
        <dl className="grid min-w-0 gap-0.5">
          <dt className="sr-only">Portal name</dt>
          <dd className="truncate text-base font-medium">{portal.name}</dd>
          <dt className="sr-only">Public address</dt>
          <dd className="truncate text-sm text-muted-foreground">
            {portal.baseUrl || "No public address set"}
          </dd>
        </dl>
      </CardContent>
    </Card>
  );
}

function PortalImage({ label, src }: Readonly<{ label: string; src: string }>) {
  const frame =
    "flex size-12 items-center justify-center rounded-lg bg-muted ring-1 ring-foreground/10";

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="rounded-lg">
            {src ? (
              <img src={src} alt={label} className={`${frame} object-contain`} />
            ) : (
              <span className={`${frame} text-muted-foreground`}>
                <ImageOff className="size-5" aria-hidden />
                <span className="sr-only">No {label.toLowerCase()}</span>
              </span>
            )}
          </span>
        </TooltipTrigger>
        <TooltipContent>{src ? label : `No ${label.toLowerCase()}`}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function SessionsCard({ sessions }: Readonly<{ sessions: ServerConfiguration["sessions"] }>) {
  return (
    <Card>
      <CardTitleBar title="Sessions" />
      <CardContent>
        <DetailList>
          <Detail label="Session Idle Timeout">{hours(sessions.idleTimeoutHours)}</Detail>
          <Detail label="Session Max Age">{hours(sessions.maxAgeHours)}</Detail>
          <Detail label="Secure Cookies">{sessions.secureCookies ? "On" : "Off"}</Detail>
        </DetailList>
      </CardContent>
    </Card>
  );
}

function AuthenticationCard({
  oidc,
  scim,
}: Readonly<{ oidc: ServerConfiguration["oidc"]; scim: ServerConfiguration["scim"] }>) {
  return (
    <Card>
      <CardTitleBar
        title="Authentication"
        action={
          <>
            {!oidc.configured && (
              <Badge variant="warning">
                <CircleAlert aria-hidden />
                Not configured
              </Badge>
            )}
            <TestConnection />
          </>
        }
      />
      <CardContent className="grid gap-4">
        <DetailList>
          <Detail label="Issuer">{oidc.issuer || <NotSet />}</Detail>
          <Detail label="Client ID">{oidc.clientId || <NotSet />}</Detail>
          <Detail label="Client Secret">
            <SetBadge set={oidc.clientSecretSet} />
          </Detail>
          <Detail label="Scopes">
            {oidc.scopes.length > 0 ? (
              <span className="flex flex-wrap gap-1">
                {oidc.scopes.map((scope) => (
                  <Badge key={scope} variant="outline">
                    {scope}
                  </Badge>
                ))}
              </span>
            ) : (
              <NotSet />
            )}
          </Detail>
          <Detail label="Groups Claim">{oidc.groupsClaim || <NotSet />}</Detail>
          <Detail
            label="Redirect URI"
            help="Register this callback URL with the identity provider."
          >
            {oidc.redirectUrl}
            <CopyIconButton
              label="Redirect URI"
              value={oidc.redirectUrl}
              size="icon-xs"
              className="-my-1"
            />
          </Detail>
          <Detail
            label="Bootstrap Admin"
            help="Granted the admin role on sign-in with this verified email."
          >
            {oidc.bootstrapAdminEmail || <NotSet />}
          </Detail>
        </DetailList>
        <Separator />
        <DetailList>
          <Detail
            label="Provisioning"
            help={
              scim.enabled
                ? "Users and groups are provisioned over SCIM 2.0; the groups claim is ignored."
                : "Group membership syncs from the groups claim on each sign-in."
            }
          >
            {scim.enabled ? (
              <Badge variant="secondary">
                <CircleCheck className="text-success" aria-hidden />
                SCIM on
              </Badge>
            ) : (
              <Badge variant="outline" className="text-muted-foreground">
                SCIM off
              </Badge>
            )}
          </Detail>
          {scim.enabled && (
            <Detail
              label="SCIM Endpoint"
              help="Configure this as the SCIM base URL in the identity provider."
            >
              {scim.url}
              <CopyIconButton
                label="SCIM Endpoint"
                value={scim.url}
                size="icon-xs"
                className="-my-1"
              />
            </Detail>
          )}
        </DetailList>
      </CardContent>
    </Card>
  );
}

// TestConnection checks the identity provider and reports the result in a toast.
function TestConnection() {
  const run = useMutation({
    mutationFn: () => runConfigAction("oidc.check"),
    onSuccess: (result) =>
      result.ok ? toast.success(result.message) : toast.error(result.message),
    onError: (error) => toast.error(error.message),
  });

  return (
    <Button variant="outline" size="sm" onClick={() => run.mutate()} disabled={run.isPending}>
      {run.isPending && <Loader2 className="animate-spin" aria-hidden />}
      Test connection
    </Button>
  );
}

function PlatformCard({
  database,
  server,
}: Readonly<{ database: ServerConfiguration["database"]; server: ServerConfiguration["server"] }>) {
  const where = [database.host, database.port].filter(Boolean).join(":");

  return (
    <Card>
      <CardTitleBar title="Platform" />
      <CardContent className="grid gap-4">
        <DetailList>
          <Detail label="Dialect">{database.dialect || <NotSet />}</Detail>
          <Detail label="Host">{where || <NotSet />}</Detail>
          <Detail label="Database">{database.name || <NotSet />}</Detail>
          <Detail label="User">{database.user || <NotSet />}</Detail>
          <Detail label="Password">
            <SetBadge set={database.passwordSet} />
          </Detail>
          <Detail label="SSL Mode">{database.sslMode || <NotSet />}</Detail>
        </DetailList>
        <Separator />
        <DetailList>
          <Detail label="Environment">{server.environment || <NotSet />}</Detail>
          <Detail label="Version">{server.version || <NotSet />}</Detail>
          <Detail label="HTTP Port">{server.httpPort}</Detail>
          <Detail label="Metrics Port">{server.metricsPort}</Detail>
          <Detail label="Log Level">{server.logLevel}</Detail>
        </DetailList>
      </CardContent>
    </Card>
  );
}

function NotSet() {
  return <span className="text-muted-foreground">Not set</span>;
}

function SetBadge({ set }: Readonly<{ set: boolean }>) {
  return set ? <Badge variant="success">Set</Badge> : <Badge variant="muted">Not set</Badge>;
}

function hours(n: number) {
  return n >= 24 && n % 24 === 0 ? days(n / 24) : plural(n, "hour");
}

function days(n: number) {
  return plural(n, "day");
}

function plural(n: number, unit: string) {
  return `${n} ${unit}${n === 1 ? "" : "s"}`;
}
