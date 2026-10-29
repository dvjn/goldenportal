import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/definitions/history/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/catalog-setup/history", replace: true });
  },
});
