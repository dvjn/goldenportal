import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { SiteHeader } from "@/components/site-header";
import { meQueryOptions } from "@/features/auth";
import { isUnauthorized } from "@/lib/api";

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (isUnauthorized(error)) {
        throw redirect({ to: "/sign-in", search: { redirect: location.href } });
      }
      throw error;
    }
  },
  component: AppLayout,
});

function AppLayout() {
  return (
    <div className="flex min-h-svh flex-col [--header-height:calc(--spacing(14))]">
      <SiteHeader />
      <main className="mx-auto flex w-full max-w-6xl flex-1 flex-col gap-4 p-4">
        <Outlet />
      </main>
    </div>
  );
}
