import { useEffect, useRef, useState } from "react";
import { backend } from "../api";
import type { Server, User } from "../api";
import { desk } from "./api";
import type { Action, Job, Plan, Result, Field } from "./api";
import {
  actionName,
  operationLabel,
  errorMessage,
  fieldName,
  messages,
  profileName,
} from "./i18n";
import type { Language } from "./i18n";
import { requestKey } from "./model";

export interface Selection {
  action: Action;
  values?: Record<string, string>;
  arguments?: string[];
}
export function ActionDialog({
  selection,
  language,
  servers,
  users,
  jobs,
  onClose,
  onStarted,
  onShare,
}: {
  selection: Selection;
  language: Language;
  servers: Server[];
  users: User[];
  jobs: Job[];
  onClose: () => void;
  onStarted: () => void;
  onShare?: (username: string) => void;
}) {
  const { action } = selection;
  const t = messages(language);
  const ref = useRef<HTMLDialogElement>(null);
  const [values, setValues] = useState<Record<string, string>>(
    selection.values ?? {},
  );
  const [args, setArgs] = useState<string[]>(selection.arguments ?? []);
  const [plan, setPlan] = useState<Plan | null>(null);
  const [jobId, setJobId] = useState("");
  const [result, setResult] = useState<Result | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [confirmation, setConfirmation] = useState("");
  const [copied, setCopied] = useState(false);
  const key = useRef(requestKey());
  const autoStarted = useRef(false);
  const job = jobs.find((job) => job.id === jobId);
  const selectedServer = servers.find(
    (server) =>
      server.name ===
      (values.server ||
        args[action.arguments.findIndex((field) => field.name === "server")]),
  );
  const trafficAction = ["user.probe", "observe.diagnose"].includes(action.id);
  const [eligible, setEligible] = useState<string[]>([]);
  useEffect(() => {
    let alive = true;
    setEligible([]);
    if (!trafficAction || !selectedServer) return;
    async function load() {
      let cursor = "";
      const choices: string[] = [];
      do {
        const page = await backend.checkUsers(selectedServer!.id, cursor);
        choices.push(...page.items);
        cursor = page.nextCursor ?? "";
      } while (cursor);
      if (alive) {
        setEligible(choices);
        setValues((old) =>
          choices.includes(old.username) ? old : { ...old, username: "" },
        );
      }
    }
    void load().catch(() => {});
    return () => {
      alive = false;
    };
  }, [trafficAction, selectedServer?.id]);
  const title = actionName(action.id, action.title, language);
  const terminal = job && !["queued", "running"].includes(job.state);
  const word = values.username || args[0] || values.name || title;
  useEffect(() => {
    ref.current?.showModal();
    return () => ref.current?.close();
  }, []);
  useEffect(() => {
    if (!terminal) return;
    let alive = true;
    desk
      .result(jobId)
      .then((value) => {
        if (alive) setResult(value);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [terminal, jobId]);
  useEffect(() => {
    const ready =
      action.arguments.every((_, index) =>
        Boolean(selection.arguments?.[index]),
      ) &&
      action.fields.every(
        (field) => !field.required || Boolean(selection.values?.[field.name]),
      );
    if (action.mutation || !ready || autoStarted.current) return;
    autoStarted.current = true;
    setBusy(true);
    desk
      .plan({
        action: action.id,
        values: selection.values ?? {},
        arguments: selection.arguments ?? [],
      })
      .then(async (value) => {
        setPlan(value);
        const started = await desk.start(value, key.current);
        setJobId(started.id);
        onStarted();
      })
      .catch(setError)
      .finally(() => setBusy(false));
  }, []);
  function set(name: string, value: string) {
    setValues((old) => ({ ...old, [name]: value }));
  }
  async function review(event?: React.FormEvent) {
    event?.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const input = { ...values };
      if (action.id === "server.cleanup") input.confirm = "CLEANUP";
      const created = await desk.plan({
        action: action.id,
        values: input,
        arguments: args,
      });
      setPlan(created);
      if (!action.mutation) {
        const started = await desk.start(created, key.current);
        setJobId(started.id);
        onStarted();
      }
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  async function apply() {
    if (!plan) return;
    setBusy(true);
    setError(null);
    try {
      const started = await desk.start(plan, key.current);
      setJobId(started.id);
      onStarted();
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  function input(field: Field, argumentIndex?: number) {
    const value =
      argumentIndex === undefined
        ? (values[field.name] ?? field.default ?? "")
        : (args[argumentIndex] ?? "");
    const change = (value: string) =>
      argumentIndex === undefined
        ? set(field.name, value)
        : setArgs((old) => {
            const next = [...old];
            next[argumentIndex] = value;
            return next;
          });
    const label = fieldName(field.name, language);
    if (["quota-bytes", "monthly-bytes"].includes(field.name))
      return (
        <label key={field.name}>
          {label} ({t.gb})
          <input
            type="number"
            min="1"
            step="1"
            placeholder={t.inherited}
            value={Number(value) > 0 ? String(Number(value) / 1073741824) : ""}
            onChange={(event) =>
              change(
                event.target.value
                  ? String(Math.round(Number(event.target.value) * 1073741824))
                  : "0",
              )
            }
          />
          <small>
            {t.quotaWindow} · {t.inherited}
          </small>
        </label>
      );
    if (
      field.name === "server" ||
      field.name === "proxy" ||
      field.name === "backend"
    )
      return (
        <label key={field.name}>
          {label}
          <select
            required={field.required}
            value={value}
            onChange={(event) => change(event.target.value)}
          >
            <option value="">{t.choose}</option>
            {servers.map((server) => (
              <option key={server.id} value={server.name}>
                {server.name}
              </option>
            ))}
          </select>
        </label>
      );
    if (field.name === "username" && action.id !== "user.add")
      return (
        <label key={field.name}>
          {label}
          <select
            value={value}
            required={field.required}
            onChange={(event) => change(event.target.value)}
          >
            <option value="">{t.choose}</option>
            {users
              .filter((user) =>
                trafficAction
                  ? eligible.includes(user.username)
                  : !selectedServer ||
                    user.nodeIds?.includes(selectedServer.id),
              )
              .map((user) => (
                <option key={user.username}>{user.username}</option>
              ))}
          </select>
        </label>
      );
    if (field.name === "engine") {
      const options =
        action.id === "observe.diagnose"
          ? ["knife-xray", "knife-singbox"]
          : ["native", "checker"];
      return (
        <label key={field.name}>
          {label}
          <select value={value} onChange={(e) => change(e.target.value)}>
            {options.map((option) => (
              <option key={option}>{option}</option>
            ))}
          </select>
        </label>
      );
    }
    if (field.type === "bool")
      return (
        <label key={field.name} className="desk-check">
          <input
            type="checkbox"
            checked={value === "true"}
            onChange={(event) => change(String(event.target.checked))}
          />
          {label}
        </label>
      );
    if (field.name === "profile") {
      const profiles = action.id.startsWith("server.profile.")
        ? (field.options ?? [])
        : (selectedServer?.profiles ?? [
            ...new Set(servers.flatMap((server) => server.profiles)),
          ]);
      return (
        <label key={field.name}>
          {label}
          <select
            required={field.required}
            value={value}
            onChange={(event) => change(event.target.value)}
          >
            <option value="">{t.choose}</option>
            {profiles.map((profile) => (
              <option key={profile} value={profile}>
                {profileName(profile)}
              </option>
            ))}
          </select>
        </label>
      );
    }
    const type = field.secret
      ? "password"
      : ["expiry", "date"].includes(field.name)
        ? "date"
        : /^(int|uint|float)/.test(field.type)
          ? "number"
          : "text";
    return (
      <label key={field.name}>
        {label}
        <input
          type={type}
          value={value}
          required={field.required}
          maxLength={16384}
          autoComplete="off"
          onChange={(event) => change(event.target.value)}
        />
      </label>
    );
  }
  const order = [
    "username",
    "name",
    "server",
    "profile",
    "quota-bytes",
    "monthly-gb",
    "monthly-bytes",
    "expiry",
    "date",
  ];
  const basic = action.fields
    .filter((field) => !field.advanced)
    .sort(
      (a, b) =>
        (order.includes(a.name) ? order.indexOf(a.name) : 99) -
        (order.includes(b.name) ? order.indexOf(b.name) : 99),
    );
  const advanced = action.fields.filter(
    (field) => field.advanced && field.name !== "confirm",
  );
  return (
    <dialog
      ref={ref}
      className="desk-dialog"
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onClose();
      }}
    >
      <header>
        <div>
          <small>{t.action}</small>
          <h2>{title}</h2>
        </div>
        <button
          className="icon-button"
          aria-label={t.close}
          disabled={busy}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      {error !== null && (
        <div className="desk-alert danger" role="alert">
          <p>{errorMessage(error, language)}</p>
          <details>
            <summary>{t.details}</summary>
            <code>
              {error instanceof Error ? error.message : "OPERATION_UNAVAILABLE"}
            </code>
          </details>
        </div>
      )}
      {!plan && !autoStarted.current && (
        <form onSubmit={(event) => void review(event)}>
          <div className="desk-form">
            {action.arguments.map((field, index) => input(field, index))}
            {basic.map((field) => input(field))}
            {advanced.length > 0 && (
              <details>
                <summary>{t.advanced}</summary>
                <div className="desk-form advanced-fields">
                  {advanced.map((field) => input(field))}
                </div>
              </details>
            )}
          </div>
          <footer>
            <button type="button" className="button" onClick={onClose}>
              {t.cancel}
            </button>
            <button type="submit" className="button primary" disabled={busy}>
              {busy ? t.preparing : action.mutation ? t.review : t.continue}
            </button>
          </footer>
        </form>
      )}
      {plan && !jobId && !autoStarted.current && (
        <>
          <div className="desk-form">
            <p>{t.reviewHint}</p>
            {action.id === "user.add" && !Number(values["quota-bytes"]) && (
              <p>
                {t.limit}: {t.inherited}
              </p>
            )}
            <dl className="desk-review-values">
              {action.arguments.map((field, i) => (
                <div key={field.name}>
                  <dt>{fieldName(field.name, language)}</dt>
                  <dd>{args[i]}</dd>
                </div>
              ))}
              {action.fields
                .filter(
                  (field) =>
                    !field.secret &&
                    values[field.name] !== undefined &&
                    field.name !== "confirm",
                )
                .map((field) => (
                  <div key={field.name}>
                    <dt>{fieldName(field.name, language)}</dt>
                    <dd>
                      {["quota-bytes", "monthly-bytes"].includes(field.name)
                        ? `${Number(values[field.name]) / 1073741824} ${t.gb}`
                        : values[field.name]}
                    </dd>
                  </div>
                ))}
            </dl>
            <h3>{t.affected}</h3>
            <ul className="desk-targets">
              {plan.targets.map((target) => (
                <li key={target.id}>
                  <span className="status-dot neutral" />
                  {target.name}
                </li>
              ))}
            </ul>
            {action.destructive && (
              <div className="desk-alert danger">
                <p>{t.destructive}</p>
                <label>
                  {t.typeConfirm} <strong>{word}</strong>
                  <input
                    value={confirmation}
                    onChange={(event) => setConfirmation(event.target.value)}
                    autoComplete="off"
                  />
                </label>
              </div>
            )}
          </div>
          <footer>
            <button
              className="button"
              disabled={busy}
              onClick={() => setPlan(null)}
            >
              {t.back}
            </button>
            <button
              className={`button ${action.destructive ? "destructive" : "primary"}`}
              disabled={busy || (action.destructive && confirmation !== word)}
              onClick={() => void apply()}
            >
              {busy ? t.preparing : t.confirm}
            </button>
          </footer>
        </>
      )}
      {autoStarted.current && !jobId && (
        <div className="desk-form" role="status">
          {t.preparing}
        </div>
      )}
      {jobId && (
        <>
          <div className="desk-form" aria-live="polite">
            <h3>
              {terminal
                ? job?.state === "succeeded"
                  ? t.succeeded
                  : t.operationPartial
                : t.running}
            </h3>
            {terminal && job?.code && (
              <div className="desk-alert danger">
                <p>{errorMessage(new Error(job.code), language)}</p>
                <details>
                  <summary>{t.details}</summary>
                  <code>{job.code}</code>
                </details>
              </div>
            )}
            <ul className="desk-targets">
              {job?.steps.map((step) => (
                <li key={step.target.id}>
                  <span
                    className={`status-dot ${step.state === "succeeded" ? "ok" : step.code ? "danger" : "warn"}`}
                  />
                  <strong>{step.target.name}</strong>
                  <span>{operationLabel(step.state, language)}</span>
                  {step.code && (
                    <details>
                      <summary>{t.details}</summary>
                      <code>{step.code}</code>
                    </details>
                  )}
                </li>
              ))}
            </ul>
            {result?.qrData && (
              <img className="desk-qr" src={result.qrData} alt={t.qr} />
            )}
            {result?.content && (
              <>
                <p>{t.sensitive}</p>
                <textarea
                  readOnly
                  value={result.content}
                  aria-label={t.share}
                />
                <button
                  className="button"
                  onClick={() => {
                    navigator.clipboard
                      .writeText(result.content!)
                      .then(() => setCopied(true))
                      .catch(() => {});
                  }}
                >
                  {copied ? t.copied : t.copy}
                </button>
              </>
            )}
            {result?.text && (
              <details open={!action.mutation}>
                <summary>{t.output}</summary>
                <pre>{result.text}</pre>
              </details>
            )}
          </div>
          <footer>
            {!terminal && (
              <button
                className="button"
                onClick={() => void desk.cancel(jobId).then(onStarted)}
              >
                {t.cancel}
              </button>
            )}
            {terminal &&
              job?.state === "succeeded" &&
              action.id === "user.add" && (
                <button
                  className="button primary"
                  onClick={() => onShare?.(values.username)}
                >
                  {t.share}
                </button>
              )}
            {terminal && action.id === "user.link" && (
              <button
                className="button"
                onClick={() => {
                  autoStarted.current = false;
                  setPlan(null);
                  setJobId("");
                  setResult(null);
                  key.current = requestKey();
                }}
              >
                {t.profile}
              </button>
            )}
            {terminal && job?.state !== "succeeded" && (
              <button
                className="button"
                onClick={() => {
                  if (job?.code === "STALE_PLAN") {
                    autoStarted.current = false;
                    setJobId("");
                    setPlan(null);
                    setResult(null);
                    key.current = requestKey();
                    return;
                  }
                  void desk
                    .resume(jobId)
                    .then(() => {
                      onStarted();
                      setResult(null);
                    })
                    .catch(setError);
                }}
              >
                {job?.code === "STALE_PLAN" ? t.review : t.resume}
              </button>
            )}
            <button className="button primary" onClick={onClose}>
              {t.close}
            </button>
          </footer>
        </>
      )}
    </dialog>
  );
}
