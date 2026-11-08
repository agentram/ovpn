import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { isFresh, userPolicy, userQuotaKind } from './api.ts';
import type { Check, User } from './api.ts';
import { en, ru, catalog } from './i18n.ts';

test('every English message has a Russian translation', () => {
  assert.deepEqual(Object.keys(en).sort(),Object.keys(ru).sort());
  for (const value of Object.values(ru)) assert.ok(value.length>0);
  assert.equal(catalog('en').overview,'Overview');
  assert.equal(catalog('ru').overview,'Обзор');
});
const eligibleUser: User = {username:'fixture',configuredNodes:2,enabledNodes:2,eligibleNodes:2,disabledNodes:0,expiredNodes:0,blockedNodes:0,quotaMinBytes:1024,quotaMaxBytes:1024,policyDiff:false,identityConflict:false};

test('a passed sample expires and does not survive a reversed clock', () => {
  const check = {freshForSeconds: 300} as Check;
  assert.equal(isFresh(check,1000,1000),true);
  assert.equal(isFresh(check,1000,300999),true);
  assert.equal(isFresh(check,1000,301000),false);
  assert.equal(isFresh(check,1000,999),false);
});
test('allowed policy never claims per-user connectivity and partial scope remains partial', () => {
  assert.equal(userPolicy(eligibleUser,2),'allowed');
  assert.equal(userPolicy({...eligibleUser,eligibleNodes:0,blockedNodes:2},2),'blocked');
  assert.equal(userPolicy({...eligibleUser,eligibleNodes:0,expiredNodes:2},2),'expired');
  assert.equal(userPolicy({...eligibleUser,enabledNodes:1,eligibleNodes:1},2),'partial');
  assert.equal(userPolicy({...eligibleUser,identityConflict:true},2),'conflict');
  assert.equal(userPolicy({...eligibleUser,enabledNodes:0,eligibleNodes:0},2),'unassigned');
});

test('plan error messages expose a stale classification without forwarding diagnostics', async () => {
  const {isStalePlanError}=await import('./api.ts');
  assert.equal(isStalePlanError(new Error('The request could not be completed (STALE_PLAN).')),true);
  assert.equal(isStalePlanError(new Error('private host or credential')),false);
  assert.equal(isStalePlanError('STALE_PLAN'),false);
});

test('quota display distinguishes inherited defaults, unmetered nodes and mixed policy', () => {
  const user=eligibleUser;
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
