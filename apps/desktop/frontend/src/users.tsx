import { useEffect, useRef, useState } from 'react';
import { catalog } from './i18n';
import type { Language } from './i18n';
import { userPolicy, userQuotaKind, demoSetUserEnabled } from './api';
import type { User, UserObservation, UserPlanRequest, Observation } from './api';
import { UserPlanReview } from './plan-review';
import {observationEn,observationRu} from './observation-i18n';

export function policyTone(user: User, enabled: number) {
 const state=userPolicy(user,enabled);
 return ['conflict','blocked'].includes(state)?'danger':['partial','expired'].includes(state)?'warn':state==='allowed'?'ok':'';
}

export function UsersPanel({language,mode,users,observations,automatic,enabled,busy,more,onMore,onCheck,onDemoChange,onPlanning}: {
 language: Language; mode: string; users: User[]; observations: Record<string,UserObservation>; automatic: Observation[]; enabled: number; busy: boolean; more: boolean;
 onMore: ()=>void; onCheck: (username: string)=>void; onDemoChange: (users: User[])=>void; onPlanning: (busy: boolean)=>void;
}) {
 const t=catalog(language);
 const ot=language==='ru'?observationRu:observationEn;
 const autoFor=(name:string)=>automatic.filter(s=>s.target.username===name);
 const autoLabel=(name:string)=>{const rows=autoFor(name);if(!rows.length)return ot.noAuto;return `${rows.filter(s=>s.fresh&&s.outcome==='passed').length} / ${rows.length}`;};
 const autoTone=(name:string)=>{const rows=autoFor(name);return !rows.length?'':rows.some(s=>s.fresh&&s.outcome==='failed')?'danger':rows.every(s=>s.fresh&&s.outcome==='passed')?'ok':'warn';};
 const [query,setQuery]=useState('');
 const [selected,setSelected]=useState<User|null>(null);
 const [request,setRequest]=useState<UserPlanRequest|null>(null);
 const [create,setCreate]=useState(false);
 const [name,setName]=useState('');
 const [quotaGB,setQuotaGB]=useState('100');
 const modal=useRef<HTMLDialogElement>(null);
 const open=!!selected||create;
 useEffect(()=>{if(open)modal.current?.showModal();else modal.current?.close();},[open]);
 const labels={allowed:t.allowed,blocked:t.blocked,expired:t.expired,disabled:t.disabledPolicy,partial:t.partial,conflict:t.conflict,unassigned:t.unassigned};
 function close(){setSelected(null);setCreate(false);setRequest(null);setName('');setQuotaGB('100');}
 function applyDemo(){
  if(mode!=='demo'||!request)return;
  if(request.action==='create'){
   if(users.some(u=>u.username===request.username))return;
   const expiresAt=request.expiry?new Date(Date.parse(`${request.expiry}T00:00:00Z`)+86400000).toISOString():undefined;
   const expired=!!expiresAt&&Date.parse(expiresAt)<=Date.now();
   onDemoChange([{username:request.username,configuredNodes:enabled,enabledNodes:enabled,eligibleNodes:expired?0:enabled,disabledNodes:0,expiredNodes:expired?enabled:0,blockedNodes:0,earliestExpiry:expiresAt,quotaMinBytes:request.quotaBytes??0,quotaMaxBytes:request.quotaBytes??0,policyDiff:false,identityConflict:false},...users]);
  }else{
   onDemoChange(users.map(u=>u.username===request.username?demoSetUserEnabled(u,request.action==='enable'):u));
  }
  close();
 }
 function add(event: React.FormEvent<HTMLFormElement>){
  event.preventDefault();
  const quota=Number(quotaGB);
  const expiry=String(new FormData(event.currentTarget).get('expiry')??'');
  if(!name.trim()||!Number.isSafeInteger(quota)||quota<=0||quota>1000000)return;
  setRequest({action:'create',username:name.trim(),quotaBytes:quota*1073741824,...(expiry?{expiry}:{})});
 }
 const quota=(bytes:number)=>bytes===0?t.unlimited:`${(bytes/1073741824).toFixed(bytes%1073741824?1:0)} GB`;
 const quotaText=(user:User)=>{const kind=userQuotaKind(user);return kind==='unknown'?t.unassigned:kind==='default'?t.defaultQuota:kind==='mixed'?t.mixedQuota:kind==='unmetered'?t.unlimited:quota(user.quotaMinBytes)+(user.quotaMinBytes!==user.quotaMaxBytes?` – ${quota(user.quotaMaxBytes)}`:'');};
 const lastCheck=(name:string)=>{const sample=observations[name];return !sample?t.unknown:!sample.fresh?t.stale:sample.outcome==='passed'?t.passed:t.failed;};
 const checkTone=(name:string)=>{const sample=observations[name];return !sample?'':!sample.fresh?'warn':sample.outcome==='passed'?'ok':'danger';};
 const filtered=users.filter(u=>u.username.toLowerCase().includes(query.toLowerCase()));
 return <>
  <div className="users-toolbar">
   <input aria-label={t.searchUsers} placeholder={t.searchUsers} value={query} onChange={e=>setQuery(e.target.value)}/>
   <button className="button primary" disabled={busy} onClick={()=>setCreate(true)}>＋ {mode==='demo'?t.createUser:t.planCreate}</button>
   <span className={`badge ${mode==='demo'?'blue':''}`}>{mode==='demo'?t.demoOnly:t.liveRead}</span>
  </div>
  <div className="notice"><strong>{t.policyOnly}</strong><p>{t.userScope}</p></div>
  <section className="panel table-panel users-table">
   <div className="panel-title"><h2>{t.allUsers}<small>{users.length}</small></h2><span className="badge blue">{t.quotaServer}</span></div>
   <div className="table-scroll"><table>
    <thead><tr><th>{t.username}</th><th>{t.policy}</th><th>{t.assignments}</th><th>{t.quotaServer}</th><th>{t.expiry}</th><th>{t.connectivity}</th><th>{ot.automated}</th><th>{t.actions}</th></tr></thead>
    <tbody>{filtered.map(user=><tr key={user.username}>
     <td><button className="person-button" aria-label={user.username} onClick={()=>setSelected(user)}><span className={`person-avatar ${policyTone(user,enabled)}`}>{user.username.slice(0,1).toUpperCase()}</span><strong>{user.username}</strong></button></td>
     <td><span className={`badge ${policyTone(user,enabled)}`}><i/>{labels[userPolicy(user,enabled)]}</span></td>
     <td className="mono">{user.enabledNodes} / {enabled}</td>
     <td className="mono">{quotaText(user)}</td>
     <td>{user.earliestExpiry?user.earliestExpiry.slice(0,10):t.noExpiry}</td>
     <td><span className={`badge ${checkTone(user.username)}`}>{lastCheck(user.username)}</span>{observations[user.username]&&<small className="scope-hint">{t.singlePath}</small>}</td>
     <td><span className={`badge ${autoTone(user.username)}`}>{autoLabel(user.username)}</span></td>
     <td><button className="text-button" aria-label={`${t.inspectUser} ${user.username}`} onClick={()=>setSelected(user)}>{t.inspectUser} →</button></td>
    </tr>)}</tbody>
   </table></div>
   {!users.length&&!busy&&<div className="empty"><h2>{t.usersEmpty}</h2><p>{t.usersEmptyHint}</p></div>}
   {more&&<div className="table-foot"><button className="button" disabled={busy} onClick={onMore}>{t.more}</button></div>}
  </section>
  <dialog ref={modal} onCancel={close} onClose={close} className={request?'plan-dialog':undefined}>
   <div className="dialog-head"><h2>{request?t.reviewPlan:create?t.createUser:t.userDetail}</h2><button className="icon-button" aria-label={t.close} onClick={close}>✕</button></div>
   {request?<UserPlanReview request={request} language={language} mode={mode} users={users} onBack={()=>setRequest(null)} onBusy={onPlanning} onDemoApply={applyDemo}/>:create?<form onSubmit={add}>
    <div className="dialog-body">
     <label>{t.username}<input autoFocus required maxLength={128} value={name} onChange={e=>setName(e.target.value)} autoComplete="off"/></label>
     {users.some(u=>u.username===name.trim())&&<p className="danger-text">{t.duplicate}</p>}
     <label>{t.planQuota}<input type="number" required min={1} max={1000000} step={1} value={quotaGB} onChange={e=>setQuotaGB(e.target.value)}/></label>
     <label>{t.planExpiry}<input type="date" name="expiry"/></label>
     <div className="notice">{t.userScope}<p>{t.affected}: {enabled}</p></div>
    </div><div className="dialog-foot"><button type="submit" className="button primary">{t.reviewPlan}</button></div>
   </form>:selected&&<>
    <div className="dialog-body">
     <div className="user-detail-head"><span className={`person-avatar large ${policyTone(selected,enabled)}`}>{selected.username.slice(0,1).toUpperCase()}</span><div><h2>{selected.username}</h2><span className={`badge ${policyTone(selected,enabled)}`}>{labels[userPolicy(selected,enabled)]}</span></div></div>
     <dl><dt>{t.configuredRecords}</dt><dd>{selected.configuredNodes}</dd><dt>{t.eligibleRecords}</dt><dd>{selected.eligibleNodes} / {enabled}</dd><dt>{t.disabledPolicy}</dt><dd>{selected.disabledNodes}</dd><dt>{t.blockedRecords}</dt><dd>{selected.blockedNodes}</dd><dt>{t.expiredRecords}</dt><dd>{selected.expiredNodes}</dd><dt>{t.defaultQuotaCount}</dt><dd>{selected.defaultQuotaNodes??0}</dd><dt>{t.unmeteredCount}</dt><dd>{selected.unmeteredNodes??0}</dd></dl>
     {selected.identityConflict&&<div className="notice danger-notice">{t.identityProblem}</div>}
     <div className="notice">{observations[selected.username]?<><strong>{t.connectivity}: {lastCheck(selected.username)}</strong><p>{observations[selected.username].server} · {observations[selected.username].profile} · {t.singlePath}</p></>:t.noTrafficEvidence}</div>
     <div className="notice"><strong>{ot.automated}: {autoLabel(selected.username)}</strong>{autoFor(selected.username).map(sample=><p key={sample.source+sample.target.serverId+sample.target.profile}><span className={`badge ${sample.fresh&&sample.outcome==='passed'?'ok':sample.fresh&&sample.outcome==='failed'?'danger':'warn'}`}>{!sample.fresh?ot.stale:sample.outcome==='passed'?ot.pass:sample.outcome==='failed'?ot.fail:ot.issues}</span> {sample.target.serverId} · {sample.target.profile} · {sample.source}</p>)}</div><p className="plan-note">{t.policyOnly}</p>
    </div>
    <div className="dialog-foot"><button className="button" onClick={()=>{const username=selected.username;close();onCheck(username);}}>{t.checkUser}</button><button className="button primary" disabled={busy} onClick={()=>setRequest({action:selected.disabledNodes===selected.enabledNodes?'enable':'disable',username:selected.username})}>{t.manageUser}</button></div>
   </>}
  </dialog>
 </>;
}
