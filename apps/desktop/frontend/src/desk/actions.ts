import type { Action } from "./api";
import type { Server, User } from "../api";
export const actionGroups = [
  "access",
  "delivery",
  "connections",
  "ha",
  "monitoring",
  "diagnostics",
  "maintenance",
  "danger",
  "system",
] as const;
export type ActionGroup = (typeof actionGroups)[number];
export function actionGroup(action: Action): ActionGroup {
  if (action.destructive) return "danger";
  const id = action.id;
  if (id.startsWith("server.backend.") || id === "server.ha-apply") return "ha";
  if (id.startsWith("server.profile.") || id.startsWith("server.set-"))
    return "connections";
  if (
    id.startsWith("server.monitor.") ||
    id.startsWith("observe.policy.") ||
    id === "observe.run"
  )
    return "monitoring";
  if (
    id.startsWith("observe.") ||
    id.startsWith("user.debug.") ||
    [
      "doctor",
      "user.probe",
      "user.diagnose",
      "server.logs",
      "server.test",
      "server.status",
    ].includes(id)
  )
    return "diagnostics";
  if (
    id.startsWith("subscription.") ||
    /subscription|invite|link|qr|export/.test(id)
  )
    return "delivery";
  if (id.startsWith("user.")) return "access";
  if (
    [
      "server.init",
      "deploy",
      "restart",
      "server.backup",
      "config.render",
      "config.validate",
    ].includes(id)
  )
    return "maintenance";
  return "system";
}
export function contextualActions(
  catalog: Action[],
  user?: User,
  server?: Server,
): Action[] {
  return catalog.filter((action) => {
    if (user) {
      if (action.area !== "users" && action.id !== "observe.diagnose")
        return false;
      if (
        [
          "user.add",
          "user.list",
          "user.top",
          "user.reconcile",
          "user.plan",
          "subscription.serve",
          "subscription.invite",
        ].includes(action.id)
      )
        return false;
      if (action.id === "user.enable" && !user.disabledNodes) return false;
      if (action.id === "user.disable" && !user.enabledNodes) return false;
      if (action.id === "user.expiry-clear" && !user.earliestExpiry)
        return false;
      return true;
    }
    if (server) {
      if (
        action.area !== "servers" ||
        ["server.add", "server.list", "server.inspect"].includes(action.id)
      )
        return false;
      if (actionGroup(action) === "ha" && server.role !== "proxy") return false;
      return true;
    }
    return true;
  });
}
