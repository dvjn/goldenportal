import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { hasRole, meQueryOptions } from "@/features/auth";

export const Route = createFileRoute("/_app/blueprints")({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);
    if (!hasRole(me.role, "builder")) {
      throw redirect({ to: "/" });
    }
  },
  component: Outlet,
});
