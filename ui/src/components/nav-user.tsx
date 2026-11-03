import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronsUpDown, LogOut, type LucideIcon, Monitor, Moon, Sun } from "lucide-react";
import { pageIcons } from "@/components/page-icons";
import { type Theme, useTheme } from "@/components/theme-provider";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenuButton, useSidebar } from "@/components/ui/sidebar";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { signOut, useMe } from "@/features/auth";

const themes: { value: Theme; label: string; icon: LucideIcon }[] = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
];

function initials(name: string) {
  const words = name.trim().split(/\s+/);
  return (
    (words[0]?.[0] ?? "") + (words.length > 1 ? (words.at(-1)?.[0] ?? "") : "")
  ).toUpperCase();
}

// NavUser is the signed-in person at the foot of the sidebar, with their
// own settings: theme, connected apps and signing out.
export function NavUser() {
  const me = useMe();
  const { isMobile } = useSidebar();
  const { theme, setTheme } = useTheme();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  async function handleSignOut() {
    await signOut();
    queryClient.clear();
    await navigate({ to: "/sign-in" });
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <SidebarMenuButton
          size="lg"
          className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
          aria-label="Open user menu"
        >
          <Avatar className="size-8">
            <AvatarFallback>{initials(me.displayName)}</AvatarFallback>
          </Avatar>
          <span className="grid flex-1 text-left text-sm leading-tight">
            <span className="truncate font-medium">{me.displayName}</span>
            <span className="truncate text-xs text-muted-foreground">{me.email}</span>
          </span>
          <ChevronsUpDown className="ml-auto size-4" />
        </SidebarMenuButton>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="min-w-56 rounded-lg"
        side={isMobile ? "bottom" : "right"}
        align="end"
        sideOffset={4}
      >
        <DropdownMenuLabel className="font-normal">
          <div className="grid text-sm leading-tight">
            <span className="truncate font-medium text-foreground">{me.displayName}</span>
            <span className="truncate text-xs">{me.email}</span>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <div className="flex items-center justify-between gap-2 px-1.5 py-1 text-sm">
            <span>Theme</span>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              spacing={0}
              value={theme}
              onValueChange={(value) => value && setTheme(value as Theme)}
              aria-label="Theme"
            >
              {themes.map(({ value, label, icon: Icon }) => (
                <ToggleGroupItem key={value} value={value} aria-label={label}>
                  <Icon />
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/account/apps">
            <pageIcons.apps />
            Connected apps
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={handleSignOut}>
          <LogOut />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
