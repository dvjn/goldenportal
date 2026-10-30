import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./style.css";
import { ThemeProvider } from "./components/theme-provider";
import { authKeys } from "./features/auth";
import { isUnauthorized } from "./lib/api";
import { createAppRouter } from "./router";

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    // A request that fails because the session ended sends the user back to sign in.
    // The route guard handles the "me" query itself.
    onError: (error, query) => {
      if (!isUnauthorized(error) || query.queryKey[0] === authKeys.me[0]) return;
      queryClient.removeQueries({ queryKey: authKeys.me });
      void router.navigate({ to: "/sign-in", search: { redirect: router.state.location.href } });
    },
  }),
});

const router = createAppRouter(queryClient);

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("Root element #root was not found.");
}

createRoot(rootElement).render(
  <StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
