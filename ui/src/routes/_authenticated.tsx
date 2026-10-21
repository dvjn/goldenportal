import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { NavUser } from "@/components/nav-user";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { loginUrl, meQueryOptions } from "@/features/auth";
import { useConfig } from "@/features/config";
import { isUnauthorized } from "@/lib/api";
import { navigateAway } from "@/lib/navigation";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (!isUnauthorized(error)) throw error;
      navigateAway(loginUrl(location.pathname + location.searchStr));
      // The browser is leaving for the sign-in page; stay pending so nothing renders.
      await new Promise(() => {});
    }
  },
  component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
  const portalName = useConfig().name;

  return (
    <div className="flex min-h-svh flex-col">
      <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b px-4">
        <Link to="/" className="min-w-0 truncate font-semibold">
          {portalName}
        </Link>
        <NavUser />
      </header>
      <main className="flex flex-1 flex-col gap-4 p-4">
        <PageBreadcrumbs />
        <Outlet />
      </main>
    </div>
  );
}
