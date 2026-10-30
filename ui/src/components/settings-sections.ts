import { type LucideIcon, ScrollText, Server, ShieldCheck } from "lucide-react";

// settingsSections are the admin settings pages, as tabs and in the user menu.
export const settingsSections = [
  { to: "/settings/configuration", label: "Server Configuration", icon: Server },
  { to: "/settings/access", label: "Access", icon: ShieldCheck },
  { to: "/settings/audit", label: "Audit Log", icon: ScrollText },
] as const satisfies readonly { to: string; label: string; icon: LucideIcon }[];
