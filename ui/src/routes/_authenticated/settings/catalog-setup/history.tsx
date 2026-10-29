import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requirePermission } from "@/features/auth";

export const Route = createFileRoute("/_authenticated/settings/catalog-setup/history")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.model"),
  staticData: { breadcrumb: "History" },
  component: Outlet,
});
