import { SearchIcon } from "lucide-react";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";

export function SearchBox({
  value,
  label,
  onChange,
}: Readonly<{
  value: string;
  label: string;
  onChange: (value: string) => void;
}>) {
  return (
    <InputGroup className="bg-background sm:max-w-sm">
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </InputGroup>
  );
}
