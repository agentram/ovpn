import { test } from "node:test";
import assert from "node:assert/strict";
import { contextualActions, actionGroup } from "./actions.ts";
import { serverSignal, poolSignal } from "./haModel.ts";
import { toolName, toolJobState, toolJobLabel } from "./toolModel.ts";
import { uiCopy } from "./uiCopy.ts";
import type { Action, HAStatus, ServerLive } from "./api";
import type { Server, User, ObservationJob } from "../api";
const proxy = {
  role: "proxy",
  enabled: true,
  backends: [
    { serverId: "node-1", enabled: true },
    { serverId: "node-2", enabled: true },
  ],
} as Server;
const now = new Date().toISOString();
function ha(values: (boolean | null)[]): HAStatus {
  return {
    supported: true,
    state: "online",
    checkedAt: now,
    backends: values.map((available, i) => ({
      serverId: `node-${i + 1}`,
      available,
    })),
  };
}
test("proxy cannot be green from core health or a stale or incomplete pool", () => {
  const core = { state: "online", checkedAt: now } as ServerLive;
  assert.equal(serverSignal(proxy, core).tone, "neutral");
  assert.equal(
    serverSignal(proxy, { ...core, ha: ha([true, true]) }).tone,
    "ok",
  );
  assert.equal(poolSignal(proxy, ha([true, false])).tone, "warn");
  assert.equal(poolSignal(proxy, ha([false, false])).tone, "danger");
  assert.equal(poolSignal(proxy, ha([true])).tone, "warn");
  assert.equal(
    poolSignal(proxy, {
      ...ha([true, true]),
      checkedAt: "2000-01-01T00:00:00Z",
    }).tone,
    "neutral",
  );
  assert.equal(
    poolSignal({ ...proxy, backends: proxy.backends!.slice(0, 1) }, ha([true]))
      .key,
    "singleBackend",
  );
});
test("context menus exclude unrelated global actions and keep role-specific controls", () => {
  const catalog = [
    { id: "server.add", area: "servers" },
    { id: "server.backend.attach", area: "servers" },
    { id: "server.ha-apply", area: "servers" },
    { id: "server.logs", area: "servers" },
    { id: "user.add", area: "users" },
    { id: "user.disable", area: "users" },
    { id: "user.enable", area: "users" },
  ] as Action[];
  assert.deepEqual(
    contextualActions(catalog, undefined, { role: "vpn" } as Server).map(
      (a) => a.id,
    ),
    ["server.logs"],
  );
  assert.ok(
    contextualActions(catalog, undefined, proxy).some(
      (a) => a.id === "server.ha-apply",
    ),
  );
  assert.deepEqual(
    contextualActions(catalog, {
      enabledNodes: 2,
      disabledNodes: 0,
    } as User).map((a) => a.id),
    ["user.disable"],
  );
  assert.equal(contextualActions(catalog).length, catalog.length);
  assert.equal(actionGroup({ id: "server.backend.attach" } as Action), "ha");
  assert.equal(
    actionGroup({ id: "user.rm", destructive: true } as Action),
    "danger",
  );
});
test("tool history uses translated task states and distinguishes engine preparation", () => {
  for (const language of ["en", "ru", "zh"] as const) {
    assert.ok(toolName("native", language));
    assert.equal(
      toolJobLabel({ kind: "diagnostic" } as ObservationJob, language),
      uiCopy(language).diagnose,
    );
    assert.equal(
      toolJobState("completed", language),
      uiCopy(language).finished,
    );
    assert.notEqual(toolJobState("completed", language), "completed");
    for (const text of Object.values(uiCopy(language))) assert.ok(text.trim());
  }
  assert.deepEqual(
    Object.keys(uiCopy("en")).sort(),
    Object.keys(uiCopy("ru")).sort(),
  );
  assert.deepEqual(
    Object.keys(uiCopy("en")).sort(),
    Object.keys(uiCopy("zh")).sort(),
  );
});
