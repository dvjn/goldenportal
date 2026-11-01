import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireRole } from "@/features/auth";

export const Route = createFileRoute("/_app/integrations")({
  beforeLoad: ({ context }) => requireRole(context.queryClient, "builder"),
  component: Outlet,
});
