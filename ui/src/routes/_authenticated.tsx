import { createFileRoute, Outlet } from "@tanstack/react-router";
import { BreadcrumbTrailProvider, HeaderBreadcrumb } from "@/components/common/breadcrumb-trail";
import { NavUser } from "@/components/nav-user";
import { loginUrl, meQueryOptions } from "@/features/auth";
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
  component: AuthenticatedAppShell,
});

function AuthenticatedAppShell() {
  return (
    <BreadcrumbTrailProvider>
      <div className="min-h-svh bg-muted dark:bg-background">
        <header className="sticky top-0 z-10 border-b bg-background/90 backdrop-blur supports-[backdrop-filter]:bg-background/75">
          <div className="mx-auto flex h-16 w-full max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
            <HeaderBreadcrumb />
            <NavUser />
          </div>
        </header>
        <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 pt-4 pb-10 sm:px-6 sm:pt-6 lg:pb-12">
          <Outlet />
        </main>
      </div>
    </BreadcrumbTrailProvider>
  );
}
