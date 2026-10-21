import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";
import { PageBreadcrumbs } from "./page-breadcrumbs";

function renderAt(path: string) {
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <PageBreadcrumbs />
        <Outlet />
      </>
    ),
  });
  const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: "/" });
  const catalogRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/catalog",
    staticData: { breadcrumb: "Catalog" },
  });
  const serviceRoute = createRoute({
    getParentRoute: () => catalogRoute,
    path: "$service",
    staticData: { breadcrumb: "Service" },
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([homeRoute, catalogRoute.addChildren([serviceRoute])]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  return render(<RouterProvider router={router} />);
}

describe("PageBreadcrumbs", () => {
  it("shows nothing on Home", async () => {
    const { container } = renderAt("/");

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(container).toBeEmptyDOMElement();
  });

  it("links every level above the current page", async () => {
    renderAt("/catalog/payments");

    expect(await screen.findByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Catalog" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByText("Service")).toHaveAttribute("aria-current", "page");
  });
});
