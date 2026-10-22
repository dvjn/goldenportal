import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PageError } from "@/components/page-error";

export const Route = createFileRoute("/_authenticated/integrations")({
  staticData: { breadcrumb: "Integrations" },
  component: Outlet,
  errorComponent: PageError,
});
