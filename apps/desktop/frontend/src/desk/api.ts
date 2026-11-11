import type { Observation, ObservationTarget } from "../api";
export interface Field {
  options?: string[];
  name: string;
  label: string;
  type: string;
  default?: string;
  required: boolean;
  secret: boolean;
  advanced: boolean;
}
export interface Action {
  id: string;
  title: string;
  area: string;
  description: string;
  mutation: boolean;
  destructive: boolean;
  fields: Field[];
  arguments: Field[];
}
export interface ActionRequest {
  action: string;
  values: Record<string, string>;
  arguments: string[];
}
export interface Plan {
  id: string;
  action: string;
  title: string;
  digest: string;
  targets: { id: string; name: string }[];
  destructive: boolean;
  mutation: boolean;
  createdAt: string;
}
export interface Job {
  id: string;
  planId: string;
  action: string;
  title: string;
  state: string;
  code?: string;
  startedAt: string;
  finishedAt?: string;
  steps: {
    target: { id: string; name: string };
    state: string;
    code?: string;
  }[];
}
export interface Result {
  data?: unknown;
  text?: string;
  content?: string;
  mediaType?: string;
  qrData?: string;
}
export interface UserLive {
  accessAt?: string;
  nextCheckAt?: string;
  serverId: string;
  username: string;
  connected: boolean | null;
  blockedByQuota?: boolean | null;
  presenceAt?: string;
  presenceCode?: string;
  trafficAt?: string;
  usedBytes: number | null;
  quotaBytes: number | null;
  remainingBytes: number | null;
  quotaEnabled: boolean | null;
  runtimeApplied: boolean | null;
  routesTotal: number;
  routesPassed: number;
  routesFailed: number;
}
export interface ServerLive {
  serverId: string;
  state: string;
  code?: string;
  checkedAt?: string;
  agentVersion?: string;
  users: UserLive[];
}
export interface Route {
  target: ObservationTarget;
  state: string;
  sample?: Observation;
  nextAt: string;
}
export interface Snapshot {
  enabled: boolean;
  running: boolean;
  servers: ServerLive[];
  routes: Route[];
  checked: number;
  total: number;
  active: number;
}
async function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  const runtime = await import("@wailsio/runtime");
  return runtime.Call.ByName(
    `main.DeskService.${method}`,
    ...args,
  ) as Promise<T>;
}
export const desk = {
  catalog: () => invoke<Action[]>("Catalog"),
  plan: (request: ActionRequest) => invoke<Plan>("OperationPlan", request),
  start: (plan: Plan, key: string) =>
    invoke<Job>("OperationStart", {
      planId: plan.id,
      digest: plan.digest,
      key,
      confirmed: true,
    }),
  jobs: () => invoke<Job[]>("OperationList"),
  resume: (id: string) => invoke<Job>("OperationResume", id),
  cancel: (id: string) => invoke<void>("OperationCancel", id),
  result: (id: string) => invoke<Result>("OperationResult", id),
  snapshot: () => invoke<Snapshot>("DeskSnapshot"),
  configure: (enabled: boolean) =>
    invoke<Snapshot>("DeskConfigure", { enabled, trafficConfirmed: enabled }),
  routes: (serverId: string, username: string, cursor = "") =>
    invoke<{ items: Route[]; nextCursor?: string }>("DeskRoutes", {
      serverId,
      username,
      cursor,
      limit: 100,
    }),
  pause: () => invoke<void>("DeskPause"),
};
export function emptySnapshot(): Snapshot {
  return {
    enabled: false,
    running: false,
    servers: [],
    routes: [],
    checked: 0,
    total: 0,
    active: 0,
  };
}
