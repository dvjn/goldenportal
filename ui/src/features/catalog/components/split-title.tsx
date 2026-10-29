import { leafTitle, splitTitle } from "../format";

export function SplitTitle({ title }: Readonly<{ title: string }>) {
  const { namespace, name } = splitTitle(title);
  return (
    <>
      <span className="text-muted-foreground">{namespace}</span>
      {name}
    </>
  );
}

export function LeafTitle({ title }: Readonly<{ title: string }>) {
  return (
    <>
      <span aria-hidden="true">{leafTitle(title)}</span>
      <span className="sr-only">{title}</span>
    </>
  );
}
