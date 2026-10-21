import { type ChangeEvent, type FormEvent, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import { type Plan, problemsOf } from "./api";
import { ProblemList } from "./problem-list";
import { usePlanDefinitions } from "./queries";

type Mode = "merge" | "replace";

const modes: { mode: Mode; title: string; description: string }[] = [
  {
    mode: "merge",
    title: "Add to and update the current definitions",
    description: "Documents in the upload replace the ones with the same kind and identifier.",
  },
  {
    mode: "replace",
    title: "Replace all definitions",
    description: "Documents that the upload does not hold are removed.",
  },
];

type UploadDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPlanned: (plan: Plan) => void;
};

export function UploadDialog({ open, onOpenChange, onPlanned }: UploadDialogProps) {
  const [source, setSource] = useState("");
  const [mode, setMode] = useState<Mode>("merge");
  const plan = usePlanDefinitions();
  const problems = problemsOf(plan.error);

  function close(next: boolean) {
    if (!next) {
      plan.reset();
      setSource("");
      setMode("merge");
    }
    onOpenChange(next);
  }

  async function read(event: ChangeEvent<HTMLInputElement>) {
    const texts = await Promise.all([...(event.target.files ?? [])].map((file) => file.text()));
    setSource(texts.map((text) => text.replace(/\s+$/, "\n")).join("---\n"));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    plan.mutate(
      { source, partial: mode === "merge" },
      {
        onSuccess: (planned) => {
          close(false);
          onPlanned(planned);
        },
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={submit} className="flex flex-col gap-6">
          <DialogHeader>
            <DialogTitle>Upload YAML</DialogTitle>
            <DialogDescription>
              Choose files or paste YAML with one or more documents separated by ---. You see the
              plan before anything changes.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="upload-files">Files</FieldLabel>
              <Input
                id="upload-files"
                type="file"
                multiple
                accept=".yaml,.yml,.json"
                onChange={read}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="upload-source">YAML</FieldLabel>
              <Textarea
                id="upload-source"
                className="min-h-48 font-mono text-xs"
                value={source}
                onChange={(event) => setSource(event.target.value)}
              />
            </Field>
            <FieldSet>
              <FieldLegend variant="label">What the upload does</FieldLegend>
              <RadioGroup value={mode} onValueChange={(value) => setMode(value as Mode)}>
                {modes.map((option) => (
                  <FieldLabel key={option.mode} htmlFor={`upload-mode-${option.mode}`}>
                    <Field orientation="horizontal">
                      <RadioGroupItem value={option.mode} id={`upload-mode-${option.mode}`} />
                      <FieldContent>
                        <FieldTitle>{option.title}</FieldTitle>
                        <FieldDescription>{option.description}</FieldDescription>
                      </FieldContent>
                    </Field>
                  </FieldLabel>
                ))}
              </RadioGroup>
            </FieldSet>
          </FieldGroup>
          {plan.isError && (
            <Alert variant="destructive">
              <AlertTitle>{plan.error.message}</AlertTitle>
              {problems.length > 0 && (
                <AlertDescription>
                  <ProblemList problems={problems} />
                </AlertDescription>
              )}
            </Alert>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={plan.isPending || source.trim() === ""}>
              Plan
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
