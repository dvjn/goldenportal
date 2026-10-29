import { Link, type LinkProps } from "@tanstack/react-router";
import {
  KeyRound,
  LogOut,
  type LucideIcon,
  ScrollText,
  Settings,
  SlidersHorizontal,
  SunMoon,
  Users,
} from "lucide-react";
import { useTheme } from "@/components/theme-provider";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { type Permission, useCan, useLogout, useMe } from "@/features/auth";

type Theme = ReturnType<typeof useTheme>["theme"];

const themes: { value: Theme; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

type SettingsLink = {
  to: LinkProps["to"];
  label: string;
  icon: LucideIcon;
  permission: Permission;
};

const settingsLinks: SettingsLink[] = [
  {
    to: "/settings/catalog-setup",
    label: "Catalog setup",
    icon: SlidersHorizontal,
    permission: "catalog.read",
  },
  { to: "/settings/access", label: "Users and groups", icon: Users, permission: "access.read" },
  {
    to: "/settings/service-tokens",
    label: "Service tokens",
    icon: KeyRound,
    permission: "access.read",
  },
  { to: "/settings/audit", label: "Audit log", icon: ScrollText, permission: "audit.read" },
  { to: "/settings/system", label: "System settings", icon: Settings, permission: "settings.read" },
];

export function avatarLetter(name: string, email: string) {
  return (name.trim() || email).charAt(0).toUpperCase();
}

export function NavUser() {
  const { user } = useMe();
  const { theme, setTheme } = useTheme();
  const logout = useLogout();
  const can = useCan();
  const avatar = (
    <Avatar>
      <AvatarFallback>{avatarLetter(user.name, user.email)}</AvatarFallback>
    </Avatar>
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="rounded-full">
          {avatar}
          <span className="sr-only">Open the user menu</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="min-w-56 rounded-lg" align="end" sideOffset={4}>
        <DropdownMenuLabel className="p-0 font-normal">
          <div className="flex items-center gap-2 px-1 py-1.5">
            {avatar}
            <div className="grid flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">{user.name}</span>
              <span className="truncate text-xs">{user.email}</span>
            </div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {settingsLinks
          .filter((link) => can(link.permission))
          .map(({ to, label, icon: Icon }) => (
            <DropdownMenuItem key={label} asChild>
              <Link to={to}>
                <Icon />
                {label}
              </Link>
            </DropdownMenuItem>
          ))}
        <DropdownMenuSeparator />
        <DropdownMenuSub>
          <DropdownMenuSubTrigger>
            <SunMoon />
            Theme
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(value) => setTheme(value as Theme)}
            >
              {themes.map(({ value, label }) => (
                <DropdownMenuRadioItem key={value} value={value}>
                  {label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={logout.isPending} onSelect={() => logout.mutate()}>
          <LogOut />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
