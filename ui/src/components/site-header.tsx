import { Link } from "@tanstack/react-router";
import { NavUser } from "@/components/nav-user";
import { useConfig } from "@/features/config";

export function SiteHeader() {
  const { name, logoUrl } = useConfig();

  return (
    <header className="sticky top-0 z-50 flex w-full items-center border-b bg-background">
      <div className="mx-auto flex h-(--header-height) w-full max-w-6xl items-center gap-2 px-4">
        <Link to="/" className="flex min-w-0 items-center gap-2 font-semibold">
          {logoUrl && <img src={logoUrl} alt="" className="size-7 shrink-0 object-contain" />}
          <span className="truncate">{name}</span>
        </Link>
        <div className="ml-auto">
          <NavUser />
        </div>
      </div>
    </header>
  );
}
