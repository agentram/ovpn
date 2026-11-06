export interface Summary { state: string; servers: number; enabledServers: number; users: number; readOnly: boolean }
export interface Server { id: string; name: string; role: string; enabled: boolean; primaryProfile: string; profiles: string[]; xrayVersion: string; runtime: string }
export interface ServerPage { items: Server[]; nextCursor?: string }
export interface User { username: string; configuredNodes: number; enabledNodes: number; eligibleNodes: number; disabledNodes: number; expiredNodes: number; blockedNodes: number; earliestExpiry?: string; quotaMinBytes: number; quotaMaxBytes: number; defaultQuotaNodes?: number; unmeteredNodes?: number; policyDiff: boolean; identityConflict: boolean }
export interface UserObservation { outcome: string; server: string; profile: string; fresh: boolean }
export interface UserPage { items: User[]; nextCursor?: string }
export interface CheckRequest { serverId: string; username: string; profile: string }
export interface Check { serverId: string; profile: string; source: string; kind: string; outcome: string; statusCode?: number; durationMs: number; ttfbMs: number; bytes: number; errorCode?: string; measuredAt: string; freshForSeconds: number; unverified: string[] }
export interface UserPlanRequest { action: 'create'|'enable'|'disable'; username: string; expiry?: string; quotaBytes?: number; expectedDigest?: string }
export interface UserPolicy { enabled: boolean; expiresAt?: string; quotaBytes?: number; quotaEnabled: boolean; quotaBlocked: boolean }
export interface UserPlanTarget { serverId: string; name: string; role: string; profiles: string[]; before: UserPolicy|null; after: UserPolicy|null; issues: string[] }
export interface UserPlan { action: string; username: string; scope: string; digest: string; generatedAt: string; executionAvailable: boolean; targets: UserPlanTarget[]; issues: string[]; requiredSteps: string[] }

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
  planUser: (request: UserPlanRequest) => invoke<UserPlan>('PlanUser', request),
};

export function isStalePlanError(error: unknown): boolean {
  return error instanceof Error && /(?:^|[^A-Z_])STALE_PLAN(?:$|[^A-Z_])/.test(error.message);
}

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
 {username:'carol',configuredNodes:2,enabledNodes:2,eligibleNodes:0,disabledNodes:0,expiredNodes:2,blockedNodes:0,earliestExpiry:'2026-01-01T00:00:00Z',unmeteredNodes:2,quotaMinBytes:0,quotaMaxBytes:0,policyDiff:false,identityConflict:false},
 {username:'support',unmeteredNodes:1,configuredNodes:1,enabledNodes:1,eligibleNodes:1,disabledNodes:0,expiredNodes:0,blockedNodes:0,quotaMinBytes:0,quotaMaxBytes:0,policyDiff:false,identityConflict:false},
 {username:'standard',configuredNodes:2,enabledNodes:2,eligibleNodes:2,disabledNodes:0,expiredNodes:0,blockedNodes:0,defaultQuotaNodes:2,quotaMinBytes:0,quotaMaxBytes:0,policyDiff:false,identityConflict:false},
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

export function userQuotaKind(user: User): 'explicit'|'default'|'unmetered'|'mixed'|'unknown' {
 if(user.enabledNodes===0)return 'unknown';
 const defaults=user.defaultQuotaNodes??0, unmetered=user.unmeteredNodes??0;
 if(defaults>0&&defaults===user.enabledNodes)return 'default';
 if(unmetered>0&&unmetered===user.enabledNodes)return 'unmetered';
 if(defaults>0||unmetered>0)return 'mixed';
 return user.quotaMaxBytes>0?'explicit':'unmetered';
}

export async function demoUserPlan(request: UserPlanRequest, users: User[]): Promise<UserPlan> {
 const {expectedDigest,...intent}=request;
 const raw=new TextEncoder().encode(JSON.stringify({intent,users,servers:demoServers}));
 const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',raw)),b=>b.toString(16).padStart(2,'0')).join('');
 if(expectedDigest&&expectedDigest!==digest)throw new Error('STALE_PLAN');
 const user=users.find(u=>u.username===request.username);
 const expiresAt=request.expiry?new Date(Date.parse(`${request.expiry}T00:00:00Z`)+86400000).toISOString().replace('.000Z','Z'):undefined;
 return {action:request.action,username:request.username,scope:'all_enabled_servers',digest,generatedAt:new Date().toISOString(),executionAvailable:false,issues:user?.identityConflict?['IDENTITY_CONFLICT']:[],requiredSteps:[],
  targets:demoServers.filter(s=>s.enabled).map((server,index)=>{
   const before: UserPolicy|null=user&&index<user.enabledNodes?{enabled:user.disabledNodes===0,expiresAt:user.earliestExpiry,quotaBytes:index<(user.defaultQuotaNodes??0)?undefined:user.quotaMinBytes||undefined,quotaEnabled:index>=(user.unmeteredNodes??0),quotaBlocked:index<user.blockedNodes}:null;
   const issues=request.action==='create'?(before?['USER_EXISTS']:[]):(!before?['USER_MISSING']:[]);
   const after: UserPolicy|null=issues.length?null:request.action==='create'?{enabled:true,expiresAt,quotaBytes:request.quotaBytes,quotaEnabled:true,quotaBlocked:false}:before?{...before,enabled:request.action==='enable'}:null;
   return {serverId:server.id,name:server.name,role:server.role,profiles:server.profiles,before,after,issues};
  })};
}

