import { useQuery } from "@tanstack/react-query";
import { Link, useMatchRoute, useSearch } from "@tanstack/react-router";
import { cn } from "cn";
import { ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { pageIcons } from "@/components/page-icons";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { type HomeDashboard, homeQueryOptions, openDashboard } from "@/features/dashboards";

type Group = { title: string; icon?: string; views: HomeDashboard[] };

// fold gathers each group's dashboards where the first of them would sit.
function fold(views: HomeDashboard[]): (HomeDashboard | Group)[] {
  const out: (HomeDashboard | Group)[] = [];
  const groups = new Map<string, Group>();
  for (const v of views) {
    if (!v.group) {
      out.push(v);
      continue;
    }
    const group = groups.get(v.group.title);
    if (group) {
      group.views.push(v);
      continue;
    }
    const added = { title: v.group.title, icon: v.group.icon, views: [v] };
    groups.set(added.title, added);
    out.push(added);
  }
  return out;
}

const chevron = "transition-transform group-data-[state=open]/fold:rotate-90";

// Rows under Home would keep clear of Home's fold arrow as if it were
// theirs, so their own arrows would sit out of line with it.
const nested = "group-has-data-[sidebar=menu-action]/menu-item:pr-2";

// HomeNav is Home in the sidebar, folding open to the dashboards it shows,
// each group a fold of its own, so the sidebar says which one Home shows.
// It starts open one level, its groups shut but for the one holding the
// open dashboard.
// Home is marked itself while the list is out of sight: folded shut, with
// the sidebar down to icons, or with one dashboard, which needs no list.
export function HomeNav({ active }: Readonly<{ active: boolean }>) {
  const views = useQuery(homeQueryOptions).data ?? [];
  const [expanded, setExpanded] = useState(true);
  const iconsOnly = useSidebar().state === "collapsed";
  const listed = views.length > 1;
  const home = (
    <SidebarMenuButton
      asChild
      isActive={active && (!listed || !expanded || iconsOnly)}
      tooltip="Home"
    >
      <Link to="/">
        <pageIcons.home />
        <span>Home</span>
      </Link>
    </SidebarMenuButton>
  );
  if (!listed) return <SidebarMenuItem>{home}</SidebarMenuItem>;
  return (
    <Collapsible open={expanded} onOpenChange={setExpanded} asChild>
      <SidebarMenuItem>
        {home}
        <CollapsibleTrigger asChild>
          <SidebarMenuAction className="group/fold" aria-label="Dashboards">
            <ChevronRight className={chevron} />
          </SidebarMenuAction>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Dashboards views={views} />
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  );
}

function Dashboards({ views }: Readonly<{ views: HomeDashboard[] }>) {
  const matchRoute = useMatchRoute();
  const { d } = useSearch({ strict: false });
  const open = matchRoute({ to: "/" })
    ? openDashboard(views, typeof d === "string" ? d : undefined)
    : undefined;
  return (
    // Each level steps in a little, its rows' highlight starting there.
    <SidebarMenu className="mt-1 gap-1 pl-4 group-data-[collapsible=icon]:hidden">
      {fold(views).map((tab) =>
        "views" in tab ? (
          <GroupFold key={tab.title} group={tab} open={open} />
        ) : (
          <SidebarMenuItem key={tab.identifier}>
            <DashboardLink view={tab} open={open} />
          </SidebarMenuItem>
        ),
      )}
    </SidebarMenu>
  );
}

function GroupFold({ group, open }: Readonly<{ group: Group; open?: HomeDashboard }>) {
  const inside = open !== undefined && group.views.includes(open);
  const [expanded, setExpanded] = useState(inside);
  // Opening one of its dashboards from elsewhere unfolds it, so the open
  // one always shows.
  useEffect(() => {
    if (inside) setExpanded(true);
  }, [inside]);
  return (
    <Collapsible open={expanded} onOpenChange={setExpanded} asChild>
      <SidebarMenuItem>
        <CollapsibleTrigger asChild>
          <SidebarMenuButton className={cn("group/fold", nested)}>
            {group.icon && <BlueprintIcon icon={group.icon} />}
            <span className="truncate">{group.title}</span>
            {/* Nudged into line with Home's, which sits in the row's action slot. */}
            <ChevronRight className={cn("-mr-0.5 ml-auto", chevron)} />
          </SidebarMenuButton>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <SidebarMenu className="mt-1 gap-1 pl-4">
            {group.views.map((v) => (
              <SidebarMenuItem key={v.identifier}>
                <DashboardLink view={v} open={open} />
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  );
}

// DashboardLink opens a dashboard on Home.
function DashboardLink({ view, open }: Readonly<{ view: HomeDashboard; open?: HomeDashboard }>) {
  const current = view === open;
  return (
    <SidebarMenuButton asChild isActive={current} className={nested}>
      <Link
        to="/"
        search={{ d: view.identifier }}
        // The router marks it only when the URL names it, not when Home
        // opens it by default.
        aria-current={current ? "page" : undefined}
      >
        <BlueprintIcon icon={view.icon} />
        <span>{view.title}</span>
      </Link>
    </SidebarMenuButton>
  );
}
