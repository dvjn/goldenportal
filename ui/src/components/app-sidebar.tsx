import { useQuery } from "@tanstack/react-query";
import { Link, useMatchRoute } from "@tanstack/react-router";
import { HomeNav } from "@/components/dashboards/dashboard-nav";
import { NavUser } from "@/components/nav-user";
import { pageIcons } from "@/components/page-icons";
import { SearchCommand } from "@/components/search-command";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { approvalsQueryOptions } from "@/features/actions";
import { useConfig } from "@/features/app-config";
import { hasRole, useMe } from "@/features/auth";

const mainItems = [
  { to: "/", label: "Home", icon: pageIcons.home, exact: true },
  { to: "/catalog", label: "Catalog", icon: pageIcons.catalog, exact: false },
  { to: "/self-service", label: "Self-Service", icon: pageIcons.selfService, exact: false },
  { to: "/scorecards", label: "Scorecards", icon: pageIcons.scorecards, exact: false },
] as const;

const activityItems = [
  { to: "/runs", label: "Runs", icon: pageIcons.runs },
  { to: "/approvals", label: "Approvals", icon: pageIcons.approvals },
] as const;

const buildItems = [
  { to: "/blueprints", label: "Blueprints", icon: pageIcons.blueprints },
  { to: "/integrations", label: "Integrations", icon: pageIcons.integrations },
  { to: "/dashboards", label: "Dashboards", icon: pageIcons.dashboards },
  { to: "/actions", label: "Actions", icon: pageIcons.actions },
  { to: "/workflows", label: "Workflows", icon: pageIcons.workflows },
] as const;

// AppSidebar is the app's navigation. Members get Home, with its dashboards under
// it, the catalog, self-service and scorecards, and their activity: their runs and
// what waits on their approval, counted; builders also get what they build, and
// admins Settings.
export function AppSidebar() {
  const { name, logoUrl } = useConfig();
  const { role } = useMe();
  const matchRoute = useMatchRoute();
  const active = (to: string, fuzzy = true) => Boolean(matchRoute({ to, fuzzy }));
  const builder = hasRole(role, "builder");
  const waiting = useQuery(approvalsQueryOptions).data?.length ?? 0;

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip={name}>
              <Link to="/">
                {logoUrl ? (
                  <img src={logoUrl} alt="" className="size-8 shrink-0 object-contain" />
                ) : (
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                    <pageIcons.home className="size-4" />
                  </span>
                )}
                <span className="truncate font-semibold">{name}</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <SidebarMenu>
          <SearchCommand />
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarMenu className="gap-1">
            {mainItems.map(({ to, label, icon: Icon, exact }) =>
              to === "/" ? (
                <HomeNav key={to} active={active(to, !exact)} />
              ) : (
                <SidebarMenuItem key={to}>
                  <SidebarMenuButton asChild isActive={active(to, !exact)} tooltip={label}>
                    <Link to={to}>
                      <Icon />
                      <span>{label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ),
            )}
          </SidebarMenu>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>Activity</SidebarGroupLabel>
          <SidebarMenu className="gap-1">
            {activityItems.map(({ to, label, icon: Icon }) => (
              <SidebarMenuItem key={to}>
                <SidebarMenuButton asChild isActive={active(to)} tooltip={label}>
                  <Link to={to}>
                    <Icon />
                    <span>{label}</span>
                  </Link>
                </SidebarMenuButton>
                {to === "/approvals" && waiting > 0 && (
                  <SidebarMenuBadge aria-label={`${waiting} waiting on you`}>
                    {waiting}
                  </SidebarMenuBadge>
                )}
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>
        {builder && (
          <SidebarGroup>
            <SidebarGroupLabel>Build</SidebarGroupLabel>
            <SidebarMenu className="gap-1">
              {buildItems.map(({ to, label, icon: Icon }) => (
                <SidebarMenuItem key={to}>
                  <SidebarMenuButton asChild isActive={active(to)} tooltip={label}>
                    <Link to={to}>
                      <Icon />
                      <span>{label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroup>
        )}
        {role === "admin" && (
          <SidebarGroup>
            <SidebarGroupLabel>Admin</SidebarGroupLabel>
            <SidebarMenu className="gap-1">
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={active("/settings")} tooltip="Settings">
                  <Link to="/settings">
                    <pageIcons.settings />
                    <span>Settings</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroup>
        )}
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <NavUser />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
