import {useEffect,useRef,useState} from 'react';
import {emptyObserver,observerBackend,observationFresh,observationError,observationTargetKey} from './api';
import type {ObserverSnapshot,Observation,ObservationPolicy,ObservationTarget} from './api';

const demoTarget={serverId:'node-1',username:'alice',profile:'vless-reality-tcp-vision'};
export function demoObservation(target:ObservationTarget,source='native',index=1):Observation {
 return {id:index,jobId:`demo-${index}`,target,source,engineVersion:'demo',revision:'demo',outcome:target.serverId==='node-2'?'failed':'passed',statusCode:target.serverId==='node-2'?undefined:204,durationMs:target.serverId==='node-2'?2000:142,ttfbMs:target.serverId==='node-2'?0:124,bytes:0,measuredAt:new Date().toISOString(),sessionId:'demo',fresh:true,freshForSeconds:600,unverified:['runtime','udp','dns','ipv6','gui-import','wire-bytes']};
}
function initialDemo():ObserverSnapshot {
 const first=demoObservation(demoTarget),second=demoObservation({...demoTarget,serverId:'node-2',profile:'vless-tcp-tls-selfsni-web'},'checker',2);
 return {...emptyObserver(),sessionId:'demo',owner:'demo',latest:[{...first,fresh:false},{...second,fresh:false}],history:[second,first],retainedSamples:2,tools:[],policies:[{id:'demo-policy',name:'Example policy',engine:'native',targets:[demoTarget],intervalSeconds:300,maxChecksPerDay:300,enabled:true,trafficConfirmed:true}]};
}
export function useObservation(mode:string) {
 const [snapshot,setSnapshot]=useState<ObserverSnapshot>(initialDemo);
 const [online,setOnline]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const stamp=useRef({mono:performance.now(),wall:Date.now()});
 const epoch=useRef(0),fetching=useRef(false),serial=useRef(10);
 useEffect(()=>{
  const generation=++epoch.current;stamp.current={mono:performance.now(),wall:Date.now()};setError('');setOnline(mode==='demo');setSnapshot(mode==='demo'?initialDemo():emptyObserver());
  let inFlight=false;
  async function poll(){if(inFlight)return;inFlight=true;try{const value=await observerBackend.status();if(epoch.current===generation){stamp.current={mono:performance.now(),wall:Date.now()};setSnapshot(value);setOnline(true);}}catch{if(epoch.current===generation)setOnline(false);}finally{inFlight=false;}}
  if(mode!=='demo')void poll();const timer=setInterval(()=>{if(mode!=='demo')void poll();},2000);
  return ()=>{clearInterval(timer);epoch.current++;};
 },[mode]);
 async function perform<T>(fn:()=>Promise<T>):Promise<T|undefined>{
  if(fetching.current)return;fetching.current=true;setBusy(true);setError('');const generation=epoch.current;
  try{const result=await fn();if(mode!=='demo'){const value=await observerBackend.status();if(generation===epoch.current){stamp.current={mono:performance.now(),wall:Date.now()};setSnapshot(value);setOnline(true);}}return result;}
  catch(e){if(generation===epoch.current)setError(observationError(e));return undefined;}
  finally{fetching.current=false;if(generation===epoch.current)setBusy(false);}
 }
 function demoRun(engine:string,targets:ObservationTarget[]){
  const now=new Date().toISOString();const base=serial.current;serial.current+=targets.length+1;const id=`demo-job-${base}`;
  const samples=targets.map((target,i)=>({...demoObservation(target,engine,base+i),jobId:id}));
  stamp.current={mono:performance.now(),wall:Date.now()};
  setSnapshot(old=>({...old,owner:'demo',latest:[...samples,...old.latest.filter(x=>!samples.some(s=>s.source===x.source&&observationTargetKey(s.target)===observationTargetKey(x.target)))],history:[...samples,...old.history].slice(0,50),retainedSamples:old.retainedSamples+samples.length,jobs:[{id,kind:'demo',engine,state:'completed',completed:targets.length,total:targets.length,startedAt:now,finishedAt:now},...old.jobs].slice(0,20)}));
 }
 useEffect(()=>{if(mode!=='demo'||!snapshot.running)return;const timer=setInterval(()=>{for(const p of snapshot.policies){if(p.enabled)demoRun(p.engine,p.targets);}},5000);return ()=>clearInterval(timer);},[mode,snapshot.running,snapshot.policies]);
 async function configure(p:ObservationPolicy){
  if(mode==='demo'){const value={...p,id:p.id||`demo-policy-${++serial.current}`};setSnapshot(old=>({...old,policies:[value,...old.policies.filter(x=>x.id!==value.id)],running:value.enabled||old.running}));if(value.enabled)demoRun(value.engine,value.targets);return value;}
  return perform(()=>observerBackend.configure(p));
 }
 async function action(action:string,id=''){
  if(mode==='demo'){if(action==='start'){for(const p of snapshot.policies){if(p.enabled)demoRun(p.engine,p.targets);}}setSnapshot(old=>({...old,owner:action==='stop'?'stopped':action==='start'?'demo':old.owner,latest:action==='stop'?old.latest.map(sample=>({...sample,fresh:false})):old.latest,running:action==='start'?true:action==='stop'?false:old.running,policies:action==='remove'?old.policies.filter(p=>p.id!==id):old.policies}));return;}
  await perform(()=>observerBackend.action(action,id));
 }
 async function run(policy:ObservationPolicy){if(mode==='demo'){demoRun(policy.engine,policy.targets);return;}await perform(()=>observerBackend.run(policy.id));}
 async function diagnose(engine:string,targets:ObservationTarget[]){if(mode==='demo'){demoRun(engine,targets);return;}await perform(()=>observerBackend.diagnose(engine,targets));}
 async function install(engine:string){if(mode==='demo'){setSnapshot(old=>({...old,tools:[...old.tools.filter(t=>t.engine!==engine),{engine,version:engine==='checker'?'1.3.1 (demo)':'11.5.0 (demo)',configured:true}]}));return;}await perform(()=>observerBackend.install(engine));}
 const latest=snapshot.latest.map(sample=>({...sample,fresh:observationFresh(sample,online,stamp.current.mono,stamp.current.wall,performance.now(),Date.now())}));
 return {snapshot:{...snapshot,latest},online,busy,error,configure,action,run,diagnose,install};
}
export type ObservationState=ReturnType<typeof useObservation>;
