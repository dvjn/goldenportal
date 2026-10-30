import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";

const copiedFor = 2000;

// useCopy copies value to the clipboard and reports it as copied for a moment.
function useCopy(value: string) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), copiedFor);
    return () => clearTimeout(timer);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      toast.error("Couldn't copy to the clipboard; select the text and copy it instead");
    }
  }

  return { copied, copy };
}

// CopyButton copies value, with a label beside the icon.
export function CopyButton({ value }: Readonly<{ value: string }>) {
  const { copied, copy } = useCopy(value);

  return (
    <Button type="button" variant="outline" onClick={copy}>
      {copied ? <Check /> : <Copy />}
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}

// CopyIconButton copies value from a compact, icon-only button. label names
// what it copies, e.g. "push endpoint".
export function CopyIconButton({ value, label }: Readonly<{ value: string; label: string }>) {
  const { copied, copy } = useCopy(value);

  return (
    <IconButton label={copied ? "Copied" : `Copy ${label}`} onClick={copy}>
      {copied ? <Check /> : <Copy />}
    </IconButton>
  );
}
