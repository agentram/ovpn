import { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { backend, isFresh } from './api';
import type { Check, Server, Summary, User } from './api';
import { catalog } from './i18n';
import type { Language } from './i18n';
import { UsersPanel } from './users';
import { CheckUserSelect, useCheckUsers } from './check-users';
import {ObserverWorkspace} from './observation';
import {useObservation} from './observation-state';
import {observationEn,observationRu} from './observation-i18n';
import './style.css';

type View = 'overview' | 'users' | 'servers' | 'checks' | 'monitoring' | 'integrations';
type Sample = {username: string; check: Check; received: number; wall: number};
const nativeProfiles = ['vless-reality-tcp-vision', 'vless-tcp-tls-selfsni-web'];
const profileName = (p: string) => ({'vless-reality-tcp-vision':'REALITY / TCP', 'vless-tcp-tls-selfsni-web':'TLS / TCP', 'vless-xhttp-plain':'XHTTP', 'vless-xhttp-vlessenc':'XHTTP / Encryption'}[p] ?? p);
function pref(key: string, fallback: string) { try {return localStorage.getItem(key) ?? fallback;} catch {return fallback;} }
function save(key: string, value: string) {try {localStorage.setItem(key, value);} catch { /* Preferences remain usable in memory. */ }}
function Icon({name}: {name: string}) {
  const paths: Record<string,string> = {overview:'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',servers:'M3 3h18v8H3z M3 14h18v7H3z M7 7h1 M7 18h1 M13 7h4 M13 18h4',users:'M8 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M2 21v-3a6 6 0 0 1 12 0v3 M16 4a4 4 0 0 1 0 7 M18 15a5 5 0 0 1 4 5',monitoring:'M2 12h5l3-8 4 16 3-8h5',integrations:'M8 3v6 M16 3v6 M5 9h14v4a7 7 0 0 1-7 7v3 M12 20a7 7 0 0 1-7-7',checks:'M2 12h5l3-8 4 16 3-8h5',settings:'M12 3v3 M12 18v3 M3 12h3 M18 12h3 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2',arrow:'M4 12h16 M14 6l6 6-6 6',shield:'M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M8 12l3 3 5-6'};
  return <svg viewBox="0 0 24 24" className="icon" aria-hidden="true"><path d={paths[name] ?? paths.shield}/></svg>;
}
function App() {
  const [language,setLanguage] = useState<Language>(() => pref('ovpn-desk.language','en') === 'ru' ? 'ru' : 'en');
  const [theme,setTheme] = useState(() => pref('ovpn-desk.theme','dark') === 'light' ? 'light' : 'dark');
  const [view,setView] = useState<View>('overview');
  const [servers,setServers] = useState<Server[]>([]);
  const [summary,setSummary] = useState<Summary>({state:'empty',servers:0,enabledServers:0,users:0,readOnly:true});
  const [users,setUsers] = useState<User[]>([]);
  const [userCursor,setUserCursor] = useState('');
  const [usersBusy,setUsersBusy] = useState(false);
  const [usersError,setUsersError] = useState(false);
  const [cursor,setCursor] = useState('');
  const [busy,setBusy] = useState(true);
  const [error,setError] = useState(false);
  const [checkError,setCheckError] = useState(false);
  const [checking,setChecking] = useState(false);
  const [planning,setPlanning] = useState(false);
  const [selection,setSelection] = useState('');
  const [profile,setProfile] = useState('');
  const [username,setUsername] = useState('');
  const [samples,setSamples] = useState<Record<string,Sample>>({});
  const [userSamples,setUserSamples] = useState<Record<string,Sample>>({});
  const [tick,setTick] = useState(0);
  const [engine,setEngine] = useState('');
  const [panel,setPanel] = useState<'settings'|'guide'|null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const epoch = useRef(0);
  const t = catalog(language);
  const ot=language==='ru'?observationRu:observationEn;
  const observation=useObservation();
  const selected = servers.find(s => s.id===selection);
  const fresh = (s: Sample) => isFresh(s.check,s.received,performance.now()) && isFresh(s.check,s.wall,Date.now());
  const userOptions = useCheckUsers(view === 'checks' && selected?.enabled ? selection : '');
  const serverSample = samples[selection];
  const sample = serverSample?.check.profile === profile && serverSample.username === username ? serverSample : undefined;
  const passed = Object.values(samples).filter(s => fresh(s) && s.check.outcome==='passed').length;
  const canCheck = !!selected?.enabled && nativeProfiles.includes(profile) && userOptions.items.includes(username) && !busy && !checking && !planning;
  useEffect(() => {document.documentElement.lang=language;save('ovpn-desk.language',language);},[language]);
  useEffect(() => {document.documentElement.dataset.theme=theme;save('ovpn-desk.theme',theme);},[theme]);
  useEffect(() => {const timer=setInterval(()=>setTick(x=>x+1),1000);return ()=>clearInterval(timer);},[]);
  useEffect(() => {if(panel) dialog.current?.showModal(); else dialog.current?.close();},[panel]);
  useEffect(() => { void load(); }, []);

  async function load(more = false) {
    const request = ++epoch.current;
    setBusy(true);
    setError(false);
    setCheckError(false);
    if (!more) {
      setSamples({});
      setUserSamples({});
      setUsername('');
      setCursor('');
      setUserCursor('');
      setUsers([]);
      setUsersError(false);
      setServers([]);
    }
    try {
      const hello = await backend.hello();
      const page = await backend.servers(more ? cursor : '');
      const info = await backend.summary();
      const userPage = more ? undefined : await backend.users();
      if (request !== epoch.current) return;
      if (userPage) {
        setUsers(userPage.items);
        setUserCursor(userPage.nextCursor ?? '');
      }
      setEngine(hello.engineVersion);
      setSummary(info);
      setServers(old => more ? [...old, ...page.items] : page.items);
      setCursor(page.nextCursor ?? '');
      if (!more) {
        const first = page.items.find(server => server.enabled) ?? page.items[0];
        if (first) choose(first);
        else setSelection('');
      }
    } catch {
      if (request === epoch.current) setError(true);
    } finally {
      if (request === epoch.current) setBusy(false);
    }
  }
  async function loadMoreUsers() {
    const request=epoch.current;
    setUsersBusy(true);setUsersError(false);
    try {const page=await backend.users(userCursor);if(request===epoch.current){setUsers(old=>[...old,...page.items]);setUserCursor(page.nextCursor??'');}}
    catch {if(request===epoch.current)setUsersError(true);}
    finally {if(request===epoch.current)setUsersBusy(false);}
  }
  function automatic(serverId:string){const rows=observation.snapshot.latest.filter(s=>s.target.serverId===serverId);if(!rows.length)return <span className="badge">{ot.noAuto}</span>;const passing=rows.filter(s=>s.fresh&&s.outcome==='passed').length;const failed=rows.some(s=>s.fresh&&s.outcome==='failed');return <span className={`badge ${failed?'danger':passing===rows.length?'ok':'warn'}`}>{passing} / {rows.length}</span>;}
  function choose(server: Server) {setSelection(server.id);setUsername('');setProfile(server.profiles.find(p=>nativeProfiles.includes(p))??server.primaryProfile);setCheckError(false);}
  async function runCheck(event: React.FormEvent) {
    event.preventDefault();
    if (!canCheck) return;
    const request = {serverId: selection, profile, username};
    setChecking(true);
    setCheckError(false);
    setSamples(old => {
      const next = {...old};
      delete next[request.serverId];
      return next;
    });
    setUserSamples(old => {
      const next = {...old};
      delete next[request.username];
      return next;
    });
    try {
      const check = await backend.check(request);
      const observation = {username: request.username, check, received: performance.now(), wall: Date.now()};
      setSamples(old => ({...old, [request.serverId]: observation}));
      setUserSamples(old => ({...old, [request.username]: observation}));
    } catch {
      setCheckError(true);
    } finally {
      setChecking(false);
    }
  }
  const badge = (label: string,kind='') => <span className={`badge ${kind}`}>{label}</span>;
  const resultLabel = (s?: Sample) => !s?t.unknown:!fresh(s)?t.stale:s.check.outcome==='passed'?t.passed:t.failed;
  const table = <section className="panel table-panel"><div className="panel-title"><h2>{t.nodes}<small>{summary.servers} {t.total}</small></h2><button className="text-button" onClick={()=>setView('checks')}>{t.openChecks} <Icon name="arrow"/></button></div><div className="table-scroll"><table><thead><tr><th>{t.name}</th><th>{t.profile}</th><th>{t.desired}</th><th>{t.runtime}</th><th>{t.traffic}</th><th>{ot.automated}</th><th><span className="sr-only">{t.inspect}</span></th></tr></thead><tbody>{servers.map(s=><tr key={s.id}><td><div className="node-name"><span className="node-icon"><Icon name="servers"/></span><span><strong>{s.name}</strong><small>{s.enabled?t.on:t.off}</small></span></div></td><td className="mono">{profileName(s.primaryProfile)}</td><td className="mono secondary">{s.xrayVersion}</td><td>{badge(t.unknown)}</td><td>{badge(resultLabel(samples[s.id]),samples[s.id]&&fresh(samples[s.id])?(samples[s.id].check.outcome==='passed'?'ok':'danger'):'')}</td><td>{automatic(s.id)}</td><td><button className="icon-button" aria-label={`${t.inspect} ${s.name}`} onClick={()=>{choose(s);setView('checks');}}><Icon name="arrow"/></button></td></tr>)}</tbody></table></div>{cursor&&<div className="table-foot"><button className="button" disabled={busy||checking||planning} onClick={()=>void load(true)}>{t.more}</button></div>}</section>;
  return <div className="shell" data-tick={tick%2}>
    <aside className="sidebar"><a href="#overview" onClick={e=>{e.preventDefault();setView('overview');}} className="brand"><span className="brand-mark"/><span>ovpn<small>DESK</small></span></a>
      <div className="caption">{t.workspace}</div><nav aria-label={t.workspace}>{(['overview','users','servers','checks','monitoring','integrations'] as View[]).map(v=><button key={v} className={`nav-item ${view===v?'active':''}`} aria-current={view===v?'page':undefined} onClick={()=>setView(v)}><Icon name={v}/>{t[v]}</button>)}</nav>
      <div className="sidebar-bottom"><button className="nav-item" onClick={()=>setPanel('settings')}><Icon name="settings"/>{t.settings}</button><small className="build">{t.about}</small></div>
    </aside>
    <div className="body"><header className="topbar"><span>{t.local}<b>/</b><strong>{t[view]}</strong></span></header>
      <main>
        <div className="heading">
          <div>
            <h1>{view === 'overview' ? t.title : t[view]}</h1>
            {view === 'checks' && <p>{t.checkSubtitle}</p>}
            {view === 'users' && <p>{t.usersSubtitle}</p>}
            {view === 'monitoring' && <p>{ot.intro}</p>}
            {view === 'integrations' && <p>{ot.toolsIntro}</p>}
          </div>
          {['overview', 'servers', 'users'].includes(view) && <button className="button" disabled={busy || checking || planning}
            onClick={() => void load()}><Icon name="servers"/>{t.refresh}</button>}
        </div>
        {busy&&<div className="notice" role="status">{t.loading}</div>}
        {error?<div className="panel empty" role="alert"><Icon name="shield"/><h2>{t.localError}</h2><p>{t.localErrorHint}</p><button className="button" onClick={()=>void load()}>{t.retry}</button></div>:<>
          {!busy&&['overview','servers','users'].includes(view)&&<div className="metrics">{[[t.inventory,summary.servers],[t.enabled,summary.enabledServers],[t.users,summary.users],[t.verified,passed]].map(([label,value])=><div key={label} className="metric"><span>{label}</span><strong>{value}</strong><small>{label===t.verified?t.fresh:t.total}</small></div>)}</div>}
          {['overview','servers'].includes(view)&&(servers.length?table:!busy&&<div className="panel empty"><Icon name="servers"/><h2>{t.emptyTitle}</h2><p>{t.emptyText}</p><button className="button primary" onClick={()=>setPanel('guide')}>{t.guide}</button></div>)}
          {view==='users'&&<>{usersError&&<div className="notice danger-notice" role="alert">{t.localError}<button className="button" onClick={()=>void loadMoreUsers()}>{t.retry}</button></div>}<UsersPanel language={language} users={users} automatic={observation.snapshot.latest} observations={Object.fromEntries(Object.entries(userSamples).map(([name,sample])=>[name,{outcome:sample.check.outcome,server:servers.find(s=>s.id===sample.check.serverId)?.name??sample.check.serverId,profile:sample.check.profile,fresh:fresh(sample)}]))} enabled={summary.enabledServers} busy={busy||usersBusy||planning||checking} onPlanning={setPlanning} more={!!userCursor} onMore={()=>void loadMoreUsers()} onCheck={name=>{if(checking)return;setUsername(name);setView('checks');}}/></>}
          {(view==='monitoring'||view==='integrations')&&<ObserverWorkspace key={view} language={language} screen={view} state={observation} servers={servers}/>}
          {view==='checks'&&<div className="checks-layout"><form className="panel check-form" onSubmit={e=>void runCheck(e)}><h2>{t.chosen}</h2><label>{t.select}<select value={selection} disabled={checking} onChange={e=>{const s=servers.find(s=>s.id===e.target.value);if(s)choose(s);}}>{!servers.length&&<option value="">—</option>}{servers.map(s=><option key={s.id} value={s.id}>{s.name}{s.enabled?'':` · ${t.off}`}</option>)}</select></label><label>{t.profile}<select value={profile} disabled={checking} onChange={e=>setProfile(e.target.value)}>{(selected?.profiles??[]).map(p=><option key={p} value={p}>{profileName(p)}</option>)}</select></label><CheckUserSelect language={language} options={userOptions} value={username} onChange={setUsername} disabled={checking}/><p className="help-line">{t.identityHint}</p><div className="target"><span>{t.target}</span><strong className="mono">https://example.com/</strong><small>{t.targetHint}</small></div>{selected&&!selected.enabled&&<div className="notice">{t.disabled}</div>}{selected&&!nativeProfiles.includes(profile)&&<div className="notice warn">{t.unsupported}</div>}<button className="button primary wide" disabled={!canCheck} type="submit"><Icon name="checks"/>{checking?t.running:t.run}</button>{checkError&&<div role="alert" className="notice warn"><strong>{t.checkError}</strong><p>{t.checkErrorHint}</p></div>}</form>
            <section className="panel observation" aria-live="polite"><div className="caption">{t.result}</div>{sample?<><div className="observation-title"><h2>{selected?.name}</h2>{badge(resultLabel(sample),fresh(sample)?(sample.check.outcome==='passed'?'ok':'danger'):'')}</div><p>{sample.username} · {profileName(sample.check.profile)}</p><div className="result-numbers"><div><span>{t.duration}</span><strong>{sample.check.durationMs}<small>ms</small></strong></div><div><span>{t.status}</span><strong>{sample.check.statusCode??'—'}</strong></div></div><dl><dt>{t.source}</dt><dd>{t.localSource}</dd><dt>{t.firstByte}</dt><dd>{sample.check.ttfbMs} ms</dd><dt>{t.bytes}</dt><dd>{sample.check.bytes}</dd><dt>{t.scope}</dt><dd>{sample.check.outcome==='passed'?t.tcp:t.unknown}</dd><dt>{t.unverified}</dt><dd>UDP · DNS · IPv6 · GUI import</dd>{sample.check.errorCode&&<><dt>{t.failureReason}</dt><dd className="mono">{sample.check.errorCode}</dd></>}</dl><div className="notice">{fresh(sample)?t.fresh:t.stale}</div></>:<div className="empty"><Icon name="checks"/><h2>{t.noChecks}</h2><p>{t.noChecksText}</p></div>}</section></div>}
        </>}
      </main><footer><span><i/>{observation.snapshot.running?ot.running:t.readOnly}</span><span>{t.scopeNote}</span></footer>
    </div>
    <dialog ref={dialog} onCancel={()=>setPanel(null)} onClose={()=>setPanel(null)}><div className="dialog-head"><h2>{panel==='settings'?t.settings:t.guideTitle}</h2><button className="icon-button" aria-label={t.close} onClick={()=>setPanel(null)}>✕</button></div>{panel==='settings'?<div className="dialog-body"><label>{t.language}<select value={language} onChange={e=>setLanguage(e.target.value as Language)}><option value="en">English</option><option value="ru">Русский</option></select></label><label>{t.theme}<select value={theme} onChange={e=>setTheme(e.target.value)}><option value="dark">{t.dark}</option><option value="light">{t.light}</option></select></label><p>{t.preferences}</p>{engine&&<dl><dt>{t.version}</dt><dd>{engine}</dd><dt>{t.protocol}</dt><dd>1</dd></dl>}</div>:<div className="dialog-body"><p>{t.guideText}</p><ol>{t.guideSteps.map(step=><li key={step}>{step}</li>)}</ol><pre>server add → server init → doctor{`\n`}→ user add → user link</pre></div>}<div className="dialog-foot"><button className="button primary" onClick={()=>setPanel(null)}>{t.close}</button></div></dialog>
  </div>;
}
createRoot(document.getElementById('root')!).render(<App/>);