export interface ObservationTarget {serverId:string;username:string;profile:string}
export interface ObservationPolicy {id:string;name:string;engine:string;targets:ObservationTarget[];intervalSeconds:number;maxChecksPerDay:number;enabled:boolean;trafficConfirmed:boolean}
export interface Observation {id:number;jobId:string;target:ObservationTarget;source:string;engineVersion:string;revision:string;outcome:string;failureClass?:string;code?:string;statusCode?:number;durationMs:number;ttfbMs:number;bytes:number;measuredAt:string;sessionId:string;fresh:boolean;freshForSeconds:number;unverified:string[]}
export interface ObservationJob {id:string;kind:string;engine:string;policyId?:string;state:string;code?:string;total:number;completed:number;startedAt:string;finishedAt?:string}
export interface ObservationIncident {revision:string;target:ObservationTarget;source:string;class:string;state:string;failures:number;successes:number;firstSeen:string;lastSeen:string}
export interface ObservationTool {engine:string;version:string;configured:boolean}
export interface ObserverSnapshot {running:boolean;owner:string;sessionId:string;heartbeat?:string;previousHeartbeat?:string;policies:ObservationPolicy[];tools:ObservationTool[];jobs:ObservationJob[];latest:Observation[];history:Observation[];incidents:ObservationIncident[];retainedSamples:number;historyLimit:number}
export const observerBackend={
 status:()=>invoke<ObserverSnapshot>('ObserverStatus'),
 configure:(p:ObservationPolicy)=>invoke<ObservationPolicy>('ObserverConfigure',p),
 action:(action:string,id='')=>invoke<void>('ObserverAction',action,id),
 run:(policyId:string)=>invoke<ObservationJob>('ObserverRun',{policyId,trafficConfirmed:true}),
 diagnose:(engine:string,targets:ObservationTarget[])=>invoke<ObservationJob>('DiagnosticStart',{engine,targets,trafficConfirmed:true}),
 install:(engine:string)=>invoke<ObservationJob>('IntegrationInstall',engine),
};
export function emptyObserver():ObserverSnapshot{return {running:false,owner:'stopped',sessionId:'',policies:[],tools:[],jobs:[],latest:[],history:[],incidents:[],retainedSamples:0,historyLimit:5000};}
export function observationFresh(sample:Observation,online:boolean,receiptMono:number,receiptWall:number,nowMono:number,nowWall:number):boolean {
 const age=nowWall-Date.parse(sample.measuredAt),mono=nowMono-receiptMono,wall=nowWall-receiptWall;
 return online&&sample.fresh&&age>=0&&age<sample.freshForSeconds*1000&&mono>=0&&mono<10000&&wall>=0&&wall<10000&&Math.abs(wall-mono)<2000;
}
export function observationTone(sample:Observation):string{return !sample.fresh?'warn':sample.outcome==='passed'?'ok':sample.outcome==='failed'?'danger':'warn';}
export function observationError(error:unknown):string {
 if(!(error instanceof Error))return 'OBSERVATION_UNAVAILABLE';
 const codes=['OBSERVER_IN_USE','JOB_BUSY','TRAFFIC_CONFIRMATION_REQUIRED','TARGET_UNAVAILABLE','TOOL_UNAVAILABLE','TOOL_DOWNLOAD_FAILED','TOOL_ARCHIVE_HASH_MISMATCH','TOOL_VERSION_UNSUPPORTED','DUPLICATE_SCHEDULE','INVALID_PARAMS','NO_ENABLED_POLICIES'];
 return codes.find(code=>error.message.includes(code))??'OBSERVATION_UNAVAILABLE';
}
export function observationTargetKey(target:ObservationTarget):string{return JSON.stringify([target.serverId,target.username,target.profile]);}
