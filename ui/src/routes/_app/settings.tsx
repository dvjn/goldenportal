import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { buttonVariants } from "@/components/ui/button";
import { meQueryOptions } from "@/features/auth";

const sections = [
  { to: "/settings/users", label: "Users" },
  { to: "/settings/groups", label: "Groups" },
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
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-xl font-semibold">Settings</h1>
      <div className="flex flex-col gap-6 md:flex-row">
        <nav aria-label="Settings" className="flex gap-1 md:w-44 md:shrink-0 md:flex-col">
          {sections.map((section) => (
            <Link
              key={section.to}
              to={section.to}
              className={buttonVariants({ variant: "ghost", className: "justify-start" })}
              activeProps={{ className: "bg-muted text-foreground", "aria-current": "page" }}
            >
              {section.label}
            </Link>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
