import { createFileRoute } from "@tanstack/react-router";
import { RotateCwIcon, TriangleAlert } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { DescriptionList, DescriptionRow } from "@/components/common/description-list";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { CopyButton } from "@/components/copy-button";
import { SecretDialog } from "@/components/secret-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { requirePermission, useCan } from "@/features/auth";
import {
  RelativeTime,
  scimQueryOptions,
  settingsQueryOptions,
  useRotateScimToken,
  useScim,
  useSettings,
} from "@/features/settings";
import { formatDuration, isOlderThan, notSet } from "@/lib/time";
import { usePageTitle } from "@/lib/use-page-title";

export const Route = createFileRoute("/_authenticated/settings/system")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "settings.read"),
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(settingsQueryOptions),
      context.queryClient.ensureQueryData(scimQueryOptions),
    ]),
  pendingComponent: ListSkeleton,
  component: SystemPage,
});

const day = 24 * 3600;

function SystemPage() {
  usePageTitle("System settings");
  const settings = useSettings();

  return (
    <>
      <PageHeader
        title="System settings"
        description="Portal-wide settings. They come from the server environment and are read-only here."
      />
      <SettingsGroup title="Portal" description="How people reach the portal.">
        <Card>
          <CardHeader>
            <CardTitle>Identity</CardTitle>
          </CardHeader>
          <CardContent>
            <DescriptionList>
              <DescriptionRow label="Name">{settings.name || notSet}</DescriptionRow>
              <DescriptionRow label="Public URL">
                <Mono value={settings.public_url} />
              </DescriptionRow>
            </DescriptionList>
          </CardContent>
        </Card>
      </SettingsGroup>
      <SettingsGroup title="Auth" description="How people sign in and how accounts arrive.">
        <Card>
          <CardHeader>
            <CardTitle>OIDC</CardTitle>
            <CardDescription>The identity provider that signs people in.</CardDescription>
          </CardHeader>
          <CardContent>
            <DescriptionList>
              <DescriptionRow label="Issuer">
                <Mono value={settings.oidc_issuer_url} />
              </DescriptionRow>
              <DescriptionRow label="Client ID">
                <Mono value={settings.oidc_client_id} />
              </DescriptionRow>
              <DescriptionRow label="Client secret">
                {settings.oidc_client_secret_set ? (
                  <Badge variant="success">Set</Badge>
                ) : (
                  <Badge variant="muted">Not set</Badge>
                )}
              </DescriptionRow>
            </DescriptionList>
          </CardContent>
        </Card>
        <ScimCard />
      </SettingsGroup>
      <SettingsGroup title="Sessions" description="How long a sign-in lasts.">
        <Card>
          <CardHeader>
            <CardTitle>Lifetime</CardTitle>
          </CardHeader>
          <CardContent>
            <DescriptionList>
              <DescriptionRow label="Session lifetime">
                {formatDuration(settings.session_max_age_seconds)}
              </DescriptionRow>
              <DescriptionRow label="Idle timeout">
                {formatDuration(settings.session_idle_timeout_seconds)}
              </DescriptionRow>
            </DescriptionList>
          </CardContent>
        </Card>
      </SettingsGroup>
    </>
  );
}

function SettingsGroup({
  title,
  description,
  children,
}: Readonly<{ title: string; description: string; children: ReactNode }>) {
  return (
    <section className="flex flex-col gap-3">
      <div>
        <h2 className="text-lg font-medium tracking-tight">{title}</h2>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      {children}
    </section>
  );
}

function Mono({ value }: { value: string }) {
  if (!value) return <span className="text-muted-foreground">{notSet}</span>;
  return <span className="font-mono text-xs break-all">{value}</span>;
}

function ScimCard() {
  const scim = useScim();
  const can = useCan();
  const stale = !scim.last_push_at || isOlderThan(scim.last_push_at, day);

  return (
    <Card>
      <CardHeader>
        <CardTitle>SCIM provisioning</CardTitle>
        <CardDescription>
          Your identity provider pushes users and groups to this endpoint.
        </CardDescription>
        {can("access.write") && (
          <CardAction>
            <RotateScimToken />
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <DescriptionList>
          <DescriptionRow label="Endpoint">
            <span className="flex items-center gap-2">
              <Mono value={scim.endpoint} />
              <CopyButton value={scim.endpoint} label="Copy URL" />
            </span>
          </DescriptionRow>
          <DescriptionRow label="Last push">
            <RelativeTime at={scim.last_push_at} empty="No push yet" />
          </DescriptionRow>
          <DescriptionRow label="Users">{scim.users}</DescriptionRow>
          <DescriptionRow label="Groups">{scim.groups}</DescriptionRow>
        </DescriptionList>
        {stale && (
          <Alert>
            <TriangleAlert />
            <AlertTitle>No push in the last 24 hours</AlertTitle>
            <AlertDescription>
              Check that the identity provider has the endpoint URL and a valid token, then sync it.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}

function RotateScimToken() {
  const rotate = useRotateScimToken();
  const [secret, setSecret] = useState<string>();

  function confirm() {
    rotate.mutate(undefined, {
      onSuccess: (rotated) => setSecret(rotated.secret),
      onError: (error) => toast.error(error.message),
    });
  }

  return (
    <>
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button variant="outline" size="sm">
            <RotateCwIcon />
            Rotate token
          </Button>
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Rotate the SCIM token?</AlertDialogTitle>
            <AlertDialogDescription>
              The current token stops working at once. Pushes fail until you give the identity
              provider the new token.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={rotate.isPending} onClick={confirm}>
              Rotate token
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <SecretDialog title="New SCIM token" secret={secret} onClose={() => setSecret(undefined)}>
        <p className="text-muted-foreground text-sm">
          Paste it into the SCIM provider of your identity provider.
        </p>
      </SecretDialog>
    </>
  );
}
