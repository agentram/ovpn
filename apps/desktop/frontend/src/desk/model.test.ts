import { test } from "node:test";
import assert from "node:assert/strict";
import { accessState, connectedState, closestQuota } from "./model.ts";
import { en, ru, zh } from "./i18n.ts";
import type { UserLive } from "./api";
import type { User } from "../api";
const person = { eligibleNodes: 2, blockedNodes: 0, expiredNodes: 0 } as User;
function row(node: string, passed: number, total = 1): UserLive {
  return {
    serverId: node,
    username: "alice",
    connected: null,
    usedBytes: null,
    remainingBytes: null,
    quotaBytes: null,
    quotaEnabled: null,
    runtimeApplied: null,
    routesTotal: total,
    routesPassed: passed,
    routesFailed: 0,
  };
}
test("green requires successful routes from every eligible assignment", () => {
  assert.equal(
    accessState(person, [row("1", 1), row("2", 1)], true).key,
    "works",
  );
  assert.equal(
    accessState(person, [row("1", 1), row("2", 0, 0)], true).key,
    "partial",
  );
  assert.equal(accessState(person, [row("1", 1)], true).key, "partial");
  assert.equal(
    accessState(person, [row("1", 1), row("2", 1)], false).key,
    "unknown",
  );
});
test("successful access checks do not imply current presence", () => {
  const rows = [row("1", 1), row("2", 1)];
  const now = Date.now();
  assert.equal(connectedState(rows, true, now), null);
  rows[0].connected = true;
  rows[0].presenceAt = new Date(now - 31000).toISOString();
  assert.equal(connectedState(rows, true, now), null);
  rows[0].presenceAt = new Date(now).toISOString();
  assert.equal(connectedState(rows, true, now), true);
  rows[0].connected = false;
  assert.equal(connectedState(rows, true, now), null);
  rows[1].connected = false;
  rows[1].presenceAt = new Date(now).toISOString();
  assert.equal(connectedState(rows, true, now), false);
});
test("remaining quota selects the closest server limit and preserves missing readings", () => {
  const first = { ...row("one", 1), quotaBytes: 100, remainingBytes: 50 };
  const second = { ...row("two", 1), quotaBytes: 20, remainingBytes: 1 };
  assert.equal(closestQuota([first, second])?.serverId, "two");
  assert.equal(closestQuota([row("unknown", 1)]), undefined);
});
test("all ordinary UI messages exist in the three supported languages", () => {
  assert.deepEqual(Object.keys(en).sort(), Object.keys(ru).sort());
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort());
  for (const dictionary of [en, ru, zh])
    for (const value of Object.values(dictionary)) assert.ok(value.trim());
});
test("quota blocks require fresh server data", () => {
  const rows = [
    {
      ...row("1", 0),
      blockedByQuota: true,
      trafficAt: new Date().toISOString(),
    },
  ];
  assert.equal(
    accessState({ ...person, eligibleNodes: 1 }, rows, true).key,
    "blocked",
  );
  rows[0].trafficAt = "2020-01-01T00:00:00Z";
  assert.equal(accessState(person, rows, true).key, "unknown");
});
test("a successful configured route cannot hide missing access on another enabled server", () => {
  const partial = { ...person, eligibleNodes: 1, missingNodes: 1 };
  assert.equal(accessState(partial, [row("one", 1)], true).key, "partial");
});
