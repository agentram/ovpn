import type { User } from "../api";
import type { UserLive } from "./api";
export type Tone = "ok" | "warn" | "danger" | "neutral";
export function isRecent(
  timestamp: string | undefined,
  now = Date.now(),
  seconds = 60,
): boolean {
  if (!timestamp) return false;
  const age = now - Date.parse(timestamp);
  return Number.isFinite(age) && age >= 0 && age < seconds * 1000;
}
export function accessState(
  user: User,
  rows: UserLive[],
  running: boolean,
): {
  key:
    | "disabled"
    | "expired"
    | "blocked"
    | "works"
    | "partial"
    | "failed"
    | "unknown";
  tone: Tone;
} {
  if (user.eligibleNodes === 0)
    return {
      key:
        user.blockedNodes > 0
          ? "blocked"
          : user.expiredNodes > 0
            ? "expired"
            : "disabled",
      tone: "neutral",
    };
  if (
    running &&
    rows.length >= user.eligibleNodes &&
    rows.length > 0 &&
    rows.every((row) => row.blockedByQuota === true && isRecent(row.trafficAt))
  )
    return { key: "blocked", tone: "warn" };
  if (!running) return { key: "unknown", tone: "neutral" };
  const expected = rows.reduce((sum, row) => sum + row.routesTotal, 0);
  const passed = rows.reduce((sum, row) => sum + row.routesPassed, 0);
  const failed = rows.reduce((sum, row) => sum + row.routesFailed, 0);
  if (!expected) return { key: "unknown", tone: "neutral" };
  if (
    passed === expected &&
    rows.filter((row) => row.routesTotal > 0).length >= user.eligibleNodes
  )
    return { key: "works", tone: "ok" };
  if (
    failed === expected &&
    rows.filter((row) => row.routesTotal > 0).length >= user.eligibleNodes
  )
    return { key: "failed", tone: "danger" };
  if (passed || failed) return { key: "partial", tone: "warn" };
  return { key: "unknown", tone: "neutral" };
}
export function connectedState(
  rows: UserLive[],
  running: boolean,
  now = Date.now(),
): boolean | null {
  if (!running || rows.length === 0) return null;
  const fresh = rows.filter((row) => isRecent(row.presenceAt, now, 30));
  if (fresh.some((row) => row.connected === true)) return true;
  return fresh.length === rows.length &&
    fresh.every((row) => row.connected === false)
    ? false
    : null;
}
export function closestQuota(rows: UserLive[]): UserLive | undefined {
  return rows
    .filter(
      (row) =>
        row.remainingBytes !== null &&
        row.quotaBytes !== null &&
        row.quotaBytes > 0,
    )
    .sort(
      (a, b) =>
        a.remainingBytes! / a.quotaBytes! - b.remainingBytes! / b.quotaBytes!,
    )[0];
}
export function bytes(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value))
    return "—";
  if (value < 1024) return `${value} B`;
  const scale = Math.min(
    3,
    Math.floor(Math.log(Math.max(value, 1)) / Math.log(1024)),
  );
  return `${(value / 1024 ** scale).toFixed(1)} ${["B", "KiB", "MiB", "GiB"][scale]}`;
}
export function timestamp(value?: string): string {
  if (!value || !Number.isFinite(Date.parse(value))) return "—";
  return new Date(value)
    .toISOString()
    .replace("T", " ")
    .replace(/\.\d+Z$/, " UTC");
}
export function requestKey(): string {
  return crypto.randomUUID().replaceAll("-", "");
}
