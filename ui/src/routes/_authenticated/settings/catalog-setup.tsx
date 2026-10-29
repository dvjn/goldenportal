import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { requirePermission } from "@/features/auth";

export const Route = createFileRoute("/_authenticated/settings/catalog-setup")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.read"),
  staticData: { breadcrumb: "Catalog setup" },
  component: Outlet,
  errorComponent: PageError,
});
