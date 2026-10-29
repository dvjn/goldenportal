import { brandLabel, hasBrandMark, SourceIcon } from "@/components/common/source-icon";

export function SourceMark({ icon, className }: Readonly<{ icon: string; className?: string }>) {
  if (!hasBrandMark(icon)) return null;
  return <SourceIcon icon={icon} label={brandLabel(icon)} className={className} />;
}
