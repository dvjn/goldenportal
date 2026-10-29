import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/integrations/")({
  beforeLoad: () => {
    throw redirect({
      to: "/settings/catalog-setup",
      search: { tab: "integrations" },
      replace: true,
    });
  },
});
