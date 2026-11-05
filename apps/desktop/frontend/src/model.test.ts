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
