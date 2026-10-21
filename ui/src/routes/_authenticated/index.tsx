import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/")({
  component: IndexPage,
});

function IndexPage() {
  return null;
}
