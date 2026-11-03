import { Link, useMatchRoute } from "@tanstack/react-router";
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
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { hasRole, useMe } from "@/features/auth";
import { useConfig } from "@/features/config";

const mainItems = [
  { to: "/", label: "Home", icon: pageIcons.home, exact: true },
  { to: "/catalog", label: "Catalog", icon: pageIcons.catalog, exact: false },
  { to: "/scorecards", label: "Scorecards", icon: pageIcons.scorecards, exact: false },
] as const;

const buildItems = [
  { to: "/blueprints", label: "Blueprints", icon: pageIcons.blueprints },
  { to: "/integrations", label: "Integrations", icon: pageIcons.integrations },
  { to: "/dashboards", label: "Dashboards", icon: pageIcons.dashboards },
] as const;

// AppSidebar is the app's navigation. Members get Home, the catalog and scorecards;
// builders also get what they build, and admins Settings.
export function AppSidebar() {
  const { name, logoUrl } = useConfig();
  const { role } = useMe();
  const matchRoute = useMatchRoute();
  const active = (to: string, fuzzy = true) => Boolean(matchRoute({ to, fuzzy }));
  const builder = hasRole(role, "builder");

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
            {mainItems.map(({ to, label, icon: Icon, exact }) => (
              <SidebarMenuItem key={to}>
                <SidebarMenuButton asChild isActive={active(to, !exact)} tooltip={label}>
                  <Link to={to}>
                    <Icon />
                    <span>{label}</span>
                  </Link>
                </SidebarMenuButton>
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
