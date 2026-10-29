import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/settings/configuration/$")({
  beforeLoad: ({ location }) => {
    throw redirect({
      href: location.href.replace("/settings/configuration", "/settings/catalog-setup"),
      replace: true,
    });
  },
});
