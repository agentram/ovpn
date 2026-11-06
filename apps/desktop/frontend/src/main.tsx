import { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { backend, demoServers, demoUsers, demoCheck, isFresh } from './api';
import type { Check, Server, Summary, User } from './api';
import { catalog } from './i18n';
import type { Language } from './i18n';
import { UsersPanel } from './users';
import { ProductPreview } from './product-preview';
import './style.css';

type View = 'overview' | 'users' | 'servers' | 'checks' | 'monitoring' | 'integrations' | 'operations';
type Sample = {check: Check; received: number; wall: number};
const nativeProfiles = ['vless-reality-tcp-vision', 'vless-tcp-tls-selfsni-web'];
const profileName = (p: string) => ({'vless-reality-tcp-vision':'REALITY / TCP', 'vless-tcp-tls-selfsni-web':'TLS / TCP', 'vless-xhttp-plain':'XHTTP', 'vless-xhttp-vlessenc':'XHTTP / Encryption'}[p] ?? p);
function pref(key: string, fallback: string) { try {return localStorage.getItem(key) ?? fallback;} catch {return fallback;} }
function save(key: string, value: string) {try {localStorage.setItem(key, value);} catch { /* Preferences remain usable in memory. */ }}
function Icon({name}: {name: string}) {
  const paths: Record<string,string> = {overview:'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',servers:'M3 3h18v8H3z M3 14h18v7H3z M7 7h1 M7 18h1 M13 7h4 M13 18h4',users:'M8 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M2 21v-3a6 6 0 0 1 12 0v3 M16 4a4 4 0 0 1 0 7 M18 15a5 5 0 0 1 4 5',monitoring:'M2 12h5l3-8 4 16 3-8h5',integrations:'M8 3v6 M16 3v6 M5 9h14v4a7 7 0 0 1-7 7v3 M12 20a7 7 0 0 1-7-7',operations:'M3 5v5h5 M3 10a9 9 0 1 1 0 7 M12 7v5l3 2',checks:'M2 12h5l3-8 4 16 3-8h5',settings:'M12 3v3 M12 18v3 M3 12h3 M18 12h3 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2',arrow:'M4 12h16 M14 6l6 6-6 6',shield:'M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M8 12l3 3 5-6'};
  return <svg viewBox="0 0 24 24" className="icon" aria-hidden="true"><path d={paths[name] ?? paths.shield}/></svg>;
}
function App() {
  const [language,setLanguage] = useState<Language>(() => pref('ovpn-desk.language','en') === 'ru' ? 'ru' : 'en');
  const [theme,setTheme] = useState(() => pref('ovpn-desk.theme','dark') === 'light' ? 'light' : 'dark');
  const [mode,setMode] = useState('demo');
  const [view,setView] = useState<View>('overview');
  const [servers,setServers] = useState<Server[]>(demoServers);
  const [summary,setSummary] = useState<Summary>({state:'ready',servers:3,enabledServers:2,users:demoUsers.length,readOnly:true});
  const [users,setUsers] = useState<User[]>(demoUsers);
  const [userCursor,setUserCursor] = useState('');
  const [usersBusy,setUsersBusy] = useState(false);
  const [usersError,setUsersError] = useState(false);
  const [cursor,setCursor] = useState('');
  const [busy,setBusy] = useState(false);
  const [error,setError] = useState(false);
  const [checkError,setCheckError] = useState(false);
  const [checking,setChecking] = useState(false);
  const [planning,setPlanning] = useState(false);
  const [selection,setSelection] = useState('node-1');
  const [profile,setProfile] = useState(demoServers[0].primaryProfile);
  const [username,setUsername] = useState('monitor');
  const [samples,setSamples] = useState<Record<string,Sample>>({});
  const [userSamples,setUserSamples] = useState<Record<string,Sample>>({});
  const [tick,setTick] = useState(0);
  const [engine,setEngine] = useState('');
  const [panel,setPanel] = useState<'settings'|'guide'|null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const epoch = useRef(0);
  const t = catalog(language);
  const selected = servers.find(s => s.id===selection);
  const fresh = (s: Sample) => isFresh(s.check,s.received,performance.now()) && isFresh(s.check,s.wall,Date.now());
  const sample = samples[selection];
  const passed = Object.values(samples).filter(s => fresh(s) && s.check.outcome==='passed').length;
  const canCheck = !!selected?.enabled && nativeProfiles.includes(profile) && username.trim().length>0 && !busy && !checking && !planning;
  useEffect(() => {document.documentElement.lang=language;save('ovpn-desk.language',language);},[language]);
  useEffect(() => {document.documentElement.dataset.theme=theme;save('ovpn-desk.theme',theme);},[theme]);
  useEffect(() => {const timer=setInterval(()=>setTick(x=>x+1),1000);return ()=>clearInterval(timer);},[]);
  useEffect(() => {if(panel) dialog.current?.showModal(); else dialog.current?.close();},[panel]);
  async function load(nextMode: string, more = false) {
    const request = ++epoch.current;
    setBusy(true);setError(false);setCheckError(false);
    if(!more) {setSamples({});setUserSamples({});setUsername(nextMode==='demo'?'monitor':'');setCursor('');setUserCursor('');setUsers([]);setUsersError(false);setServers([]);setEngine('');setSummary({state:'empty',servers:0,enabledServers:0,users:0,readOnly:true});}
    try {
      if(nextMode==='demo') {
        setServers(demoServers);setSummary({state:'ready',servers:3,enabledServers:2,users:demoUsers.length,readOnly:true});setUsers(demoUsers);choose(demoServers[0]);
      } else {
        const hello = await backend.hello();
        const page = await backend.servers(more?cursor:'');
        const info = await backend.summary();
        const userPage = !more ? await backend.users() : undefined;
        if(request!==epoch.current) return;
        if(userPage){setUsers(userPage.items);setUserCursor(userPage.nextCursor??'');}
        setEngine(hello.engineVersion);setSummary(info);setServers(old=>more?[...old,...page.items]:page.items);setCursor(page.nextCursor??'');
        if(!more) {const first=page.items.find(s=>s.enabled)??page.items[0];if(first) choose(first);else setSelection('');}
      }
    } catch {if(request===epoch.current) setError(true);}
    finally {if(request===epoch.current) setBusy(false);}
  }
  async function loadMoreUsers() {
    const request=epoch.current;
    setUsersBusy(true);setUsersError(false);
    try {const page=await backend.users(userCursor);if(request===epoch.current){setUsers(old=>[...old,...page.items]);setUserCursor(page.nextCursor??'');}}
    catch {if(request===epoch.current)setUsersError(true);}
    finally {if(request===epoch.current)setUsersBusy(false);}
  }
  function choose(server: Server) {setSelection(server.id);setProfile(server.profiles.find(p=>nativeProfiles.includes(p))??server.primaryProfile);setCheckError(false);}
  async function runCheck(event: React.FormEvent) {
    event.preventDefault();if(!canCheck) return;
    setChecking(true);setCheckError(false);
    setUserSamples(old=>{const next={...old};delete next[username.trim()];return next;});
    try {
      const request = {serverId:selection,profile,username:username.trim()};
      const check = mode==='demo' ? await new Promise<Check>(resolve=>setTimeout(()=>resolve(demoCheck(request)),900)) : await backend.check(request);
      const observation={check,received:performance.now(),wall:Date.now()};
      setSamples(old=>({...old,[request.serverId]:observation}));
      setUserSamples(old=>({...old,[request.username]:observation}));
    } catch {setCheckError(true);}
    finally {setChecking(false);}
  }
  const badge = (label: string,kind='') => <span className={`badge ${kind}`}>{label}</span>;
  const resultLabel = (s?: Sample) => !s?t.unknown:!fresh(s)?t.stale:s.check.outcome==='passed'?t.passed:t.failed;
  const table = <section className="panel table-panel"><div className="panel-title"><h2>{t.nodes}<small>{summary.servers} {t.total}</small></h2><button className="text-button" onClick={()=>setView('checks')}>{t.openChecks} <Icon name="arrow"/></button></div><div className="table-scroll"><table><thead><tr><th>{t.name}</th><th>{t.profile}</th><th>{t.desired}</th><th>{t.runtime}</th><th>{t.traffic}</th><th><span className="sr-only">{t.inspect}</span></th></tr></thead><tbody>{servers.map(s=><tr key={s.id}><td><div className="node-name"><span className="node-icon"><Icon name="servers"/></span><span><strong>{s.name}</strong><small>{s.enabled?t.on:t.off}</small></span></div></td><td className="mono">{profileName(s.primaryProfile)}</td><td className="mono secondary">{s.xrayVersion}</td><td>{badge(t.unknown)}</td><td>{badge(resultLabel(samples[s.id]),samples[s.id]&&fresh(samples[s.id])?(samples[s.id].check.outcome==='passed'?'ok':'danger'):'')}</td><td><button className="icon-button" aria-label={`${t.inspect} ${s.name}`} onClick={()=>{choose(s);setView('checks');}}><Icon name="arrow"/></button></td></tr>)}</tbody></table></div>{cursor&&<div className="table-foot"><button className="button" disabled={busy||checking||planning} onClick={()=>void load(mode,true)}>{t.more}</button></div>}</section>;
  return <div className="shell" data-tick={tick%2}>
    <aside className="sidebar"><a href="#overview" onClick={e=>{e.preventDefault();setView('overview');}} className="brand"><span className="brand-mark"/><span>ovpn<small>DESK</small></span></a>
      <label className="workspace-label"><span className="sr-only">{t.workspace}</span><select aria-label={t.workspace} value={mode} disabled={checking||busy||usersBusy||planning} onChange={e=>{setMode(e.target.value);void load(e.target.value);}}><option value="demo">{t.demo}</option><option value="local">{t.local}</option></select></label>
      <div className="caption">{t.workspace}</div><nav aria-label={t.workspace}>{(['overview','users','servers','checks','monitoring','integrations','operations'] as View[]).map(v=><button key={v} className={`nav-item ${view===v?'active':''}`} aria-current={view===v?'page':undefined} onClick={()=>setView(v)}><Icon name={v}/>{t[v]}</button>)}</nav>
      <div className="sidebar-bottom"><div className="ownership"><Icon name="shield"/><span>{t.personal}<br/><strong>{t.control}</strong></span></div><button className="nav-item" onClick={()=>setPanel('settings')}><Icon name="settings"/>{t.settings}</button><small className="build">{t.about}</small></div>
    </aside>
    <div className="body"><header className="topbar"><span>{mode==='demo'?t.demo:t.local}<b>/</b><strong>{t[view]}</strong></span>{badge(mode==='demo'?t.demoTag:t.early,'blue')}</header>
      <main>
        <div className="heading"><div><div className="caption">OVPN DESK</div><h1>{view==='checks'?t.checkTitle:view==='users'?t.usersTitle:['monitoring','integrations','operations'].includes(view)?t[view as 'monitoring'|'integrations'|'operations']:t.title}</h1><p>{view==='checks'?t.checkSubtitle:view==='users'?t.usersSubtitle:['monitoring','integrations','operations'].includes(view)?t.workflow:mode==='demo'?t.demoSubtitle:t.subtitle}</p></div>{['overview','servers','users'].includes(view)&&<button className="button" disabled={busy||checking||planning} onClick={()=>void load(mode)}><Icon name="servers"/>{t.refresh}</button>}</div>
        {busy&&<div className="notice" role="status">{t.loading}</div>}
        {error?<div className="panel empty" role="alert"><Icon name="shield"/><h2>{t.localError}</h2><p>{t.localErrorHint}</p><button className="button" onClick={()=>void load(mode)}>{t.retry}</button></div>:<>
          {['overview','servers','users'].includes(view)&&<div className="metrics">{[[t.inventory,summary.servers],[t.enabled,summary.enabledServers],[t.users,summary.users],[t.verified,passed]].map(([label,value])=><div key={label} className="metric"><span>{label}</span><strong>{value}</strong><small>{label===t.verified?t.fresh:t.total}</small></div>)}</div>}
          {view==='overview'&&<div className="intro"><div className="panel intro-main"><Icon name="shield"/><div><h2>{t.welcome}</h2><p>{t.welcomeText}</p></div><button className="button primary" onClick={()=>setView('checks')}>{t.openChecks}<Icon name="arrow"/></button></div><div className="notice"><strong>{t.readOnly}</strong><p>{t.readOnlyHint}</p></div></div>}
          {['overview','servers'].includes(view)&&(servers.length?table:!busy&&<div className="panel empty"><Icon name="servers"/><h2>{t.emptyTitle}</h2><p>{t.emptyText}</p><button className="button primary" onClick={()=>setPanel('guide')}>{t.guide}</button></div>)}
          {view==='users'&&<>{usersError&&<div className="notice danger-notice" role="alert">{t.localError}<button className="button" onClick={()=>void loadMoreUsers()}>{t.retry}</button></div>}<UsersPanel key={mode} language={language} mode={mode} users={users} observations={Object.fromEntries(Object.entries(userSamples).map(([name,sample])=>[name,{outcome:sample.check.outcome,server:servers.find(s=>s.id===sample.check.serverId)?.name??sample.check.serverId,profile:sample.check.profile,fresh:fresh(sample)}]))} enabled={summary.enabledServers} busy={busy||usersBusy||planning||checking} onPlanning={setPlanning} more={!!userCursor} onMore={()=>void loadMoreUsers()} onCheck={name=>{if(checking)return;setUsername(name);setView('checks');}} onDemoChange={next=>{if(mode!=='demo')return;setUsers(next);setUserSamples({});setSamples({});setSummary(old=>({...old,users:next.length}));}}/></>}
          {(view==='monitoring'||view==='integrations'||view==='operations')&&<ProductPreview key={view+mode} language={language} mode={mode} screen={view}/>}
          {view==='checks'&&<div className="checks-layout"><form className="panel check-form" onSubmit={e=>void runCheck(e)}><h2>{t.chosen}</h2><label>{t.select}<select value={selection} disabled={checking} onChange={e=>{const s=servers.find(s=>s.id===e.target.value);if(s)choose(s);}}>{!servers.length&&<option value="">—</option>}{servers.map(s=><option key={s.id} value={s.id}>{s.name}{s.enabled?'':` · ${t.off}`}</option>)}</select></label><label>{t.profile}<select value={profile} disabled={checking} onChange={e=>setProfile(e.target.value)}>{(selected?.profiles??[]).map(p=><option key={p} value={p}>{profileName(p)}</option>)}</select></label><label>{t.identity}<input required maxLength={128} autoComplete="off" value={username} disabled={checking} onChange={e=>setUsername(e.target.value)} placeholder="monitor"/><small>{t.identityHint}</small></label><div className="target"><span>{t.target}</span><strong className="mono">https://example.com/</strong><small>{t.targetHint}</small></div>{selected&&!selected.enabled&&<div className="notice">{t.disabled}</div>}{selected&&!nativeProfiles.includes(profile)&&<div className="notice warn">{t.unsupported}</div>}<button className="button primary wide" disabled={!canCheck} type="submit"><Icon name="checks"/>{checking?t.running:mode==='demo'?t.simulate:t.run}</button>{checkError&&<div role="alert" className="notice warn"><strong>{t.checkError}</strong><p>{t.checkErrorHint}</p></div>}</form>
            <section className="panel observation" aria-live="polite"><div className="caption">{t.result}</div>{sample?<><div className="observation-title"><h2>{selected?.name}</h2>{badge(resultLabel(sample),fresh(sample)?(sample.check.outcome==='passed'?'ok':'danger'):'')}</div><p>{profileName(sample.check.profile)}</p><div className="result-numbers"><div><span>{t.duration}</span><strong>{sample.check.durationMs}<small>ms</small></strong></div><div><span>{t.status}</span><strong>{sample.check.statusCode??'—'}</strong></div></div><dl><dt>{t.source}</dt><dd>{sample.check.source==='demo'?t.demoSource:t.localSource}</dd><dt>{t.firstByte}</dt><dd>{sample.check.ttfbMs} ms</dd><dt>{t.bytes}</dt><dd>{sample.check.bytes}</dd><dt>{t.scope}</dt><dd>{sample.check.outcome==='passed'?t.tcp:t.unknown}</dd><dt>{t.unverified}</dt><dd>UDP · DNS · IPv6 · GUI import</dd>{sample.check.errorCode&&<><dt>{t.failureReason}</dt><dd className="mono">{sample.check.errorCode}</dd></>}</dl><div className="notice">{fresh(sample)?t.fresh:t.stale}</div></>:<div className="empty"><Icon name="checks"/><h2>{t.noChecks}</h2><p>{t.noChecksText}</p></div>}</section></div>}
        </>}
      </main><footer><span><i/>{t.readOnly}</span><span>{mode==='demo'?t.demoSubtitle:t.scopeNote}</span></footer>
    </div>
    <dialog ref={dialog} onCancel={()=>setPanel(null)} onClose={()=>setPanel(null)}><div className="dialog-head"><h2>{panel==='settings'?t.settings:t.guideTitle}</h2><button className="icon-button" aria-label={t.close} onClick={()=>setPanel(null)}>✕</button></div>{panel==='settings'?<div className="dialog-body"><label>{t.language}<select value={language} onChange={e=>setLanguage(e.target.value as Language)}><option value="en">English</option><option value="ru">Русский</option></select></label><label>{t.theme}<select value={theme} onChange={e=>setTheme(e.target.value)}><option value="dark">{t.dark}</option><option value="light">{t.light}</option></select></label><p>{t.preferences}</p>{engine&&<dl><dt>{t.version}</dt><dd>{engine}</dd><dt>{t.protocol}</dt><dd>1</dd></dl>}</div>:<div className="dialog-body"><p>{t.guideText}</p><ol>{t.guideSteps.map(step=><li key={step}>{step}</li>)}</ol><pre>server add → server init → doctor{`\n`}→ user add → user link</pre></div>}<div className="dialog-foot"><button className="button primary" onClick={()=>setPanel(null)}>{t.close}</button></div></dialog>
  </div>;
}
createRoot(document.getElementById('root')!).render(<App/>);
