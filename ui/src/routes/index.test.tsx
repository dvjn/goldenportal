import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { expect, it } from "vite-plus/test";
import { routeTree } from "../routeTree.gen";

it("renders the GoldPort heading", async () => {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
    context: { queryClient: new QueryClient() },
  });

  render(<RouterProvider router={router} />);

  expect(await screen.findByRole("heading", { name: "GoldPort" })).toBeInTheDocument();
});
