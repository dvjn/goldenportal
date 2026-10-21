import { Link } from "@tanstack/react-router";
import type * as React from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarRail,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { useConfig } from "@/features/config";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const portalName = useConfig().name;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <div className="flex h-8 items-center gap-1 overflow-hidden">
          <SidebarTrigger />
          <Link
            to="/"
            className="min-w-0 truncate whitespace-nowrap font-semibold transition-opacity duration-200 ease-linear group-data-[collapsible=icon]:opacity-0"
          >
            {portalName}
          </Link>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarMenu />
        </SidebarGroup>
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  );
}
