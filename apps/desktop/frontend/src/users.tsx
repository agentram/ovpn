import { useEffect, useRef, useState } from 'react';
import { catalog } from './i18n';
import type { Language } from './i18n';
import { userPolicy, demoSetUserEnabled } from './api';
import type { User, UserObservation } from './api';

export function policyTone(user: User, enabled: number) {
  const state=userPolicy(user,enabled);
  return ['conflict','blocked'].includes(state)?'danger':['partial','expired'].includes(state)?'warn':state==='allowed'?'ok':'';
}
export function UsersPanel({language,mode,users,observations,enabled,busy,more,onMore,onCheck,onDemoChange}: {
 language: Language; mode: string; users: User[]; observations: Record<string,UserObservation>; enabled: number; busy: boolean; more: boolean;
 onMore: ()=>void; onCheck: (username: string)=>void; onDemoChange: (users: User[])=>void;
}) {
 const t=catalog(language);
 const [query,setQuery]=useState('');
 const [selected,setSelected]=useState<User|null>(null);
 const [plan,setPlan]=useState(false);
 const [create,setCreate]=useState(false);
 const [name,setName]=useState('');
 const modal=useRef<HTMLDialogElement>(null);
 const open=!!selected||create;
 useEffect(()=>{if(open)modal.current?.showModal();else modal.current?.close();},[open]);
 const labels={allowed:t.allowed,blocked:t.blocked,expired:t.expired,disabled:t.disabledPolicy,partial:t.partial,conflict:t.conflict,unassigned:t.unassigned};
 function close(){setSelected(null);setCreate(false);setPlan(false);setName('');}
 function apply(){
  if(mode!=='demo'||!selected)return;
  const enable=selected.disabledNodes===selected.enabledNodes;
  onDemoChange(users.map(u=>u.username===selected.username?demoSetUserEnabled(u,enable):u));
  close();
 }
 function add(event: React.FormEvent<HTMLFormElement>){
  event.preventDefault();if(mode!=='demo'||!name.trim()||users.some(u=>u.username===name.trim()))return;
  onDemoChange([{username:name.trim(),configuredNodes:enabled,enabledNodes:enabled,eligibleNodes:enabled,disabledNodes:0,expiredNodes:0,blockedNodes:0,quotaMinBytes:107374182400,quotaMaxBytes:107374182400,policyDiff:false,identityConflict:false},...users]);close();
 }
 const quota=(bytes: number)=>bytes===0?t.unlimited:`${(bytes/1073741824).toFixed(bytes%1073741824?1:0)} GB`;
 const lastCheck=(name:string)=>{const sample=observations[name];return !sample?t.unknown:!sample.fresh?t.stale:sample.outcome==='passed'?t.passed:t.failed;};
 const checkTone=(name:string)=>{const sample=observations[name];return !sample?'':!sample.fresh?'warn':sample.outcome==='passed'?'ok':'danger';};
 const filtered=users.filter(u=>u.username.toLowerCase().includes(query.toLowerCase()));
 return <>
  <div className="users-toolbar"><input aria-label={t.searchUsers} placeholder={t.searchUsers} value={query} onChange={e=>setQuery(e.target.value)}/>{mode==='demo'&&<button className="button primary" onClick={()=>setCreate(true)}>＋ {t.createUser}</button>}<span className={`badge ${mode==='demo'?'blue':''}`}>{mode==='demo'?t.demoOnly:t.liveRead}</span></div>
  <div className="notice"><strong>{t.policyOnly}</strong><p>{t.userScope}</p></div>
  <section className="panel table-panel users-table"><div className="panel-title"><h2>{t.allUsers}<small>{users.length}</small></h2><span className="badge blue">{t.quotaServer}</span></div><div className="table-scroll"><table><thead><tr><th>{t.username}</th><th>{t.policy}</th><th>{t.assignments}</th><th>{t.quotaServer}</th><th>{t.expiry}</th><th>{t.connectivity}</th><th>{t.actions}</th></tr></thead><tbody>{filtered.map(user=><tr key={user.username}><td><button className="person-button" aria-label={user.username} onClick={()=>{setSelected(user);setPlan(false);}}><span className={`person-avatar ${policyTone(user,enabled)}`}>{user.username.slice(0,1).toUpperCase()}</span><strong>{user.username}</strong></button></td><td><span className={`badge ${policyTone(user,enabled)}`}><i/>{labels[userPolicy(user,enabled)]}</span></td><td className="mono">{user.enabledNodes} / {enabled}</td><td className="mono">{quota(user.quotaMinBytes)}{user.quotaMinBytes!==user.quotaMaxBytes?` – ${quota(user.quotaMaxBytes)}`:''}</td><td>{user.earliestExpiry?user.earliestExpiry.slice(0,10):t.noExpiry}</td><td><span className={`badge ${checkTone(user.username)}`}>{lastCheck(user.username)}</span>{observations[user.username]&&<small className="scope-hint">{t.singlePath}</small>}</td><td><button className="text-button" aria-label={`${t.inspectUser} ${user.username}`} onClick={()=>{setSelected(user);setPlan(false);}}>{t.inspectUser} →</button></td></tr>)}</tbody></table></div>{!users.length&&!busy&&<div className="empty"><h2>{t.usersEmpty}</h2><p>{t.usersEmptyHint}</p></div>}{more&&<div className="table-foot"><button className="button" disabled={busy} onClick={onMore}>{t.more}</button></div>}</section>
  <dialog ref={modal} onCancel={close} onClose={close}><div className="dialog-head"><h2>{create?t.createUser:plan?(mode==='demo'?t.demoPlan:t.manageUser):t.userDetail}</h2><button className="icon-button" aria-label={t.close} onClick={close}>✕</button></div>{create?<form onSubmit={add}><div className="dialog-body"><label>{t.username}<input autoFocus required maxLength={128} value={name} onChange={e=>setName(e.target.value)} autoComplete="off"/></label>{users.some(u=>u.username===name.trim())&&<p className="danger-text">{t.duplicate}</p>}<div className="notice">{t.userScope}<p>{t.quotaServer}: 100 GB · {t.noExpiry} · {t.affected}: {enabled}</p></div></div><div className="dialog-foot"><button type="submit" className="button primary">{t.addDemo}</button></div></form>:selected&&<><div className="dialog-body"><div className="user-detail-head"><span className={`person-avatar large ${policyTone(selected,enabled)}`}>{selected.username.slice(0,1).toUpperCase()}</span><div><h2>{selected.username}</h2><span className={`badge ${policyTone(selected,enabled)}`}>{labels[userPolicy(selected,enabled)]}</span></div></div><dl><dt>{t.configuredRecords}</dt><dd>{selected.configuredNodes}</dd><dt>{t.eligibleRecords}</dt><dd>{selected.eligibleNodes} / {enabled}</dd><dt>{t.disabledPolicy}</dt><dd>{selected.disabledNodes}</dd><dt>{t.blockedRecords}</dt><dd>{selected.blockedNodes}</dd><dt>{t.expiredRecords}</dt><dd>{selected.expiredNodes}</dd></dl>{selected.identityConflict&&<div className="notice danger-notice">{t.identityProblem}</div>}{plan?<><div className="notice"><strong>{selected.disabledNodes===selected.enabledNodes?t.enableUser:t.disableUser} · {t.affected}: {enabled}</strong><p>{t.preserved} · {t.planVerify}</p></div>{mode!=='demo'&&<p className="plan-note">{t.managementPlanned}</p>}</>:<><div className="notice">{observations[selected.username]?<><strong>{t.connectivity}: {lastCheck(selected.username)}</strong><p>{observations[selected.username].server} · {observations[selected.username].profile} · {t.singlePath}</p></>:t.noTrafficEvidence}</div><p className="plan-note">{t.policyOnly}</p></>}</div><div className="dialog-foot">{plan?<><button className="button" onClick={()=>setPlan(false)}>{t.cancel}</button>{mode==='demo'&&<button className={`button ${selected.disabledNodes===selected.enabledNodes?'primary':'destructive'}`} disabled={selected.identityConflict} onClick={apply}>{t.applyDemo}</button>}</>:<><button className="button" onClick={()=>{const username=selected.username;close();onCheck(username);}}>{t.checkUser}</button><button className="button primary" onClick={()=>setPlan(true)}>{t.manageUser}</button></>}</div></>}</dialog>
 </>;
}
