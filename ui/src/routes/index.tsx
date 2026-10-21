import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: IndexPage,
});

function IndexPage() {
  return <h1 className="p-6 text-2xl font-semibold">GoldPort</h1>;
}
