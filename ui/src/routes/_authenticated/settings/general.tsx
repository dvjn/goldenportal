import { createFileRoute } from "@tanstack/react-router";
import { TriangleAlert } from "lucide-react";
import { useState } from "react";
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
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useCan } from "@/features/auth";
import {
  scimQueryOptions,
  settingsQueryOptions,
  useRotateScimToken,
  useScim,
  useSettings,
} from "@/features/settings";
import { formatDuration, formatRelative, isOlderThan } from "@/lib/time";

export const Route = createFileRoute("/_authenticated/settings/general")({
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(settingsQueryOptions),
      context.queryClient.ensureQueryData(scimQueryOptions),
    ]),
  staticData: { breadcrumb: "General" },
  component: GeneralPage,
});

const day = 24 * 3600;

function GeneralPage() {
  const settings = useSettings();

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">General</h1>
        <p className="text-muted-foreground text-sm">
          Portal-wide settings. They come from the server environment and are read-only here.
        </p>
      </header>
      <Card>
        <CardHeader>
          <CardTitle>Identity</CardTitle>
          <CardDescription>How people reach the portal and sign in.</CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-[auto_1fr]">
            <Setting label="Portal name" value={settings.name} />
            <Setting label="Public URL" value={settings.public_url} mono />
            <Setting label="OIDC issuer" value={settings.oidc_issuer_url} mono />
            <Setting label="OIDC client ID" value={settings.oidc_client_id} mono />
            <Setting
              label="OIDC client secret"
              value={settings.oidc_client_secret_set ? "Set" : "Not set"}
            />
            <Setting
              label="Session lifetime"
              value={formatDuration(settings.session_max_age_seconds)}
            />
            <Setting
              label="Session idle timeout"
              value={formatDuration(settings.session_idle_timeout_seconds)}
            />
          </dl>
        </CardContent>
      </Card>
      <ScimCard />
    </>
  );
}

function Setting({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={mono ? "font-mono break-all" : undefined}>{value}</dd>
    </>
  );
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
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <code className="min-w-0 flex-1 break-all font-mono text-sm">{scim.endpoint}</code>
          <CopyButton value={scim.endpoint} label="Copy URL" />
        </div>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-[auto_1fr]">
          <Setting
            label="Last push"
            value={scim.last_push_at ? formatRelative(scim.last_push_at) : "No push yet"}
          />
          <Setting label="Users" value={String(scim.users)} />
          <Setting label="Groups" value={String(scim.groups)} />
        </dl>
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

  return (
    <>
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button variant="outline">Rotate token</Button>
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
            <AlertDialogAction
              disabled={rotate.isPending}
              onClick={() =>
                rotate.mutate(undefined, { onSuccess: (rotated) => setSecret(rotated.secret) })
              }
            >
              Rotate token
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {rotate.isError && <p className="text-destructive text-sm">{rotate.error.message}</p>}
      <SecretDialog title="New SCIM token" secret={secret} onClose={() => setSecret(undefined)}>
        <p className="text-muted-foreground text-sm">
          Paste it into the SCIM provider of your identity provider.
        </p>
      </SecretDialog>
    </>
  );
}
