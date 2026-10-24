import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/")({
  component: IndexPage,
});

function IndexPage() {
  return null;
}
