import { useCallback, useEffect, useRef, useState } from "react";
import { backend, observerBackend, emptyObserver } from "../api";
import type { Server, User } from "../api";
import { desk, emptySnapshot } from "./api";
import type { Action, Job, Snapshot } from "./api";
import { ServerWizard } from "./ServerWizard";
import { ActionBrowser } from "./ActionBrowser";
import { uiCopy } from "./uiCopy";
import { toolName } from "./toolModel";
import { ActionDialog } from "./ActionDialog";
import type { Selection } from "./ActionDialog";
import { Activity, Detail, Overview, Servers, Users } from "./components";
import { actionName, errorMessage, messages } from "./i18n";
import type { Language } from "./i18n";

type View = "overview" | "users" | "servers" | "activity";
function preference(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}
export function App() {
  const [language, setLanguage] = useState<Language>(() => {
    const value = preference("ovpn-desk.language", "en");
    return value === "ru" || value === "zh" ? value : "en";
  });
  const [theme, setTheme] = useState(() =>
    preference("ovpn-desk.theme", "dark") === "light" ? "light" : "dark",
  );
  const [view, setView] = useState<View>("overview");
  const [serverWizard, setServerWizard] = useState(false);
  const [settings, setSettings] = useState(false);
  const [servers, setServers] = useState<Server[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [catalog, setCatalog] = useState<Action[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [observation, setObservation] = useState(emptyObserver);
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [detail, setDetail] = useState<{ user?: User; server?: Server } | null>(
    null,
  );
  const [version, setVersion] = useState("");
  const mounted = useRef(true);
  const refreshRunning = useRef(false);
  const settingsDialog = useRef<HTMLDialogElement>(null);
  const t = messages(language);
  useEffect(() => {
    document.documentElement.lang = language;
    try {
      localStorage.setItem("ovpn-desk.language", language);
    } catch {}
  }, [language]);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("ovpn-desk.theme", theme);
    } catch {}
  }, [theme]);
  useEffect(() => {
    if (settings) settingsDialog.current?.showModal();
    else settingsDialog.current?.close();
  }, [settings]);
  const refresh = useCallback(async () => {
    if (refreshRunning.current) return;
    refreshRunning.current = true;
    try {
      let cursor = "";
      const nodes: Server[] = [];
      do {
        const page = await backend.servers(cursor);
        nodes.push(...page.items);
        cursor = page.nextCursor ?? "";
      } while (cursor);
      const people: User[] = [];
      cursor = "";
      do {
        const page = await backend.users(cursor);
        people.push(...page.items);
        cursor = page.nextCursor ?? "";
      } while (cursor);
      if (mounted.current) {
        setServers(nodes);
        setUsers(people);
        setError(null);
      }
    } catch (error) {
      if (mounted.current) setError(error);
    } finally {
      refreshRunning.current = false;
      if (mounted.current) setBusy(false);
    }
  }, []);
  const refreshJobs = useCallback(async () => {
    try {
      const next = await desk.jobs();
      if (mounted.current) setJobs(next);
    } catch {}
  }, []);
  useEffect(() => {
    mounted.current = true;
    backend
      .hello()
      .then((value) => {
        if (mounted.current) setVersion(value.engineVersion);
      })
      .catch(setError);
    desk
      .catalog()
      .then((value) => {
        if (mounted.current) setCatalog(value);
      })
      .catch(setError);
    void refresh();
    const inventory = setInterval(() => void refresh(), 30000);
    const changes = setInterval(() => void refreshJobs(), 2000);
    let polling = false;
    async function poll() {
      if (polling) return;
      polling = true;
      try {
        const [next, history] = await Promise.all([
          desk.snapshot(),
          observerBackend.status(),
        ]);
        if (mounted.current) setObservation(history);
        if (mounted.current) setSnapshot(next);
      } catch (error) {
        if (mounted.current) {
          setSnapshot((old) => ({ ...old, running: false }));
          setError(error);
        }
      } finally {
        polling = false;
      }
    }
    void poll();
    void refreshJobs();
    const readings = setInterval(() => void poll(), 5000);
    function wake() {
      if (document.visibilityState === "visible") {
        void poll();
        void refresh();
      }
    }
    document.addEventListener("visibilitychange", wake);
    return () => {
      mounted.current = false;
      clearInterval(inventory);
      clearInterval(changes);
      clearInterval(readings);
      document.removeEventListener("visibilitychange", wake);
    };
  }, [refresh, refreshJobs]);
  const completed = jobs.map((job) => `${job.id}/${job.state}`).join("|");
  useEffect(() => {
    void refresh();
  }, [completed, refresh]);
  function choose(
    id: string,
    values: Record<string, string> = {},
    args: string[] = [],
  ) {
    if (id === "server.add") {
      setServerWizard(true);
      return;
    }
    const action = catalog.find((action) => action.id === id);
    if (!action) return;
    setDetail(null);
    setSelection({ action, values, arguments: args });
  }
  async function configure(enabled: boolean) {
    try {
      setSnapshot(await desk.configure(enabled));
    } catch (error) {
      setError(error);
    }
  }
  function onAction(value: Selection) {
    setDetail(null);
    setSettings(false);
    setSelection(value);
  }
  const title = view === "overview" ? t.welcome : t[view];
  return (
    <div className="desk-shell">
      <aside className="desk-sidebar">
        <button
          className="desk-brand"
          onClick={() => setView("overview")}
          aria-label="OVPN Desk"
        >
          <img src="/brand/ovpn.svg" alt="OVPN" />
          <span>
            OVPN<small>DESK</small>
          </span>
        </button>
        <nav aria-label="Workspace">
          {(["overview", "users", "servers", "activity"] as View[]).map(
            (item) => (
              <button
                key={item}
                aria-current={view === item ? "page" : undefined}
                onClick={() => {
                  setView(item);
                  setSettings(false);
                }}
              >
                <span aria-hidden>
                  {
                    { overview: "◫", users: "♙", servers: "▤", activity: "⌁" }[
                      item
                    ]
                  }
                </span>
                {t[item]}
              </button>
            ),
          )}
        </nav>
        <div className="desk-sidebar-bottom">
          <span className="desk-local-dot" />
          {t.current}
          <button onClick={() => setSettings(true)}>⚙ {t.settings}</button>
          <small>OVPN {version}</small>
        </div>
      </aside>
      <div className="desk-body">
        <header className="desk-topbar">
          <span>
            OVPN Desk <span>/</span> {t[view]}
          </span>
          <button
            className="button"
            disabled={busy}
            onClick={() => void refresh()}
          >
            {t.refresh}
          </button>
        </header>
        <main className="desk-main">
          <div className="desk-heading">
            <div>
              <p className="eyebrow">OVPN</p>
              <h1>{title}</h1>
              {view === "overview" && <p>{t.intro}</p>}
            </div>
            <div className="desk-heading-actions">
              {view === "servers" || servers.length === 0 ? (
                <button
                  className="button primary"
                  disabled={!catalog.length}
                  onClick={() =>
                    choose("server.add", {
                      "ssh-user": "root",
                      "ssh-identity": "",
                    })
                  }
                >
                  ＋ {t.addServer}
                </button>
              ) : (
                view !== "activity" && (
                  <button
                    className="button primary"
                    disabled={!catalog.length || !servers.length}
                    onClick={() => choose("user.add")}
                  >
                    ＋ {t.addUser}
                  </button>
                )
              )}
            </div>
          </div>
          {error !== null && (
            <div className="desk-alert danger" role="alert">
              <p>{errorMessage(error, language)}</p>
              <details>
                <summary>{t.details}</summary>
                <code>
                  {error instanceof Error
                    ? error.message
                    : "WORKSPACE_UNAVAILABLE"}
                </code>
              </details>
            </div>
          )}
          <section
            className={`desk-monitor-banner ${snapshot.enabled ? "enabled" : ""}`}
          >
            <div>
              <span
                className={`status-dot ${snapshot.running && snapshot.enabled ? "ok" : "neutral"}`}
              />
              <strong>
                {snapshot.enabled && snapshot.running
                  ? t.autoRunning
                  : t.autoPaused}
              </strong>
              <small>
                {snapshot.enabled
                  ? `${t.coverage} ${snapshot.checked} / ${snapshot.total}`
                  : t.autoHint}
              </small>
            </div>
            <button
              className="button"
              onClick={() => void configure(!snapshot.enabled)}
            >
              {snapshot.enabled ? t.pauseAuto : t.enableAuto}
            </button>
          </section>
          {busy ? (
            <p role="status">{t.preparing}</p>
          ) : view === "overview" ? (
            <Overview
              language={language}
              servers={servers}
              users={users}
              snapshot={snapshot}
              jobs={jobs}
              onView={setView}
              onUser={(user) => setDetail({ user })}
              onServer={(server) => setDetail({ server })}
            />
          ) : view === "users" ? (
            <Users
              language={language}
              users={users}
              servers={servers}
              snapshot={snapshot}
              onOpen={(user) => setDetail({ user })}
              onAdd={() => choose("user.add")}
            />
          ) : view === "servers" ? (
            <Servers
              language={language}
              servers={servers}
              snapshot={snapshot}
              onOpen={(server) => setDetail({ server })}
              onAdd={() =>
                choose("server.add", { "ssh-user": "root", "ssh-identity": "" })
              }
            />
          ) : (
            <Activity
              language={language}
              jobs={jobs}
              history={observation.history}
              nodes={servers}
              toolJobs={observation.jobs}
              onRefresh={() => void refreshJobs()}
            />
          )}
        </main>
        <footer className="desk-footer">
          <span>{snapshot.enabled ? t.checkInfo : t.autoPaused}</span>
          <span>
            {snapshot.active
              ? `${snapshot.active} ${t.checking}`
              : `OVPN ${version}`}
          </span>
        </footer>
      </div>
      {serverWizard && (
        <ServerWizard
          servers={servers}
          language={language}
          onClose={() => {
            setServerWizard(false);
            void refresh();
          }}
          onChanged={() => {
            void refresh();
            void refreshJobs();
          }}
        />
      )}
      {selection && (
        <ActionDialog
          key={selection.action.id + JSON.stringify(selection.values)}
          selection={selection}
          language={language}
          servers={servers}
          users={users}
          jobs={jobs}
          onClose={() => {
            setSelection(null);
            void refresh();
          }}
          onShare={(username) =>
            choose("user.link", {
              username,
              server: servers.find((node) => node.enabled)?.name ?? "",
            })
          }
          onStarted={() => void refreshJobs()}
        />
      )}
      {detail && (
        <Detail
          language={language}
          {...detail}
          servers={servers}
          snapshot={snapshot}
          catalog={catalog}
          onAction={onAction}
          onClose={() => setDetail(null)}
        />
      )}
      <dialog
        ref={settingsDialog}
        className="desk-dialog"
        onCancel={() => setSettings(false)}
      >
        <header>
          <h2>{t.settings}</h2>
          <button
            className="icon-button"
            aria-label={t.close}
            onClick={() => setSettings(false)}
          >
            ×
          </button>
        </header>
        <div className="desk-form">
          <label>
            {t.language}
            <select
              value={language}
              onChange={(event) => setLanguage(event.target.value as Language)}
            >
              <option value="en">English</option>
              <option value="ru">Русский</option>
              <option value="zh">简体中文</option>
            </select>
          </label>
          <label>
            {t.appearance}
            <select
              value={theme}
              onChange={(event) => setTheme(event.target.value)}
            >
              <option value="dark">{t.dark}</option>
              <option value="light">{t.light}</option>
            </select>
          </label>
          <section>
            <h3>{t.tools}</h3>
            <p>{t.toolHint}</p>
            <div className="desk-tool-status">
              {observation.tools.map((tool) => (
                <p key={tool.engine}>
                  <span
                    className={`status-dot ${tool.configured ? "ok" : "neutral"}`}
                  />
                  <strong>{toolName(tool.engine, language)}</strong> ·{" "}
                  {tool.version} ·{" "}
                  {tool.configured
                    ? uiCopy(language).installed
                    : uiCopy(language).notInstalled}
                </p>
              ))}
              {observation.jobs
                .filter(
                  (job) => job.kind === "install" && job.state === "running",
                )
                .map((job) => (
                  <p key={job.id} role="status">
                    {t.preparing} {job.engine}
                  </p>
                ))}
            </div>
            <div className="desk-detail-actions">
              <button
                className="button"
                onClick={() =>
                  void observerBackend.install("checker").catch(setError)
                }
              >
                {t.installChecker}
              </button>
              <button
                className="button"
                onClick={() =>
                  void observerBackend.install("knife").catch(setError)
                }
              >
                {t.installKnife}
              </button>
            </div>
          </section>
          <details>
            <summary>{t.advanced}</summary>
            <p>{t.advancedHint}</p>
            <ActionBrowser
              actions={catalog}
              language={language}
              onChoose={(action) => onAction({ action })}
            />
          </details>
          <small>
            {t.about} · {version}
          </small>
        </div>
        <footer>
          <button className="button primary" onClick={() => setSettings(false)}>
            {t.close}
          </button>
        </footer>
      </dialog>
    </div>
  );
}
