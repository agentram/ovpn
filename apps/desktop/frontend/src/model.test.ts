import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { demoCheck, isFresh } from './api.ts';
import { en, ru, catalog } from './i18n.ts';

test('every English message has a Russian translation', () => {
  assert.deepEqual(Object.keys(en).sort(),Object.keys(ru).sort());
  for (const value of Object.values(ru)) assert.ok(value.length>0);
  assert.equal(catalog('en').overview,'Overview');
  assert.equal(catalog('ru').overview,'Обзор');
});
test('a passed sample expires and does not survive a reversed clock', () => {
  const check = demoCheck({serverId:'node-1',profile:'vless-reality-tcp-vision',username:'monitor'});
  assert.equal(isFresh(check,1000,1000),true);
  assert.equal(isFresh(check,1000,300999),true);
  assert.equal(isFresh(check,1000,301000),false);
  assert.equal(isFresh(check,1000,999),false);
});
test('demo outcomes identify their source and preserve unverified scope', () => {
  const check = demoCheck({serverId:'node-2',profile:'vless-tcp-tls-selfsni-web',username:'monitor'});
  assert.equal(check.source,'demo');
  assert.equal(check.outcome,'failed');
  assert.ok(check.unverified.includes('udp'));
  assert.ok(!JSON.stringify(check).includes('monitor'));
});

test('allowed policy never claims per-user connectivity and partial scope remains partial', async () => {
  const {demoUsers,userPolicy}=await import('./api.ts');
  assert.equal(userPolicy(demoUsers[0],2),'allowed');
  assert.equal(userPolicy(demoUsers[1],2),'blocked');
  assert.equal(userPolicy(demoUsers[2],2),'expired');
  assert.equal(userPolicy(demoUsers[3],2),'partial');
  assert.equal(userPolicy({...demoUsers[0],identityConflict:true},2),'conflict');
  assert.equal(userPolicy({...demoUsers[0],enabledNodes:0,eligibleNodes:0},2),'unassigned');
});

test('demo suspension preserves expiry, quota and identity facets', async () => {
  const {demoUsers,demoSetUserEnabled,userPolicy}=await import('./api.ts');
  const before=demoUsers[1];
  const disabled=demoSetUserEnabled(before,false);
  assert.equal(disabled.blockedNodes,before.blockedNodes);
  assert.equal(disabled.quotaMaxBytes,before.quotaMaxBytes);
  assert.equal(userPolicy(disabled,2),'disabled');
  const enabled=demoSetUserEnabled(disabled,true);
  assert.equal(enabled.eligibleNodes,0);
  assert.equal(userPolicy(enabled,2),'blocked');
});

test('plan review captures scope, preserves independent policy and detects a changed snapshot', async () => {
  const {demoUsers,demoUserPlan}=await import('./api.ts');
  const request={action:'enable' as const,username:'bob'};
  const plan=await demoUserPlan(request,demoUsers);
  assert.equal(plan.executionAvailable,false);
  assert.deepEqual(plan.targets.map(t=>t.name),['edge-a','edge-b']);
  assert.ok(plan.targets.every(t=>t.after?.quotaBlocked));
  assert.equal(plan.targets[0].after?.quotaBytes,demoUsers[1].quotaMinBytes);
  const checked=await demoUserPlan({...request,expectedDigest:plan.digest},demoUsers);
  assert.equal(checked.digest,plan.digest);
  await assert.rejects(demoUserPlan({...request,expectedDigest:plan.digest},demoUsers.slice(1)),/STALE_PLAN/);
  await assert.rejects(demoUserPlan({...request,action:'disable',expectedDigest:plan.digest},demoUsers),/STALE_PLAN/);
  const missing=await demoUserPlan({action:'disable',username:'support'},demoUsers);
  assert.ok(missing.targets[1].issues.includes('USER_MISSING'));
  const create=await demoUserPlan({action:'create',username:'new-user',expiry:'2030-01-01',quotaBytes:100},demoUsers);
  assert.equal(create.targets[0].after?.expiresAt,'2030-01-02T00:00:00Z');
});

test('plan error messages expose a stale classification without forwarding diagnostics', async () => {
  const {isStalePlanError}=await import('./api.ts');
  assert.equal(isStalePlanError(new Error('The request could not be completed (STALE_PLAN).')),true);
  assert.equal(isStalePlanError(new Error('private host or credential')),false);
  assert.equal(isStalePlanError('STALE_PLAN'),false);
});

test('quota display distinguishes inherited defaults, unmetered nodes and mixed policy', async () => {
  const {demoUsers,userQuotaKind}=await import('./api.ts');
  const user=demoUsers[0];
  assert.equal(userQuotaKind(user),'explicit');
  assert.equal(userQuotaKind({...user,quotaMinBytes:0,quotaMaxBytes:0,defaultQuotaNodes:2}),'default');
  assert.equal(userQuotaKind({...user,quotaMinBytes:0,quotaMaxBytes:0,unmeteredNodes:2}),'unmetered');
  assert.equal(userQuotaKind({...user,defaultQuotaNodes:1}),'mixed');
  assert.equal(userQuotaKind({...user,unmeteredNodes:1}),'mixed');
  assert.equal(userQuotaKind({...user,enabledNodes:0}),'unknown');
});

test('observation translations cover the complete enabled workflow',async()=>{
 const {observationEn,observationRu}=await import('./observation-i18n.ts');
 assert.deepEqual(Object.keys(observationEn).sort(),Object.keys(observationRu).sort());
 for(const value of Object.values(observationRu))assert.ok(value.length>0);
});
test('automatic evidence expires on source loss, cached queries, sleep and clock uncertainty',async()=>{
 const {observationFresh,observationError}=await import('./api.ts');
 const sample={fresh:true,measuredAt:new Date(100000).toISOString(),freshForSeconds:600};
 assert.equal(observationFresh(sample as any,true,100,100000,200,100100),true);
 assert.equal(observationFresh(sample as any,false,100,100000,200,100100),false);
 assert.equal(observationFresh(sample as any,true,100,100000,10200,110100),false);
 assert.equal(observationFresh(sample as any,true,100,100000,200,106000),false);
 assert.equal(observationFresh(sample as any,true,100,100000,90,99990),false);
 assert.equal(observationError(new Error('private-host token detail')),'OBSERVATION_UNAVAILABLE');
 assert.equal(observationError(new Error('The request failed (OBSERVER_IN_USE)')),'OBSERVER_IN_USE');
});
test('target identity is stable across JSON property order and keeps credential references distinct',async()=>{
 const {observationTargetKey}=await import('./api.ts');
 assert.equal(observationTargetKey({serverId:'node-1',username:'monitor',profile:'tcp'}),observationTargetKey({profile:'tcp',username:'monitor',serverId:'node-1'}));
 assert.notEqual(observationTargetKey({serverId:'node-1',username:'a',profile:'tcp'}),observationTargetKey({serverId:'node-1',username:'b',profile:'tcp'}));
});
