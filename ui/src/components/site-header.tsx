import { Link } from "@tanstack/react-router";
import { NavUser } from "@/components/nav-user";
import {
  NavigationMenu,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  navigationMenuTriggerStyle,
} from "@/components/ui/navigation-menu";
import { hasRole, type Role, useMe } from "@/features/auth";
import { useConfig } from "@/features/config";

const navItems: { to: string; label: string; role: Role }[] = [
  { to: "/blueprints", label: "Blueprints", role: "builder" },
];

export function SiteHeader() {
  const { name, logoUrl } = useConfig();
  const me = useMe();
  const items = navItems.filter((item) => hasRole(me.role, item.role));

  return (
    <header className="sticky top-0 z-50 flex w-full items-center border-b bg-background">
      <div className="mx-auto flex h-(--header-height) w-full max-w-6xl items-center gap-6 px-4">
        <Link to="/" className="flex min-w-0 items-center gap-2 font-semibold">
          {logoUrl && <img src={logoUrl} alt="" className="size-7 shrink-0 object-contain" />}
          <span className="truncate">{name}</span>
        </Link>
        {items.length > 0 && (
          <NavigationMenu>
            <NavigationMenuList>
              {items.map((item) => (
                <NavigationMenuItem key={item.to}>
                  <NavigationMenuLink asChild className={navigationMenuTriggerStyle()}>
                    <Link to={item.to} activeProps={{ "data-active": true }}>
                      {item.label}
                    </Link>
                  </NavigationMenuLink>
                </NavigationMenuItem>
              ))}
            </NavigationMenuList>
          </NavigationMenu>
        )}
        <div className="ml-auto">
          <NavUser />
        </div>
      </div>
    </header>
  );
}
