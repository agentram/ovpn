import { useEffect, useRef, useState } from "react";
import type { Server, User, Observation, ObservationJob } from "../api";
import { desk } from "./api";
import type { Action, Job, Route, Snapshot, UserLive } from "./api";
import type { Language } from "./i18n";
import { actionName, messages, profileName, operationLabel } from "./i18n";
import {
  accessState,
  bytes,
  closestQuota,
  connectedState,
  isRecent,
  timestamp,
} from "./model";
import type { Selection } from "./ActionDialog";

export function Signal({
  text,
  tone = "neutral",
}: {
  text: string;
  tone?: string;
}) {
  return (
    <span className={`desk-signal ${tone}`}>
      <span className={`status-dot ${tone}`} />
      {text}
    </span>
  );
}
export function Empty({
  title,
  body,
  action,
  onAction,
}: {
  title: string;
  body: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <section className="panel desk-empty">
      <span className="desk-empty-ring" />
      <h2>{title}</h2>
      <p>{body}</p>
      {action && (
        <button className="button primary" onClick={onAction}>
          {action}
        </button>
      )}
    </section>
  );
}
export function Overview({
  language,
  snapshot,
  servers,
  users,
  jobs,
  onView,
  onUser,
  onServer,
}: {
  language: Language;
  snapshot: Snapshot;
  servers: Server[];
  users: User[];
  jobs: Job[];
  onView: (view: "users" | "servers" | "activity") => void;
  onUser: (user: User) => void;
  onServer: (server: Server) => void;
}) {
  const t = messages(language);
  const connected = users.filter(
    (user) =>
      connectedState(
        snapshot.servers
          .flatMap((server) => server.users)
          .filter((row) => row.username === user.username),
        snapshot.running,
      ) === true,
  );
  const knownPresence = users.filter(
    (user) =>
      connectedState(
        snapshot.servers
          .flatMap((server) => server.users)
          .filter((row) => row.username === user.username),
        snapshot.running,
      ) !== null,
  ).length;
  const readings = snapshot.servers.flatMap((server) => server.users);
  const allTrafficKnown =
    readings.length > 0 &&
    readings.length ===
      users.reduce((sum, user) => sum + user.configuredNodes, 0) &&
    readings.every((row) => row.usedBytes !== null && isRecent(row.trafficAt));
  const traffic = allTrafficKnown
    ? readings.reduce((sum, row) => sum + row.usedBytes!, 0)
    : null;
  const problems = users.filter((user) =>
    ["partial", "failed"].includes(
      accessState(
        user,
        snapshot.servers
          .flatMap((server) => server.users)
          .filter((row) => row.username === user.username),
        snapshot.running,
      ).key,
    ),
  );
  return (
    <>
      <div className="desk-metrics">
        <button onClick={() => onView("servers")}>
          <span>{t.servers}</span>
          <strong>{servers.length}</strong>
          <small>{t.current}</small>
        </button>
        <button onClick={() => onView("users")}>
          <span>{t.traffic}</span>
          <strong>{bytes(traffic)}</strong>
          <small>{t.quotaWindow}</small>
        </button>
        <button onClick={() => onView("users")}>
          <span>{t.activeUsers}</span>
          <strong>
            {snapshot.running &&
            (knownPresence === users.length || connected.length > 0)
              ? `${connected.length}${knownPresence < users.length ? "+" : ""}`
              : "—"}
          </strong>
          <small>
            {knownPresence < users.length ? t.unknown : t.connected}
          </small>
        </button>
        <button onClick={() => onView("users")}>
          <span>{t.problems}</span>
          <strong className={problems.length ? "warning-value" : ""}>
            {snapshot.running && snapshot.checked > 0 ? problems.length : "—"}
          </strong>
          <small>{t.checks}</small>
        </button>
      </div>
      <div className="desk-two-columns">
        <section className="panel">
          <div className="panel-title">
            <h2>{t.servers}</h2>
            <button className="text-button" onClick={() => onView("servers")}>
              {t.details} →
            </button>
          </div>
          <div className="desk-list">
            {servers.map((server) => {
              const live = snapshot.servers.find(
                (row) => row.serverId === server.id,
              );
              const state = !isRecent(live?.checkedAt)
                ? "neutral"
                : live?.state === "online"
                  ? "ok"
                  : ["failed", "unreachable"].includes(live?.state ?? "")
                    ? "danger"
                    : "neutral";
              return (
                <button key={server.id} onClick={() => onServer(server)}>
                  <span className={`status-dot ${state}`} />
                  <span>
                    <strong>{server.name}</strong>
                    <small>
                      {server.profiles.map(profileName).join(" · ")}
                    </small>
                  </span>
                  <Signal
                    text={
                      state === "ok"
                        ? t.works
                        : state === "danger"
                          ? t.nodeUnavailable
                          : t.unknown
                    }
                    tone={state}
                  />
                </button>
              );
            })}
          </div>
        </section>
        <section className="panel">
          <div className="panel-title">
            <h2>{t.events}</h2>
            <button className="text-button" onClick={() => onView("activity")}>
              {t.details} →
            </button>
          </div>
          {jobs.length ? (
            <div className="desk-list">
              {jobs.slice(0, 5).map((job) => (
                <button key={job.id} onClick={() => onView("activity")}>
                  <span
                    className={`status-dot ${job.state === "succeeded" ? "ok" : ["running", "queued"].includes(job.state) ? "warn" : "danger"}`}
                  />
                  <span>
                    <strong>
                      {actionName(job.action, job.title, language)}
                    </strong>
                    <small>{timestamp(job.startedAt)}</small>
                  </span>
                </button>
              ))}
            </div>
          ) : (
            <p className="desk-panel-empty">{t.noActivity}</p>
          )}
        </section>
      </div>
      {problems.length > 0 && (
        <section className="panel">
          <div className="panel-title">
            <h2>{t.problems}</h2>
          </div>
          <div className="desk-list">
            {problems.map((user) => (
              <button key={user.username} onClick={() => onUser(user)}>
                <span className="status-dot warn" />
                <strong>{user.username}</strong>
                <span>{t.details} →</span>
              </button>
            ))}
          </div>
        </section>
      )}
    </>
  );
}
export function Users({
  language,
  users,
  servers,
  snapshot,
  onOpen,
  onAdd,
}: {
  language: Language;
  users: User[];
  servers: Server[];
  snapshot: Snapshot;
  onOpen: (user: User) => void;
  onAdd: () => void;
}) {
  const [query, setQuery] = useState("");
  const t = messages(language);
  if (!users.length)
    return (
      <Empty
        title={t.noUsers}
        body={t.emptyUsers}
        action={t.addUser}
        onAction={onAdd}
      />
    );
  const rows = users.filter((user) =>
    user.username.toLowerCase().includes(query.toLowerCase()),
  );
  return (
    <section className="panel">
      <div className="desk-table-toolbar">
        <input
          type="search"
          aria-label={t.search}
          placeholder={t.search}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <span>
          {rows.length} / {users.length}
        </span>
      </div>
      <div className="table-scroll">
        <table className="desk-table">
          <thead>
            <tr>
              <th>{t.user}</th>
              <th>{t.access}</th>
              <th>{t.connected}</th>
              <th>{t.traffic}</th>
              <th>{t.remaining}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map((user) => {
              const live = snapshot.servers
                .flatMap((server) => server.users)
                .filter((row) => row.username === user.username);
              const state = accessState(user, live, snapshot.running);
              const online = connectedState(live, snapshot.running);
              const quota = closestQuota(live);
              const knownTraffic = live.filter((row) => row.usedBytes !== null);
              const traffic =
                knownTraffic.length === user.configuredNodes
                  ? knownTraffic.reduce((sum, row) => sum + row.usedBytes!, 0)
                  : null;
              return (
                <tr key={user.username}>
                  <td>
                    <button
                      className="desk-person"
                      onClick={() => onOpen(user)}
                    >
                      <span aria-hidden>{user.username[0].toUpperCase()}</span>
                      <strong>{user.username}</strong>
                    </button>
                    <small>
                      {user.configuredNodes} {t.servers.toLowerCase()}
                    </small>
                  </td>
                  <td>
                    <Signal text={t[state.key]} tone={state.tone} />
                    <small>
                      {timestamp(
                        live
                          .map((row) => row.accessAt)
                          .filter(Boolean)
                          .sort()[0],
                      )}
                    </small>
                  </td>
                  <td>
                    <Signal
                      text={
                        online === true
                          ? t.connected
                          : online === false
                            ? t.disconnected
                            : t.unknown
                      }
                      tone={online === true ? "ok" : "neutral"}
                    />
                  </td>
                  <td>
                    <strong>{bytes(traffic)}</strong>
                    <small>{t.quotaWindow}</small>
                  </td>
                  <td>
                    <strong>
                      {live.length > 0 &&
                      live.every((row) => row.quotaEnabled === false)
                        ? t.unlimited
                        : bytes(quota?.remainingBytes)}
                    </strong>
                    {quota && (
                      <>
                        <progress
                          data-level={
                            quota.remainingBytes === 0
                              ? "danger"
                              : quota.remainingBytes! / quota.quotaBytes! < 0.1
                                ? "warn"
                                : "ok"
                          }
                          max={quota.quotaBytes!}
                          value={quota.usedBytes ?? 0}
                        />
                        <small>
                          {
                            servers.find(
                              (server) => server.id === quota.serverId,
                            )?.name
                          }{" "}
                          · {t.minRemaining}
                        </small>
                      </>
                    )}
                  </td>
                  <td>
                    <button
                      className="text-button"
                      aria-label={`${t.viewUser} ${user.username}`}
                      onClick={() => onOpen(user)}
                    >
                      {t.details} →
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
export function Servers({
  language,
  servers,
  snapshot,
  onOpen,
  onAdd,
}: {
  language: Language;
  servers: Server[];
  snapshot: Snapshot;
  onOpen: (server: Server) => void;
  onAdd: () => void;
}) {
  const t = messages(language);
  if (!servers.length)
    return (
      <Empty
        title={t.noServers}
        body={t.emptyServers}
        action={t.addServer}
        onAction={onAdd}
      />
    );
  return (
    <div className="desk-server-grid">
      {servers.map((server) => {
        const live = snapshot.servers.find((row) => row.serverId === server.id);
        const fresh = live?.state === "online" && isRecent(live.checkedAt);
        const failed =
          isRecent(live?.checkedAt) &&
          ["failed", "unreachable"].includes(live?.state ?? "");
        const passed =
          live?.users.reduce((sum, user) => sum + user.routesPassed, 0) ?? 0;
        const expected =
          live?.users.reduce((sum, user) => sum + user.routesTotal, 0) ?? 0;
        return (
          <button
            key={server.id}
            className="panel desk-server-card"
            onClick={() => onOpen(server)}
          >
            <div>
              <span className="desk-server-symbol">▤</span>
              <Signal
                text={fresh ? t.works : failed ? t.nodeUnavailable : t.unknown}
                tone={fresh ? "ok" : failed ? "danger" : "neutral"}
              />
            </div>
            <h2>{server.name}</h2>
            <p>
              {server.role === "proxy" ? "Proxy" : "Xray"} ·{" "}
              {server.xrayVersion}
            </p>
            <div className="desk-profile-pills">
              {server.profiles.map((profile) => (
                <span key={profile}>{profileName(profile)}</span>
              ))}
            </div>
            <dl>
              <dt>{t.coverage}</dt>
              <dd>
                {passed} / {expected}
              </dd>
              <dt>{t.lastCheck}</dt>
              <dd>{timestamp(live?.checkedAt)}</dd>
            </dl>
            <span className="text-button">{t.details} →</span>
          </button>
        );
      })}
    </div>
  );
}
export function Activity({
  language,
  jobs,
  history,
  toolJobs,
  nodes,
  onRefresh,
}: {
  language: Language;
  jobs: Job[];
  history: Observation[];
  toolJobs: ObservationJob[];
  nodes: Server[];
  onRefresh: () => void;
}) {
  const t = messages(language);
  const [error, setError] = useState("");
  if (!jobs.length && !history.length && !toolJobs.length)
    return <Empty title={t.activity} body={t.noActivity} />;
  return (
    <section className="panel">
      {error && <p role="alert">{error}</p>}
      <div className="table-scroll">
        <table className="desk-table">
          <thead>
            <tr>
              <th>{t.action}</th>
              <th>{t.current}</th>
              <th>{t.servers}</th>
              <th>{t.time}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id}>
                <td>
                  <strong>{actionName(job.action, job.title, language)}</strong>
                  <small className="mono">{job.id.slice(0, 8)}</small>
                </td>
                <td>
                  <Signal
                    text={operationLabel(job.state, language)}
                    tone={
                      job.state === "succeeded"
                        ? "ok"
                        : ["running", "queued"].includes(job.state)
                          ? "warn"
                          : "danger"
                    }
                  />
                  {job.code && (
                    <details>
                      <summary>{t.details}</summary>
                      <code>{job.code}</code>
                    </details>
                  )}
                </td>
                <td>
                  {job.steps.map((step) => (
                    <div key={step.target.id} className="desk-step">
                      <span
                        className={`status-dot ${step.state === "succeeded" ? "ok" : step.code ? "danger" : "neutral"}`}
                      />
                      {step.target.name}
                    </div>
                  ))}
                </td>
                <td className="small-time">{timestamp(job.startedAt)}</td>
                <td>
                  {["running", "queued"].includes(job.state) ? (
                    <button
                      className="text-button"
                      onClick={() =>
                        void desk
                          .cancel(job.id)
                          .then(onRefresh)
                          .catch(() => setError(t.operationError))
                      }
                    >
                      {t.cancel}
                    </button>
                  ) : (
                    job.state !== "succeeded" && (
                      <button
                        className="text-button"
                        onClick={() =>
                          void desk
                            .resume(job.id)
                            .then(onRefresh)
                            .catch(() => setError(t.operationError))
                        }
                      >
                        {t.resume}
                      </button>
                    )
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {toolJobs.length > 0 && (
        <section className="desk-history">
          <h2>{t.tools}</h2>
          {toolJobs.slice(0, 10).map((job) => (
            <p key={job.id}>
              <Signal
                text={
                  job.engine +
                  " · " +
                  ((t as Record<string, string>)[job.state] ?? job.state)
                }
                tone={
                  job.state === "completed"
                    ? "ok"
                    : job.state === "failed"
                      ? "danger"
                      : "warn"
                }
              />{" "}
              <small>{timestamp(job.startedAt)}</small>
              {job.code && (
                <details>
                  <summary>{t.details}</summary>
                  <code>{job.code}</code>
                </details>
              )}
            </p>
          ))}
        </section>
      )}
      {history.length > 0 && (
        <section className="desk-history">
          <h2>{t.checks}</h2>
          <div className="table-scroll">
            <table className="desk-table">
              <thead>
                <tr>
                  <th>{t.user}</th>
                  <th>{t.server}</th>
                  <th>{t.profile}</th>
                  <th>{t.current}</th>
                  <th>{t.time}</th>
                </tr>
              </thead>
              <tbody>
                {history
                  .slice()
                  .sort(
                    (a, b) =>
                      Date.parse(b.measuredAt) - Date.parse(a.measuredAt),
                  )
                  .slice(0, 100)
                  .map((sample) => (
                    <tr key={sample.id}>
                      <td>{sample.target.username}</td>
                      <td>
                        {nodes.find(
                          (node) => node.id === sample.target.serverId,
                        )?.name ?? sample.target.serverId}
                      </td>
                      <td>
                        {profileName(sample.target.profile)}{" "}
                        <small>{sample.source}</small>
                      </td>
                      <td>
                        <Signal
                          text={
                            sample.outcome === "passed"
                              ? t.works
                              : sample.outcome === "failed"
                                ? t.failed
                                : t.unknown
                          }
                          tone={
                            sample.outcome === "passed"
                              ? "ok"
                              : sample.outcome === "failed"
                                ? "danger"
                                : "neutral"
                          }
                        />
                        {sample.code && (
                          <details>
                            <summary>{t.details}</summary>
                            <code>{sample.code}</code>
                          </details>
                        )}
                      </td>
                      <td>{timestamp(sample.measuredAt)}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </section>
  );
}
export function Detail({
  language,
  user,
  server,
  servers,
  snapshot,
  catalog,
  onAction,
  onClose,
}: {
  language: Language;
  user?: User;
  server?: Server;
  servers: Server[];
  snapshot: Snapshot;
  catalog: Action[];
  onAction: (selection: Selection) => void;
  onClose: () => void;
}) {
  const t = messages(language);
  const ref = useRef<HTMLDialogElement>(null);
  const [routes, setRoutes] = useState<Route[]>([]);
  const [advanced, setAdvanced] = useState(false);
  const rows = snapshot.servers
    .flatMap((node) => node.users)
    .filter(
      (row) =>
        (!user || row.username === user.username) &&
        (!server || row.serverId === server.id),
    );
  useEffect(() => {
    ref.current?.showModal();
    return () => ref.current?.close();
  }, []);
  useEffect(() => {
    let alive = true;
    async function load() {
      try {
        let cursor = "";
        const items: Route[] = [];
        do {
          const page = await desk.routes(
            server?.id ?? "",
            user?.username ?? "",
            cursor,
          );
          items.push(...page.items);
          cursor = page.nextCursor ?? "";
        } while (cursor);
        if (alive) setRoutes(items);
      } catch {}
    }
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [server?.id, user?.username]);
  function choose(id: string) {
    const action = catalog.find((action) => action.id === id);
    if (!action) return;
    const values: Record<string, string> = {};
    if (user) values.username = user.username;
    if (server) values.server = server.name;
    else if (user) {
      const node =
        servers.find(
          (node) => node.enabled && user.nodeIds?.includes(node.id),
        ) ??
        servers.find(
          (node) =>
            node.enabled && rows.some((row) => row.serverId === node.id),
        ) ??
        servers.find((node) => node.enabled);
      if (node) values.server = node.name;
    }
    const args = action.arguments.map((field) =>
      field.name === "server" ? (server?.name ?? "") : "",
    );
    const known = Object.fromEntries(
      Object.entries(values).filter(([key]) =>
        action.fields.some((field) => field.name === key),
      ),
    );
    onAction({ action, values: known, arguments: args });
  }
  const primary = user
    ? [
        "user.link",
        "user.invite",
        "user.enable",
        "user.disable",
        "user.quota-set",
        "user.expiry-set",
        "user.expiry-clear",
        "user.probe",
        "user.diagnose",
        "observe.diagnose",
      ]
    : [
        "server.init",
        "deploy",
        "restart",
        "doctor",
        "server.logs",
        "server.backup",
        "server.restore",
      ];
  const advancedActions = catalog.filter((action) =>
    user
      ? (action.area === "users" || action.id === "observe.diagnose") &&
        !primary.includes(action.id)
      : action.area === "servers" && !primary.includes(action.id),
  );
  return (
    <dialog ref={ref} className="desk-dialog desk-detail" onCancel={onClose}>
      <header>
        <div>
          <small>{user ? t.user : t.server}</small>
          <h2>{user?.username ?? server?.name}</h2>
        </div>
        <button className="icon-button" aria-label={t.close} onClick={onClose}>
          ×
        </button>
      </header>
      <div className="desk-form">
        <div className="desk-detail-actions">
          {primary.map((id) => {
            const action = catalog.find((action) => action.id === id);
            return (
              action && (
                <button
                  key={id}
                  className={`button ${id === "user.link" ? "primary" : ""}`}
                  onClick={() => choose(id)}
                >
                  {actionName(id, action.title, language)}
                </button>
              )
            );
          })}
        </div>
        {server && <p>{t.serverSetupHint}</p>}
        {user && rows.length > 0 && (
          <section>
            <h3>{t.traffic}</h3>
            <p>{t.quotaWindow}</p>
            <div className="desk-assignment-cards">
              {rows.map((row) => (
                <Assignment
                  key={row.serverId + row.username}
                  row={row}
                  language={language}
                  serverName={
                    servers.find((server) => server.id === row.serverId)
                      ?.name ?? row.serverId
                  }
                  running={snapshot.running}
                />
              ))}
            </div>
          </section>
        )}
        {server && rows.length > 0 && (
          <section>
            <h3>{t.users}</h3>
            <div className="table-scroll desk-server-users">
              <table className="desk-table">
                <thead>
                  <tr>
                    <th>{t.user}</th>
                    <th>{t.connected}</th>
                    <th>{t.traffic}</th>
                    <th>{t.remaining}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={row.username}>
                      <td>{row.username}</td>
                      <td>
                        {connectedState([row], snapshot.running) === true
                          ? t.connected
                          : connectedState([row], snapshot.running) === false
                            ? t.disconnected
                            : t.unknown}
                      </td>
                      <td>
                        {bytes(row.usedBytes)}
                        <small>{timestamp(row.trafficAt)}</small>
                      </td>
                      <td>
                        {row.quotaEnabled === false
                          ? t.unlimited
                          : bytes(row.remainingBytes)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}
        <section>
          <h3>{t.methods}</h3>
          <p>{t.checkInfo}</p>
          {routes.length ? (
            <div className="table-scroll">
              <table className="desk-table">
                <thead>
                  <tr>
                    <th>{user ? t.server : t.user}</th>
                    <th>{t.profile}</th>
                    <th>{t.access}</th>
                    <th>{t.duration}</th>
                    <th>{t.lastCheck}</th>
                    <th>{t.nextCheck}</th>
                  </tr>
                </thead>
                <tbody>
                  {routes.map((route) => {
                    const fresh = route.sample?.fresh && snapshot.running;
                    const key =
                      route.state === "checking"
                        ? "checking"
                        : !fresh
                          ? "unknown"
                          : route.state === "passed"
                            ? "works"
                            : route.state === "failed"
                              ? "failed"
                              : "unknown";
                    return (
                      <tr key={JSON.stringify(route.target)}>
                        <td>
                          {user
                            ? servers.find(
                                (server) => server.id === route.target.serverId,
                              )?.name
                            : route.target.username}
                        </td>
                        <td>{profileName(route.target.profile)}</td>
                        <td>
                          <Signal
                            text={t[key]}
                            tone={
                              key === "works"
                                ? "ok"
                                : key === "failed"
                                  ? "danger"
                                  : key === "checking"
                                    ? "warn"
                                    : "neutral"
                            }
                          />
                          {route.sample?.code && (
                            <details>
                              <summary>{t.details}</summary>
                              <code>{route.sample.code}</code>
                            </details>
                          )}
                        </td>
                        <td>
                          {route.sample?.ttfbMs
                            ? `${route.sample.ttfbMs} ms`
                            : "—"}
                        </td>
                        <td>{timestamp(route.sample?.measuredAt)}</td>
                        <td>{timestamp(route.nextAt)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="desk-panel-empty">{t.noRoutes}</p>
          )}
        </section>
        <button
          className="text-button"
          aria-expanded={advanced}
          onClick={() => setAdvanced((old) => !old)}
        >
          {t.advanced} {advanced ? "−" : "+"}
        </button>
        {advanced && (
          <div className="desk-detail-actions">
            {advancedActions.map((action) => (
              <button
                key={action.id}
                className={`button ${action.destructive ? "danger-text" : ""}`}
                onClick={() => choose(action.id)}
              >
                {actionName(action.id, action.title, language)}
              </button>
            ))}
          </div>
        )}
      </div>
      <footer>
        <button className="button" onClick={onClose}>
          {t.close}
        </button>
      </footer>
    </dialog>
  );
}
function Assignment({
  row,
  language,
  serverName,
  running,
}: {
  row: UserLive;
  language: Language;
  serverName: string;
  running: boolean;
}) {
  const t = messages(language);
  const online = connectedState([row], running);
  return (
    <article className="desk-assignment">
      <h4>{serverName}</h4>
      <Signal
        text={
          online === true
            ? t.connected
            : online === false
              ? t.disconnected
              : row.presenceCode === "AGENT_UPDATE_REQUIRED"
                ? t.presenceUpdate
                : row.presenceCode === "CHECK_IN_PROGRESS"
                  ? t.probePresence
                  : t.unknown
        }
        tone={online === true ? "ok" : "neutral"}
      />
      <dl>
        <dt>{t.traffic}</dt>
        <dd>{bytes(row.usedBytes)}</dd>
        <dt>{t.remaining}</dt>
        <dd>
          {row.quotaEnabled === false ? t.unlimited : bytes(row.remainingBytes)}
        </dd>
        <dt>{t.limit}</dt>
        <dd>
          {row.quotaEnabled === false ? t.unlimited : bytes(row.quotaBytes)}
        </dd>
      </dl>
      {row.quotaBytes !== null && (
        <progress max={row.quotaBytes} value={row.usedBytes ?? 0} />
      )}
      <small>{timestamp(row.trafficAt)}</small>
    </article>
  );
}
