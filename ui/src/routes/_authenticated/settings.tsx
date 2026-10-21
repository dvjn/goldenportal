import { createFileRoute, Link, Outlet, useMatchRoute } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { Button } from "@/components/ui/button";
import { type Permission, requirePermission, useCan } from "@/features/auth";
import { settingsQueryOptions } from "@/features/settings";

export const Route = createFileRoute("/_authenticated/settings")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "settings.read"),
  loader: ({ context }) => context.queryClient.ensureQueryData(settingsQueryOptions),
  staticData: { breadcrumb: "System settings" },
  component: SettingsLayout,
  errorComponent: PageError,
});

type Section = {
  to: "/settings/general";
  label: string;
  permission: Permission;
};

const sections: Section[] = [
  { to: "/settings/general", label: "General", permission: "settings.read" },
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
        <Outlet />
      </div>
    </div>
  );
}
