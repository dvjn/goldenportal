import { createFileRoute, Link, Outlet, useMatchRoute } from "@tanstack/react-router";
import { ShieldAlert } from "lucide-react";
import { useState } from "react";
import { PageError } from "@/components/page-error";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { GrantDialog, grantsQueryOptions, subjectsQueryOptions } from "@/features/access";
import { meQueryOptions, type Permission, requirePermission, useCan } from "@/features/auth";
import { settingsQueryOptions, useSettings } from "@/features/settings";

export const Route = createFileRoute("/_authenticated/settings")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "settings.read"),
  loader: async ({ context: { queryClient } }) => {
    const me = await queryClient.ensureQueryData(meQueryOptions);
    const grantDialogData = me.permissions.includes("access.write")
      ? [
          queryClient.ensureQueryData(grantsQueryOptions),
          queryClient.ensureQueryData(subjectsQueryOptions),
        ]
      : [];
    await Promise.all([queryClient.ensureQueryData(settingsQueryOptions), ...grantDialogData]);
  },
  staticData: { breadcrumb: "System settings" },
  component: SettingsLayout,
  errorComponent: PageError,
});

type Section = {
  to: "/settings/general" | "/settings/access";
  label: string;
  permission: Permission;
};

const sections: Section[] = [
  { to: "/settings/general", label: "General", permission: "settings.read" },
  { to: "/settings/access", label: "Access", permission: "access.read" },
];

function SettingsLayout() {
  const can = useCan();
  const matchRoute = useMatchRoute();

  return (
    <div className="flex flex-col gap-6 md:flex-row">
      <nav aria-label="System settings" className="flex shrink-0 flex-col gap-1 md:w-48">
        <h2 className="px-3 py-1 font-semibold">System settings</h2>
        {sections
          .filter((section) => can(section.permission))
          .map((section) => (
            <Button
              key={section.to}
              asChild
              variant={matchRoute({ to: section.to, fuzzy: true }) ? "secondary" : "ghost"}
              className="justify-start"
            >
              <Link to={section.to}>{section.label}</Link>
            </Button>
          ))}
      </nav>
      <div className="flex min-w-0 flex-1 flex-col gap-6">
        <BootstrapBanner />
        <Outlet />
      </div>
    </div>
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
