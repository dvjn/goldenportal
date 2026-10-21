import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";
import { requirePermission } from "@/features/auth";

export const Route = createFileRoute("/_authenticated/definitions")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.model"),
  staticData: { breadcrumb: "Definitions" },
  component: Outlet,
  errorComponent: PageError,
});
