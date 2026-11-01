import { createFileRoute, Link, Outlet, useLocation } from "@tanstack/react-router";
import { PageHeader } from "@/components/page-header";
import { PageTab } from "@/components/page-tabs";
import { settingsSections } from "@/components/settings-sections";
import { Tabs, TabsList } from "@/components/ui/tabs";
import { requireRole } from "@/features/auth";

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: ({ context }) => requireRole(context.queryClient, "admin"),
  component: SettingsLayout,
});

function SettingsLayout() {
  const { pathname } = useLocation();
  const active = settingsSections.find((section) => pathname.startsWith(section.to))?.to;

  return (
    <Tabs value={active} className="gap-4">
      <PageHeader
        title="Settings"
        tabs={
          <TabsList variant="line" aria-label="Settings">
            {settingsSections.map((section) => (
              <PageTab key={section.to} value={section.to} asChild>
                <Link to={section.to}>{section.label}</Link>
              </PageTab>
            ))}
          </TabsList>
        }
      />
      <Outlet />
    </Tabs>
  );
}
