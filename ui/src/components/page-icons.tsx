import {
  AppWindow,
  ConciergeBell,
  History,
  House,
  LayoutDashboard,
  Library,
  type LucideIcon,
  Plug,
  Settings,
  Shapes,
  Stamp,
  Trophy,
  Workflow,
  Zap,
} from "lucide-react";
import { IconTile } from "@/components/catalog/blueprint-icon";

// pageIcons are the app's own pages' icons, which the sidebar and each
// page's header both show, so a page reads as the same thing in either.
export const pageIcons = {
  home: House,
  catalog: Library,
  selfService: ConciergeBell,
  scorecards: Trophy,
  runs: History,
  approvals: Stamp,
  blueprints: Shapes,
  integrations: Plug,
  dashboards: LayoutDashboard,
  actions: Zap,
  workflows: Workflow,
  settings: Settings,
  apps: AppWindow,
} as const satisfies Record<string, LucideIcon>;

export type PageName = keyof typeof pageIcons;

// sectionPages names the pages that head a section of the app, by path, so
// a breadcrumb linking to one shows its icon.
const sectionPages: Record<string, PageName> = {
  "/": "home",
  "/catalog": "catalog",
  "/self-service": "selfService",
  "/scorecards": "scorecards",
  "/runs": "runs",
  "/approvals": "approvals",
  "/blueprints": "blueprints",
  "/integrations": "integrations",
  "/dashboards": "dashboards",
  "/actions": "actions",
  "/workflows": "workflows",
  "/settings": "settings",
  "/account/apps": "apps",
};

export function sectionPage(path: unknown): PageName | undefined {
  return typeof path === "string" ? sectionPages[path] : undefined;
}

// PageIcon is a page's icon on the tile a page header leads with, as
// blueprints and documents have theirs. A section has no color of its own,
// so its tile is gray, in a breadcrumb as in its header.
export function PageIcon({ page }: Readonly<{ page: PageName }>) {
  return <IconTile Icon={pageIcons[page]} color="gray" size="lg" />;
}
