export interface Summary { state: string; servers: number; enabledServers: number; users: number; readOnly: boolean }
export interface Server { id: string; name: string; role: string; enabled: boolean; primaryProfile: string; profiles: string[]; xrayVersion: string; runtime: string }
export interface ServerPage { items: Server[]; nextCursor?: string }
export interface User { username: string; configuredNodes: number; enabledNodes: number; eligibleNodes: number; disabledNodes: number; expiredNodes: number; blockedNodes: number; earliestExpiry?: string; quotaMinBytes: number; quotaMaxBytes: number; policyDiff: boolean; identityConflict: boolean }
export interface UserObservation { outcome: string; server: string; profile: string; fresh: boolean }
export interface UserPage { items: User[]; nextCursor?: string }
export interface CheckRequest { serverId: string; username: string; profile: string }
export interface Check { serverId: string; profile: string; source: string; kind: string; outcome: string; statusCode?: number; durationMs: number; ttfbMs: number; bytes: number; errorCode?: string; measuredAt: string; freshForSeconds: number; unverified: string[] }

async function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  const runtime = await import('@wailsio/runtime');
  return runtime.Call.ByName(`main.DeskService.${method}`, ...args) as Promise<T>;
}
export const backend = {
  hello: () => invoke<{protocolVersion: number; engineVersion: string}>('Hello'),
  summary: () => invoke<Summary>('Summary'),
  servers: (cursor = '') => invoke<ServerPage>('Servers', cursor),
  users: (cursor = '') => invoke<UserPage>('Users', cursor),
  check: (request: CheckRequest) => invoke<Check>('Check', request),
};

export function isFresh(check: Check, receivedAt: number, now: number): boolean {
  const age = now - receivedAt;
  return age >= 0 && age < check.freshForSeconds * 1000;
}

export const demoServers: Server[] = [
  {id:'node-1',name:'edge-a',role:'vpn',enabled:true,primaryProfile:'vless-reality-tcp-vision',profiles:['vless-reality-tcp-vision'],xrayVersion:'26.3.27',runtime:'not_checked'},
  {id:'node-2',name:'edge-b',role:'vpn',enabled:true,primaryProfile:'vless-tcp-tls-selfsni-web',profiles:['vless-tcp-tls-selfsni-web','vless-xhttp-plain'],xrayVersion:'26.3.27',runtime:'not_checked'},
  {id:'node-3',name:'reserve-a',role:'vpn',enabled:false,primaryProfile:'vless-reality-tcp-vision',profiles:['vless-reality-tcp-vision'],xrayVersion:'26.3.27',runtime:'not_checked'},
];

export function demoCheck(request: CheckRequest): Check {
  const failed = request.serverId === 'node-2';
  return {serverId:request.serverId,profile:request.profile,source:'demo',kind:'https.tcp',outcome:failed?'failed':'passed',statusCode:failed?undefined:200,durationMs:failed?2000:142,ttfbMs:failed?0:124,bytes:failed?0:528,errorCode:failed?'request_failed':undefined,measuredAt:new Date().toISOString(),freshForSeconds:300,unverified:['udp','dns','ipv6','gui-import']};
}

export const demoUsers: User[] = [
 {username:'alice',configuredNodes:2,enabledNodes:2,eligibleNodes:2,disabledNodes:0,expiredNodes:0,blockedNodes:0,quotaMinBytes:107374182400,quotaMaxBytes:107374182400,policyDiff:false,identityConflict:false},
 {username:'bob',configuredNodes:2,enabledNodes:2,eligibleNodes:0,disabledNodes:0,expiredNodes:0,blockedNodes:2,quotaMinBytes:53687091200,quotaMaxBytes:53687091200,policyDiff:false,identityConflict:false},
 {username:'carol',configuredNodes:2,enabledNodes:2,eligibleNodes:0,disabledNodes:0,expiredNodes:2,blockedNodes:0,earliestExpiry:'2026-01-01T00:00:00Z',quotaMinBytes:0,quotaMaxBytes:0,policyDiff:false,identityConflict:false},
 {username:'support',configuredNodes:1,enabledNodes:1,eligibleNodes:1,disabledNodes:0,expiredNodes:0,blockedNodes:0,quotaMinBytes:0,quotaMaxBytes:0,policyDiff:false,identityConflict:false},
];
export type PolicyState = 'conflict' | 'unassigned' | 'blocked' | 'expired' | 'disabled' | 'partial' | 'allowed';
export function userPolicy(user: User, enabledServers: number): PolicyState {
 if(user.identityConflict) return 'conflict';
 if(user.enabledNodes===0) return 'unassigned';
 if(user.disabledNodes===user.enabledNodes) return 'disabled';
 if(user.eligibleNodes===0) return user.blockedNodes>0?'blocked':user.expiredNodes>0?'expired':user.disabledNodes===user.enabledNodes?'disabled':'partial';
 if(user.policyDiff || user.eligibleNodes<user.enabledNodes || user.enabledNodes<enabledServers) return 'partial';
 return 'allowed';
}

export function demoSetUserEnabled(user: User, enabled: boolean): User {
 return {...user,disabledNodes:enabled?0:user.enabledNodes,eligibleNodes:enabled&&user.blockedNodes===0&&user.expiredNodes===0?user.enabledNodes:0};
}
