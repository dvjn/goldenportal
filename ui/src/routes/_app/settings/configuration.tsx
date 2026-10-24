import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  AppWindow,
  CircleAlert,
  CircleCheck,
  Copy,
  ImageOff,
  KeyRound,
  Loader2,
  type LucideIcon,
  Server,
  Timer,
} from "lucide-react";
import type { ReactNode } from "react";
import { toast } from "sonner";
import { IconButton } from "@/components/icon-button";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
      <p className="text-sm text-muted-foreground">
        This is the instance configuration. It's chosen where the portal is deployed and can't be
        changed here.
      </p>
      {/* Two columns that stack their own cards, so a tall card never stretches its neighbour. */}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <div className="grid gap-4">
          <PortalCard portal={config.portal} />
          <AuthenticationCard oidc={config.oidc} scim={config.scim} />
        </div>
        <div className="grid gap-4">
          <SessionsCard sessions={config.sessions} tokens={config.tokens} />
          <PlatformCard database={config.database} server={config.server} />
        </div>
      </div>
    </div>
  );
}

function PortalCard({ portal }: Readonly<{ portal: ServerConfiguration["portal"] }>) {
  return (
    <Card>
      <ConfigCardHeader icon={AppWindow} title="Portal" />
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

function SessionsCard({
  sessions,
  tokens,
}: Readonly<{ sessions: ServerConfiguration["sessions"]; tokens: ServerConfiguration["tokens"] }>) {
  return (
    <Card>
      <ConfigCardHeader icon={Timer} title="Sessions & API Tokens" />
      <CardContent>
        <Rows>
          <Row label="Session Idle Timeout">{hours(sessions.idleTimeoutHours)}</Row>
          <Row label="Session Max Age">{hours(sessions.maxAgeHours)}</Row>
          <Row label="Secure Cookies">{sessions.secureCookies ? "On" : "Off"}</Row>
          <Row label="API Token Max Expiry">{days(tokens.maxDays)}</Row>
          <Row label="API Token Prefix">{tokens.prefix}</Row>
        </Rows>
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
      <ConfigCardHeader icon={KeyRound} title="Authentication">
        {!oidc.configured && (
          <Badge variant="outline" className="text-warning">
            <CircleAlert aria-hidden />
            Not configured
          </Badge>
        )}
        <TestConnection />
      </ConfigCardHeader>
      <CardContent className="grid gap-4">
        <Rows>
          <Row label="Issuer">{oidc.issuer || <NotSet />}</Row>
          <Row label="Client ID">{oidc.clientId || <NotSet />}</Row>
          <Row label="Client Secret">
            <SetBadge set={oidc.clientSecretSet} />
          </Row>
          <Row label="Scopes">
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
          </Row>
          <Row label="Groups Claim">{oidc.groupsClaim || <NotSet />}</Row>
          <Row label="Redirect URI" help="Register this callback URL with the identity provider.">
            {oidc.redirectUrl}
            <CopyButton label="Redirect URI" value={oidc.redirectUrl} />
          </Row>
          <Row
            label="Bootstrap Admin"
            help="Granted the admin role on sign-in with this verified email."
          >
            {oidc.bootstrapAdminEmail || <NotSet />}
          </Row>
        </Rows>
        <Separator />
        <Rows>
          <Row
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
          </Row>
          {scim.enabled && (
            <Row
              label="SCIM Endpoint"
              help="Configure this as the SCIM base URL in the identity provider."
            >
              {scim.url}
              <CopyButton label="SCIM Endpoint" value={scim.url} />
            </Row>
          )}
        </Rows>
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
      <ConfigCardHeader icon={Server} title="Platform" />
      <CardContent className="grid gap-4">
        <Rows>
          <Row label="Dialect">{database.dialect || <NotSet />}</Row>
          <Row label="Host">{where || <NotSet />}</Row>
          <Row label="Database">{database.name || <NotSet />}</Row>
          <Row label="User">{database.user || <NotSet />}</Row>
          <Row label="Password">
            <SetBadge set={database.passwordSet} />
          </Row>
          <Row label="SSL Mode">{database.sslMode || <NotSet />}</Row>
        </Rows>
        <Separator />
        <Rows>
          <Row label="Environment">{server.environment || <NotSet />}</Row>
          <Row label="Version">{server.version || <NotSet />}</Row>
          <Row label="HTTP Port">{server.httpPort}</Row>
          <Row label="Metrics Port">{server.metricsPort}</Row>
          <Row label="Log Level">{server.logLevel}</Row>
        </Rows>
      </CardContent>
    </Card>
  );
}

// Rows is a compact list of settings: one line per setting where it fits.
// ConfigCardHeader is every card's header: an icon tile and the title, with
// any actions on the right, set off from the card's rows by a border.
function ConfigCardHeader({
  icon: Icon,
  title,
  children,
}: Readonly<{ icon: LucideIcon; title: string; children?: ReactNode }>) {
  return (
    <CardHeader className="border-b">
      <CardTitle className="flex items-center gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <Icon className="size-4" aria-hidden />
        </span>
        {title}
      </CardTitle>
      {children && (
        <CardAction className="flex items-center gap-2 self-center">{children}</CardAction>
      )}
    </CardHeader>
  );
}

function Rows({ children }: Readonly<{ children: ReactNode }>) {
  return <dl className="grid gap-2">{children}</dl>;
}

function Row({
  label,
  help,
  children,
}: Readonly<{ label: string; help?: string; children: ReactNode }>) {
  return (
    <div data-slot="setting" className="grid gap-x-4 gap-y-0.5 sm:grid-cols-[9rem_minmax(0,1fr)]">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="grid min-w-0 gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm [overflow-wrap:anywhere]">
          {children}
        </div>
        {help && <p className="text-xs text-muted-foreground">{help}</p>}
      </dd>
    </div>
  );
}

function NotSet() {
  return <span className="text-muted-foreground">Not set</span>;
}

function SetBadge({ set }: Readonly<{ set: boolean }>) {
  return set ? (
    <Badge variant="secondary">Set</Badge>
  ) : (
    <Badge variant="outline" className="text-muted-foreground">
      Not set
    </Badge>
  );
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

function CopyButton({ label, value }: Readonly<{ label: string; value: string }>) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`Copied the ${label.toLowerCase()}`);
    } catch {
      toast.error("Couldn't copy. Select the text and copy it instead.");
    }
  }

  return (
    <IconButton label={`Copy ${label}`} size="icon-xs" className="-my-1" onClick={copy}>
      <Copy />
    </IconButton>
  );
}
