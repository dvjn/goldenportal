import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

type RouterContext = {
  queryClient: QueryClient;
};

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

function RootLayout() {
  return (
    <main className="min-h-screen">
      <Outlet />
    </main>
  );
}
