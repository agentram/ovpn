import { useEffect, useRef, useState } from 'react';
import { backend, demoUserPlan, isStalePlanError } from './api';
import type { User, UserPlan, UserPlanRequest, UserPolicy } from './api';
import { catalog } from './i18n';
import type { Language } from './i18n';

export function UserPlanReview({request,language,mode,users,onBack,onBusy,onDemoApply}: {request: UserPlanRequest; language: Language; mode: string; users: User[]; onBack: ()=>void; onBusy: (busy: boolean)=>void; onDemoApply: ()=>void}) {
 const t=catalog(language);
 const [plan,setPlan]=useState<UserPlan|null>(null);
 const [busy,setBusy]=useState(true);
 const [error,setError]=useState<'stale'|'failed'|null>(null);
 const [checked,setChecked]=useState(false);
 const generation=useRef(0);
 async function load(recheck: boolean) {
  const token=++generation.current;
  setBusy(true);onBusy(true);setError(null);setChecked(false);
  try {
   const params={...request,expectedDigest:recheck?plan?.digest:undefined};
   const result=await (mode==='demo'?demoUserPlan(params,users):backend.planUser(params));
   if(token===generation.current){setPlan(result);setChecked(recheck);}
  } catch(e) {if(token===generation.current)setError(isStalePlanError(e)?'stale':'failed');}
  finally {if(token===generation.current){setBusy(false);onBusy(false);}}
 }
 useEffect(()=>{void load(false);return ()=>{generation.current++;onBusy(false);};},[request,onBusy]);
 const action=request.action==='create'?t.createUser:request.action==='enable'?t.enableUser:t.disableUser;
 const issue=(code:string)=>({USER_EXISTS:t.duplicate,USER_MISSING:t.planMissing,IDENTITY_CONFLICT:t.identityProblem,NO_ENABLED_SERVERS:t.planNoNodes,INVALID_EXPIRY:t.planInvalidExpiry}[code]??t.planIssue);
 function policy(value: UserPolicy|null) {
  if(!value)return <span className="secondary">{t.planNoRecord}</span>;
  return <div className="policy-summary"><span className={`badge ${value.enabled?'blue':''}`}>{value.enabled?t.on:t.disabledPolicy}</span><small>{value.expiresAt?`${t.expiry}: ${value.expiresAt.slice(0,10)} UTC (${t.planExclusive})`:t.noExpiry}</small><small>{t.quotaServer}: {!value.quotaEnabled?t.unlimited:!value.quotaBytes?t.planDefaultQuota:`${(value.quotaBytes/1073741824).toFixed(1)} GB`}</small>{value.expiresAt&&Date.parse(value.expiresAt)<=Date.now()&&<span className="badge warn">{t.expired}</span>}{value.quotaBlocked&&<span className="badge danger">{t.blocked}</span>}</div>;
 }
 return <>
  <div className="dialog-body plan-review" aria-busy={busy}>
   <div className="caption">{mode==='demo'?t.demoPlan:t.localPlan}</div><h2>{action} · {request.username}</h2>
   <div className="notice preview-banner"><strong>{t.planNotApplied}</strong><p>{mode==='demo'?t.demoOnly:t.planReadOnly}</p></div>
   {busy&&<p role="status">{t.planLoading}</p>}
   {error&&<div className="notice danger-notice" role="alert">{error==='stale'?t.planStale:t.planError}</div>}
   {checked&&<div className="notice" role="status">{t.planUnchanged}</div>}
   {plan&&<><div className="plan-scope"><strong>{t.affected}: {plan.targets.length}</strong><span className="badge warn">{t.planRemoteUnknown}</span></div>
    {plan.issues.map(code=><div key={code} className="notice danger-notice">{issue(code)}</div>)}
    <div className="plan-targets">{plan.targets.map(target=><section className="plan-target" key={target.serverId}><div className="plan-node"><strong>{target.name}</strong><span className="badge">{target.role}</span></div><p className="mono">{target.profiles.join(' · ')}</p><div className="policy-comparison"><div><h3>{t.planBefore}</h3>{policy(target.before)}</div><span aria-hidden="true">→</span><div><h3>{t.planAfter}</h3>{policy(target.after)}</div></div>{target.issues.map(code=><p className="danger-text" key={code}>{issue(code)}</p>)}</section>)}</div>
    <p className="help-line">{t.planDigest}: <code>{plan.digest.slice(0,12)}</code> · {t.planSnapshotOnly}</p>
    <div className="notice"><strong>{t.planNext}</strong><p>{t.planExecutionSteps}</p></div>
   </>}
  </div>
  <div className="dialog-foot"><button className="button" disabled={busy} onClick={onBack}>{t.cancel}</button>{plan&&!error&&<button className="button" disabled={busy} onClick={()=>void load(true)}>{t.planRecheck}</button>}{(!plan||error)&&<button className="button primary" disabled={busy} onClick={()=>void load(false)}>{t.planRebuild}</button>}{mode==='demo'&&plan&&!error&&<button className="button primary" disabled={busy||plan.issues.length>0||plan.targets.some(t=>t.issues.length>0)} onClick={onDemoApply}>{t.applyDemo}</button>}</div>
 </>;
}
