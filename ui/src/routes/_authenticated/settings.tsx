import { createFileRoute, Outlet } from "@tanstack/react-router";
import { ShieldAlert } from "lucide-react";
import { useState } from "react";
import { PageError } from "@/components/page-error";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { GrantDialog, grantsQueryOptions, subjectsQueryOptions } from "@/features/access";
import { meQueryOptions, useCan } from "@/features/auth";
import { settingsQueryOptions, useSettings } from "@/features/settings";

export const Route = createFileRoute("/_authenticated/settings")({
  loader: async ({ context: { queryClient } }) => {
    const { permissions } = await queryClient.ensureQueryData(meQueryOptions);
    const settingsData = permissions.includes("settings.read")
      ? [queryClient.ensureQueryData(settingsQueryOptions)]
      : [];
    const grantDialogData = permissions.includes("access.write")
      ? [
          queryClient.ensureQueryData(grantsQueryOptions),
          queryClient.ensureQueryData(subjectsQueryOptions),
        ]
      : [];
    await Promise.all([...settingsData, ...grantDialogData]);
  },
  component: SettingsLayout,
  errorComponent: PageError,
});

function SettingsLayout() {
  const can = useCan();

  return (
    <>
      {can("settings.read") && <BootstrapBanner />}
      <Outlet />
    </>
  );
}

function BootstrapBanner() {
  const { bootstrap } = useSettings();
  const can = useCan();
  const [granting, setGranting] = useState(false);

  if (bootstrap.admin_granted) return null;

  return (
    <Alert>
      <ShieldAlert />
      <AlertTitle>Access relies on bootstrap admins</AlertTitle>
      <AlertDescription>
        GOLDPORT_BOOTSTRAP_ADMINS grants Admin to {bootstrap.admins.join(", ")}. No Admin grant
        exists yet, so grant Admin to a user or group.
      </AlertDescription>
      {can("access.write") && (
        <AlertAction>
          <Button size="sm" onClick={() => setGranting(true)}>
            Grant access
          </Button>
          <GrantDialog open={granting} onOpenChange={setGranting} />
        </AlertAction>
      )}
    </Alert>
  );
}
