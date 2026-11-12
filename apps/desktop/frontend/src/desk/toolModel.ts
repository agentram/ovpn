import type { Language } from "./i18n";
import type { ObservationJob } from "../api";
import { uiCopy } from "./uiCopy.ts";
export function toolName(engine: string, language: Language): string {
  return (
    {
      native: uiCopy(language).builtIn,
      checker: "Xray Checker",
      knife: "Xray Knife",
      "knife-singbox": "Knife · sing-box",
      "knife-xray": uiCopy(language).knifeCore,
    }[engine] ?? engine
  );
}
export function toolJobLabel(job: ObservationJob, language: Language): string {
  const t = uiCopy(language);
  return (
    {
      install: t.install,
      diagnose: t.diagnose,
      diagnostic: t.diagnose,
      once: t.check,
      check: t.check,
    }[job.kind] ?? t.check
  );
}
export function toolJobState(state: string, language: Language): string {
  const t = uiCopy(language);
  return (
    {
      completed: t.completed,
      succeeded: t.completed,
      running: t.running,
      queued: t.queued,
      failed: t.failed,
      cancelled: t.cancelled,
      interrupted: t.interrupted,
    }[state] ?? t.interrupted
  );
}
