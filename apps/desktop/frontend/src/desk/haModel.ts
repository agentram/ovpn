import type { Server } from "../api";
import type { HAStatus, ServerLive } from "./api";
import { isRecent } from "./model.ts";
export function poolSignal(server: Server, ha?: HAStatus) {
  const expected = (server.backends ?? []).filter((row) => row.enabled);
  if (!expected.length) return { tone: "warn", key: "noBackends" } as const;
  if (!ha?.supported || !isRecent(ha.checkedAt) || ha.code)
    return { tone: "neutral", key: "poolUnknown" } as const;
  const states = expected.map(
    (row) =>
      ha.backends.find((live) => live.serverId === row.serverId)?.available,
  );
  const up = states.filter((value) => value === true).length;
  const down = states.filter((value) => value === false).length;
  if (down === expected.length)
    return { tone: "danger", key: "poolFailed" } as const;
  if (up === expected.length && ha.backends.length === expected.length)
    return {
      tone: expected.length > 1 ? "ok" : "warn",
      key: expected.length > 1 ? "poolAvailable" : "singleBackend",
    } as const;
  if (up || down) return { tone: "warn", key: "poolPartial" } as const;
  return { tone: "neutral", key: "poolUnknown" } as const;
}
export function serverSignal(server: Server, live?: ServerLive) {
  if (!server.enabled || !isRecent(live?.checkedAt))
    return { tone: "neutral", key: "unknown" } as const;
  if (["unreachable", "failed"].includes(live?.state ?? ""))
    return { tone: "danger", key: "nodeUnavailable" } as const;
  if (live?.state !== "online")
    return { tone: "neutral", key: "unknown" } as const;
  if (server.role === "proxy") return poolSignal(server, live.ha);
  return { tone: "ok", key: "works" } as const;
}
