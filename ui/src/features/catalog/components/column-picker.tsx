import { Columns3Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { Column } from "../columns";
import { toggleValue } from "../search";

export function ColumnPicker({
  options,
  visible,
  onChange,
  onReset,
}: Readonly<{
  options: Column[];
  visible: string[];
  onChange: (visible: string[]) => void;
  onReset: () => void;
}>) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="outline">
          <Columns3Icon aria-hidden="true" />
          Columns
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-80 w-56 overflow-auto">
        {options.map((column) => (
          <DropdownMenuCheckboxItem
            key={column.key}
            checked={visible.includes(column.key)}
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={() => onChange(toggleValue(visible, column.key))}
          >
            {column.label}
          </DropdownMenuCheckboxItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onReset}>Reset</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
