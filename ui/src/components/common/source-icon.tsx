import { cn } from "cn";
import { type LucideIcon, PlugIcon } from "lucide-react";
import type { SVGProps } from "react";

type BrandMark = {
  label: string;
  color: string;
  paths: string[];
  viewBox?: string;
  fillRule?: "evenodd";
};

// Brand paths from Simple Icons (CC0), https://simpleicons.org
const brandMarks: Record<string, BrandMark> = {
  gitlab: {
    label: "GitLab",
    color: "#FC6D26",
    paths: [
      "m23.6004 9.5927-.0337-.0862L20.3.9814a.851.851 0 0 0-.3362-.405.8748.8748 0 0 0-.9997.0539.8748.8748 0 0 0-.29.4399l-2.2055 6.748H7.5375l-2.2057-6.748a.8573.8573 0 0 0-.29-.4412.8748.8748 0 0 0-.9997-.0537.8585.8585 0 0 0-.3362.4049L.4332 9.5015l-.0325.0862a6.0657 6.0657 0 0 0 2.0119 7.0105l.0113.0087.03.0213 4.976 3.7264 2.462 1.8633 1.4995 1.1321a1.0085 1.0085 0 0 0 1.2197 0l1.4995-1.1321 2.4619-1.8633 5.006-3.7489.0125-.01a6.0682 6.0682 0 0 0 2.0094-7.003z",
    ],
  },
  sonarqube: {
    label: "SonarQube",
    color: "#4E9BCD",
    paths: [
      "M12 0a.774.774 0 0 0-.775.775c0 .43.346.776.775.776 5.762 0 10.45 4.687 10.45 10.449 0 .43.345.775.775.775A.774.774 0 0 0 24 12c0-6.616-5.384-12-12-12zm0 3.932a.774.774 0 0 0-.775.775c0 .43.346.775.775.775A6.524 6.524 0 0 1 18.518 12c0 .43.346.775.775.775.43 0 .775-.346.775-.775 0-4.448-3.62-8.068-8.068-8.068zm0 3.925a.774.774 0 0 0-.775.776c0 .43.346.775.775.775A2.597 2.597 0 0 1 14.592 12c0 .43.346.775.775.775.43 0 .776-.346.776-.775A4.145 4.145 0 0 0 12 7.857zM.775 11.225A.774.774 0 0 0 0 12c0 6.616 5.384 12 12 12 .43 0 .775-.346.775-.775a.774.774 0 0 0-.775-.776C6.238 22.45 1.55 17.762 1.55 12a.774.774 0 0 0-.775-.775zm3.932 0a.774.774 0 0 0-.775.775c0 4.448 3.62 8.068 8.068 8.068.43 0 .775-.346.775-.775a.774.774 0 0 0-.775-.775A6.524 6.524 0 0 1 5.482 12a.774.774 0 0 0-.775-.775zm3.926 0a.774.774 0 0 0-.776.775A4.145 4.145 0 0 0 12 16.143c.43 0 .775-.347.775-.776a.774.774 0 0 0-.775-.775A2.597 2.597 0 0 1 9.408 12a.774.774 0 0 0-.775-.775z",
    ],
  },
  authentik: {
    label: "Authentik",
    color: "#FD4B2D",
    paths: [
      "M13.96 9.01h-.84V7.492h-1.234v3.663H5.722c.34.517.538.982.538 1.152 0 .46-1.445 3.059-3.197 3.059C.8 15.427-.745 12.8.372 10.855a3.062 3.062 0 0 1 2.691-1.606c1.04 0 1.971.915 2.557 1.755V6.577a3.773 3.773 0 0 1 3.77-3.769h10.84C22.31 2.808 24 4.5 24 6.577v10.845a3.773 3.773 0 0 1-3.77 3.769h-1.6V17.5h-7.64v3.692h-1.6a3.773 3.773 0 0 1-3.77-3.769v-3.41h12.114v-6.52h-1.59v.893h-.84v-.893H13.96v1.516Zm-9.956 1.845c-.662-.703-1.578-.544-2.209 0-2.105 2.054 1.338 5.553 3.302 1.447a5.395 5.395 0 0 0-1.093-1.447Z",
    ],
  },
  portal: {
    label: "Portal",
    color: "#E0A526",
    viewBox: "0 0 32 32",
    fillRule: "evenodd",
    paths: [
      "M8 0h16a8 8 0 0 1 8 8v16a8 8 0 0 1-8 8H8a8 8 0 0 1-8-8V8a8 8 0 0 1 8-8Zm2 25V15a6 6 0 0 1 12 0v10h-3.5V15a2.5 2.5 0 0 0-5 0v10Z",
    ],
  },
};

export function brandLabel(icon: string): string | undefined {
  return brandMarks[icon]?.label;
}

export function hasBrandMark(icon: string): boolean {
  return icon in brandMarks;
}

function BrandSvg({
  mark,
  ...props
}: Readonly<{ mark: BrandMark } & Omit<SVGProps<SVGSVGElement>, "viewBox">>) {
  return (
    <svg viewBox={mark.viewBox ?? "0 0 24 24"} fill={mark.color} {...props}>
      <title>{mark.label}</title>
      {mark.paths.map((path) => (
        <path key={path} d={path} fillRule={mark.fillRule} />
      ))}
    </svg>
  );
}

export function SourceIcon({
  icon,
  label,
  className,
  fallback: Fallback = PlugIcon,
}: Readonly<{
  icon: string;
  label?: string;
  className?: string;
  fallback?: LucideIcon;
}>) {
  const accessibility = label
    ? { role: "img", "aria-label": label }
    : { "aria-hidden": true as const };
  const mark = brandMarks[icon];
  if (!mark) {
    return (
      <Fallback data-source-icon={icon} className={cn("shrink-0", className)} {...accessibility} />
    );
  }
  return (
    <BrandSvg
      mark={mark}
      data-source-icon={icon}
      className={cn("shrink-0", className)}
      {...accessibility}
    />
  );
}
