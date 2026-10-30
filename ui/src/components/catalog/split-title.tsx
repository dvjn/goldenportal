// SplitTitle dims the namespace of a title like group/name, so the name stands out.
export function SplitTitle({ title }: Readonly<{ title: string }>) {
  const cut = title.lastIndexOf("/") + 1;
  if (cut <= 0 || cut >= title.length) return title;
  return (
    <>
      <span className="text-muted-foreground">{title.slice(0, cut)}</span>
      {title.slice(cut)}
    </>
  );
}
