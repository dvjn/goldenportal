import { createFileRoute, Link, Outlet, redirect, useLocation } from "@tanstack/react-router";
import { PageHeader } from "@/components/page-header";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { meQueryOptions } from "@/features/auth";

const sections = [
  { to: "/settings/access", label: "Access" },
  { to: "/settings/audit", label: "Audit log" },
] as const;

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);
    if (me.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
  component: SettingsLayout,
});

function SettingsLayout() {
  const { pathname } = useLocation();
  const active = sections.find((section) => pathname.startsWith(section.to))?.to;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Settings" description="Who can do what in the portal." />
      <Tabs value={active}>
        <TabsList variant="line" aria-label="Settings">
          {sections.map((section) => (
            <TabsTrigger
              key={section.to}
              value={section.to}
              asChild
              // A soft highlight suits underlined tabs better than the default ring.
              className="focus-visible:border-transparent focus-visible:bg-muted focus-visible:ring-0 focus-visible:outline-none"
            >
              <Link to={section.to}>{section.label}</Link>
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <Outlet />
    </div>
  );
}
