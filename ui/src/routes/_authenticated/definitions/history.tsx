import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/definitions/history")({
  staticData: { breadcrumb: "History" },
  component: Outlet,
});
