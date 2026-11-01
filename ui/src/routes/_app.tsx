import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { AppSidebar } from "@/components/app-sidebar";
import { PageBar, PageBarProvider } from "@/components/page-bar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { meQueryOptions } from "@/features/auth";
import { useUiState } from "@/hooks/use-ui-state";
import { isUnauthorized } from "@/lib/api";

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (isUnauthorized(error)) {
        throw redirect({ to: "/sign-in", search: { redirect: location.href } });
      }
      throw error;
    }
  },
  component: AppLayout,
});

function AppLayout() {
  const [sidebarOpen, setSidebarOpen] = useUiState("sidebarOpen", true);

  return (
    <TooltipProvider>
      <SidebarProvider open={sidebarOpen} onOpenChange={setSidebarOpen}>
        <AppSidebar />
        <SidebarInset>
          <PageBarProvider>
            <PageBar />
            <div className="mx-auto flex w-full max-w-6xl flex-1 flex-col gap-6 px-4 pt-4 pb-10 sm:px-6 sm:pt-6 lg:pb-12">
              <Outlet />
            </div>
          </PageBarProvider>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}
