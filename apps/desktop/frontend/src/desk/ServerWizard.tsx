import { useEffect, useRef, useState } from "react";
import type { Server } from "../api";
import { uiCopy } from "./uiCopy";
import { desk } from "./api";
import type { ActionRequest, Job, Result } from "./api";
import { errorMessage, messages } from "./i18n";
import type { Language } from "./i18n";
import { requestKey } from "./model";

// The wizard sends the same structured plans used by individual management actions.
export function ServerWizard({
  language,
  servers,
  onClose,
  onChanged,
}: {
  language: Language;
  servers: Server[];
  onClose: () => void;
  onChanged: () => void;
}) {
  const t = messages(language);
  const ui = uiCopy(language);
  const [role, setRole] = useState("vpn");
  const [preset, setPreset] = useState("ru");
  const [backends, setBackends] = useState<string[]>([]);
  const eligibleBackends = servers.filter(
    (server) =>
      server.role === "vpn" &&
      server.enabled &&
      server.profiles.includes("vless-reality-tcp-vision"),
  );
  const ref = useRef<HTMLDialogElement>(null);
  const alive = useRef(true);
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [domain, setDomain] = useState("");
  const [user, setUser] = useState("root");
  const [port, setPort] = useState("22");
  const [identity, setIdentity] = useState("");
  const [keys, setKeys] = useState<{ fingerprint: string; line: string }[]>([]);
  const [fingerprint, setFingerprint] = useState("");
  const [job, setJob] = useState<Job | null>(null);
  useEffect(() => {
    ref.current?.showModal();
    return () => {
      alive.current = false;
      ref.current?.close();
    };
  }, []);
  async function run(input: ActionRequest): Promise<Result> {
    const plan = await desk.plan(input);
    const started = await desk.start(plan, requestKey());
    if (alive.current) setJob(started);
    onChanged();
    const deadline = Date.now() + 20 * 60 * 1000;
    while (alive.current && Date.now() < deadline) {
      const rows = await desk.jobs();
      const current = rows.find((row) => row.id === started.id);
      if (current && alive.current) setJob(current);
      if (current && !["queued", "running"].includes(current.state)) {
        onChanged();
        if (current.state !== "succeeded")
          throw new Error(
            current.steps.find((s) => s.code)?.code ||
              current.code ||
              "APPLY_UNVERIFIED",
          );
        return desk.result(current.id);
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    throw new Error("INTERRUPTED");
  }
  async function execute(work: () => Promise<void>) {
    setBusy(true);
    setError(null);
    try {
      await work();
    } catch (e) {
      if (alive.current) setError(e);
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  async function inspect() {
    await execute(async () => {
      const result = await run({
        action: "server.inspect",
        values: { host, "ssh-port": port },
        arguments: [],
      });
      const found = result.data as { fingerprint: string; line: string }[];
      if (!Array.isArray(found) || !found.length)
        throw new Error("SSH_HOST_UNREACHABLE");
      setKeys(found);
      setFingerprint(found[0].fingerprint);
      setStep(1);
    });
  }
  async function register() {
    await execute(async () => {
      await run({
        action: "server.add",
        values: {
          name,
          role,
          "proxy-preset": role === "proxy" ? preset : "",
          host,
          domain,
          "ssh-user": user,
          "ssh-port": port,
          "ssh-identity": identity,
          "trust-fingerprint": fingerprint,
        },
        arguments: [],
      });
      setStep(2);
      await run({ action: "server.test", values: {}, arguments: [name] });
    });
  }
  async function install() {
    await execute(async () => {
      if (role === "proxy") {
        for (const backend of backends)
          await run({
            action: "server.backend.attach",
            values: { proxy: name, backend },
            arguments: [],
          });
        await run({ action: "server.ha-apply", values: {}, arguments: [name] });
      } else
        await run({ action: "server.init", values: {}, arguments: [name] });
      setStep(3);
    });
  }
  return (
    <dialog
      ref={ref}
      className="desk-dialog"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <header>
        <div>
          <small>
            {t.serverSetup} · {step + 1} / 4
          </small>
          <h2>{t.addServer}</h2>
        </div>
        <button className="icon-button" aria-label={t.close} onClick={onClose}>
          ×
        </button>
      </header>
      {error !== null && (
        <div className="desk-alert danger" role="alert">
          <p>{errorMessage(error, language)}</p>
          <details>
            <summary>{t.details}</summary>
            <code>
              {error instanceof Error ? error.message : "SSH_ACCESS_FAILED"}
            </code>
          </details>
        </div>
      )}
      <div className="desk-form">
        {step === 0 && (
          <form
            id="server-address"
            onSubmit={(event) => {
              event.preventDefault();
              void inspect();
            }}
            className="desk-form"
          >
            <p>{t.serverAddressHint}</p>
            <label>
              {ui.serverPurpose}
              <select value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="vpn">{ui.standardServer}</option>
                <option value="proxy">{ui.proxyServer}</option>
              </select>
            </label>
            {role === "proxy" && (
              <>
                <label>
                  {ui.routingPreset}
                  <select
                    value={preset}
                    onChange={(e) => setPreset(e.target.value)}
                  >
                    <option value="ru">RU</option>
                    <option value="cn">CN</option>
                  </select>
                </label>
                <fieldset className="desk-backend-choices">
                  <legend>{ui.backendChoice}</legend>
                  <p>{ui.backendChoiceHint}</p>
                  {eligibleBackends.map((server) => (
                    <label key={server.id}>
                      <input
                        type="checkbox"
                        checked={backends.includes(server.name)}
                        onChange={(e) =>
                          setBackends((old) =>
                            e.target.checked
                              ? [...old, server.name]
                              : old.filter((name) => name !== server.name),
                          )
                        }
                      />
                      {server.name}
                    </label>
                  ))}
                  {!eligibleBackends.length && <p>{ui.noBackends}</p>}
                </fieldset>
                <p>{ui.proxyWarning}</p>
              </>
            )}
            <label>
              {t.name}
              <input
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoComplete="off"
              />
            </label>
            <label>
              {t.address}
              <input
                required
                value={host}
                onChange={(e) => setHost(e.target.value)}
                autoComplete="off"
                placeholder="server.example.com"
              />
            </label>
            <label>
              {t.domain}
              <input
                required
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                autoComplete="off"
                placeholder="connect.example.com"
              />
            </label>
            <label>
              {t.sshUser}
              <input
                required
                value={user}
                onChange={(e) => setUser(e.target.value)}
                autoComplete="off"
              />
            </label>
            <label>
              {t.sshIdentity}
              <input
                value={identity}
                onChange={(e) => setIdentity(e.target.value)}
                autoComplete="off"
                placeholder={t.sshAgentHint}
              />
            </label>
            <details>
              <summary>{t.advanced}</summary>
              <label>
                {t.port}
                <input
                  type="number"
                  min="1"
                  max="65535"
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                />
              </label>
            </details>
          </form>
        )}
        {step === 1 && (
          <>
            <h3>
              {name} · {host}
            </h3>
            <p>{t.hostTrustHint}</p>
            <label>
              {t.fingerprint}
              <select
                value={fingerprint}
                onChange={(e) => setFingerprint(e.target.value)}
              >
                {keys.map((k) => (
                  <option key={k.fingerprint}>{k.fingerprint}</option>
                ))}
              </select>
            </label>
            <code>{fingerprint}</code>
            <p>{t.serverRegistrationHint}</p>
          </>
        )}
        {step === 2 && (
          <>
            <h3>{name}</h3>
            <p>{t.installHint}</p>
            {role === "proxy" && (
              <>
                <p>{ui.haApplyHint}</p>
                <ul className="desk-targets">
                  {backends.map((name) => (
                    <li key={name}>{name}</li>
                  ))}
                  <li>
                    {name} · {ui.entry}
                  </li>
                </ul>
                <p>{ui.proxyWarning}</p>
              </>
            )}
            <ul className="desk-targets">
              <li>{host}</li>
              <li>{domain}</li>
            </ul>
          </>
        )}
        {step === 3 && (
          <div className="desk-success">
            <span className="status-dot ok" />
            <h3>{t.serverReady}</h3>
            <p>{t.serverReadyHint}</p>
          </div>
        )}
        {busy && (
          <div role="status" aria-live="polite">
            <progress />
            <p>{t.running}</p>
            {job?.steps.map((s) => (
              <p key={s.target.id}>
                {s.target.name} ·{" "}
                {(t as Record<string, string>)[s.state] ?? t.running}
              </p>
            ))}
          </div>
        )}
      </div>
      <footer>
        <button className="button" onClick={onClose}>
          {busy ? t.activity : t.cancel}
        </button>
        {step === 0 && (
          <button
            type="submit"
            form="server-address"
            className="button primary"
            disabled={busy || (role === "proxy" && !backends.length)}
          >
            {t.continue}
          </button>
        )}
        {step === 1 && (
          <button
            className="button primary"
            disabled={busy}
            onClick={() => void register()}
          >
            {t.trustContinue}
          </button>
        )}
        {step === 2 && (
          <button
            className="button primary"
            disabled={busy || error !== null}
            onClick={() => void install()}
          >
            {t.installServer}
          </button>
        )}
        {step === 2 && error !== null && (
          <button
            className="button"
            disabled={busy}
            onClick={() =>
              void execute(async () => {
                await run({
                  action: "server.test",
                  values: {},
                  arguments: [name],
                });
              })
            }
          >
            {t.retry}
          </button>
        )}
        {step === 3 && (
          <button className="button primary" onClick={onClose}>
            {t.done}
          </button>
        )}
      </footer>
    </dialog>
  );
}
