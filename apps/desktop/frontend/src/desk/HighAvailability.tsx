import type { Server } from "../api";
import type { Snapshot } from "./api";
import type { Language } from "./i18n";
import { messages } from "./i18n";
import { uiCopy } from "./uiCopy";
import { poolSignal } from "./haModel";
import { isRecent, timestamp } from "./model";

export function HighAvailability({
  server,
  servers,
  snapshot,
  language,
  onChoose,
}: {
  server: Server;
  servers: Server[];
  snapshot: Snapshot;
  language: Language;
  onChoose: (id: string, values?: Record<string, string>) => void;
}) {
  const t = uiCopy(language),
    common = messages(language);
  const live = snapshot.servers.find((row) => row.serverId === server.id)?.ha;
  const signal = poolSignal(server, snapshot.running ? live : undefined);
  return (
    <section className="desk-ha" aria-label={t.ha}>
      <div className="desk-section-heading">
        <h3>{t.ha}</h3>
        <span className={`desk-signal ${signal.tone}`}>
          <span className={`status-dot ${signal.tone}`} />
          <span>{t[signal.key]}</span>
        </span>
      </div>
      <div className="desk-ha-flow">
        <span>{t.client}</span>
        <span aria-hidden>→</span>
        <strong>
          {server.name}
          <small>{t.entry}</small>
        </strong>
        <span aria-hidden>→</span>
        <span>{t.backendPool}</span>
      </div>
      <div className="desk-ha-backends">
        {(server.backends ?? []).map((link) => {
          const node = servers.find((row) => row.id === link.serverId);
          const available =
            snapshot.running &&
            isRecent(live?.checkedAt) &&
            !live?.code &&
            link.enabled
              ? live?.backends.find((row) => row.serverId === link.serverId)
                  ?.available
              : null;
          return (
            <div key={link.serverId} className="desk-ha-backend">
              <span
                className={`status-dot ${available === true ? "ok" : available === false ? "danger" : "neutral"}`}
              />
              <div>
                <strong>{node?.name ?? link.serverId}</strong>
                <small>
                  {!link.enabled
                    ? common.disabled
                    : available === true
                      ? common.works
                      : available === false
                        ? common.nodeUnavailable
                        : common.unknown}
                </small>
              </div>
              <button
                className="text-button danger-text"
                onClick={() =>
                  onChoose("server.backend.detach", {
                    backend: node?.name ?? "",
                  })
                }
              >
                {t.removeBackend}
              </button>
            </div>
          );
        })}
        {!server.backends?.length && <p>{t.noBackends}</p>}
      </div>
      <div className="desk-ha-toolbar">
        <button
          className="button"
          onClick={() => onChoose("server.backend.attach")}
        >
          + {t.addBackend}
        </button>
        <small>
          {t.routingPreset}: {server.proxyPreset?.toUpperCase() ?? "—"}
        </small>
      </div>
      <p>
        {t.relayCheck} <time>{timestamp(live?.checkedAt)}</time>
      </p>
      {live?.code && (
        <details>
          <summary>{common.details}</summary>
          <p>
            {live.code === "HA_METRICS_UNAVAILABLE"
              ? t.haMetricsDown
              : live.code === "HA_METRICS_INVALID"
                ? t.haMetricsInvalid
                : t.haUpdate}
          </p>
          <code>{live.code}</code>
        </details>
      )}
      <p className="desk-ha-notice">{t.proxyWarning}</p>
      <details>
        <summary>{common.details}</summary>
        <p>{t.splitRouting}</p>
        <p>{t.haApplyHint}</p>
      </details>
    </section>
  );
}
