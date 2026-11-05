import { useEffect, useState } from 'react';
import type { Language } from './i18n';
const en = {
 preview:'Design preview', demo:'Simulated monitoring · no network requests', planned:'Planned capability',
 monitorTitle:'Observe every path. Act on evidence.', monitorIntro:'Server runtime, access policy and traffic are separate signals.',
 localTitle:'Set up an observation policy', localIntro:'The live build has manual native checks. Scheduled probes, checker ingestion and incident history below are the proposed next stage.',
 policy:'Automatic check policy', cadence:'Cadence', every:'Every 60 seconds', identity:'Identity', scoped:'Dedicated identity per server/profile',
 budget:'Traffic budget', bounded:'20 MiB per target/day · speed tests off', scope:'Scope', scopeValue:'Enabled node profiles · sample customer identities only with opt-in',
 policyHint:'Policy preview. Production scheduling is not enabled by these controls.', auto:'Demo scheduler', cycle:'Run demo cycle', cycleDone:'Cycle completed',
 health:'Fleet health', runtime:'Runtime', tunnel:'HTTPS path', auth:'User reconciliation', source:'Source', last:'Last sample',
 verified:'Verified', failed:'Failed', stale:'Stale', attention:'Needs attention', unknown:'No evidence', sampled:'Sample measurements',
 node:'Node', native:'Native Xray', checker:'Xray Checker', age:'just now', daemon:'Monitoring stays on when the window closes only with a configured observer service.',
 incidents:'Incidents', incidentTitle:'edge-b: HTTPS fails while runtime responds', incidentText:'Both local native and remote checker fixtures failed. The cause is still unknown; inspect the evidence before changing configuration.',
 inspect:'Inspect evidence', evidenceTitle:'Failure evidence', evidenceText:'Runtime: sample agent health passed. HTTPS: failed from two sample sources. Customer-wide failure is not established. Next: inspect user/runtime reconciliation and retry a bounded check.',
 toolsTitle:'One workspace. Specialized engines.', toolsIntro:'OVPN owns the workflow. Each adapter has a defined scope and returns normalized, redacted observations.',
 ready:'Available', unconfigured:'Not configured', sampleConnected:'Demo connection', setup:'Review integration',
 nativeDesc:'Fast explicit HTTPS checks through the pinned core, without an extra daemon.',
 checkerDesc:'Scheduled proxy checks, Prometheus metrics and independent observation sources.',
 knifeDesc:'Batch diagnostics, Xray/sing-box comparisons and explicit speed tests.',
 nativeCaps:['HTTPS / TCP','Existing identities','No direct fallback'], checkerCaps:['Latest proxy status','Named sources','Metrics and history adapter'], knifeCaps:['JSONL batch results','Core comparison','Budgeted experiments'],
 integrationTitle:'Adapter setup plan', permissions:'Minimum permissions', permissionsText:'Client-only prepared profiles, private job files and a bounded result allowlist. No control database or SSH keys are passed to a third-party tool.',
 risk:'Connection and performance tests consume traffic. Default scheduling uses a dedicated identity; speed tests and direct diagnostics require a separate choice.',
 operationsTitle:'A change has a scope and a result.', operationsIntro:'User lifecycle, subscription delivery and recovery are connected workflows.',
 journeys:['Create user → preview all enabled nodes → apply → verify per node → deliver subscription','Disable access → mark desired state → reconcile reachable nodes → show remaining nodes','Replace node → snapshot → canary apply → fresh connection → publish same subscription URL','Recover → verify archive and key → restore a disposable copy → verify access'],
 userChange:'User access', delivery:'Subscription delivery', recovery:'Recovery rehearsal',
 accessText:'Create, disable, expire and change per-server rolling quotas. Unreachable nodes remain visible until reconciliation succeeds.',
 deliveryText:'Publish, refresh, rotate or revoke a delivery URL. Imported VPN identities have a separate lifecycle.',
 recoveryText:'Verify restore material and rehearse recovery on a disposable target before relying on a backup.',
 implementation:'Implementation order', steps:['1. User visibility and rich policy views','2. Shared operation journal and reviewed user writes','3. Native scheduler and Checker adapter','4. Knife laboratory and compatibility matrix','5. Node groups, staged changes and recovery drills'],
 close:'Close', sourceRules:'Green = fresh verified result. Red = failed check. Amber = partial or stale. Gray = no evidence.',
} as const;
const ru: {[K in keyof typeof en]: typeof en[K] extends readonly string[]?readonly string[]:string} = {
 preview:'Проект интерфейса', demo:'Демонстрационный мониторинг · без сетевых запросов', planned:'Запланировано',
 monitorTitle:'Наблюдение за маршрутами с проверкой результата.', monitorIntro:'Службы сервера, политика доступа и трафик — отдельные сигналы.',
 localTitle:'Настройте политику наблюдения', localIntro:'В рабочей сборке доступны ручные проверки. Планировщик, приём данных Checker и история инцидентов ниже — проект следующего этапа.',
 policy:'Политика автоматических проверок', cadence:'Интервал', every:'Раз в 60 секунд', identity:'Учётная запись', scoped:'Отдельная учётная запись на сервер и профиль',
 budget:'Бюджет трафика', bounded:'20 МиБ на цель в сутки · тесты скорости выключены', scope:'Область', scopeValue:'Профили включённых узлов · проверки клиентов только по выбору',
 policyHint:'Предпросмотр политики. Эти настройки не включают рабочий планировщик.', auto:'Демо-планировщик', cycle:'Запустить демо-цикл', cycleDone:'Цикл завершён',
 health:'Состояние серверов', runtime:'Службы', tunnel:'HTTPS-маршрут', auth:'Сверка пользователей', source:'Источник', last:'Последняя проверка',
 verified:'Проверено', failed:'Ошибка', stale:'Устарело', attention:'Требует внимания', unknown:'Нет данных', sampled:'Пример измерений',
 node:'Узел', native:'Встроенный Xray', checker:'Xray Checker', age:'только что', daemon:'Для мониторинга при закрытом приложении нужен настроенный сервис наблюдения.',
 incidents:'Инциденты', incidentTitle:'edge-b: HTTPS не проходит, службы отвечают', incidentText:'Демо-проверки встроенного Xray и внешнего Checker завершились ошибкой. Причина пока неизвестна; перед изменением изучите данные.',
 inspect:'Открыть данные', evidenceTitle:'Данные об ошибке', evidenceText:'Службы: демо-агент отвечает. HTTPS: ошибка с двух демонстрационных источников. Отказ у всех клиентов не доказан. Далее: сверить пользователя с runtime и повторить ограниченную проверку.',
 toolsTitle:'Одна рабочая область. Специализированные движки.', toolsIntro:'Сценарием управляет OVPN. Адаптеры имеют заданную область и возвращают нормализованные результаты без секретов.',
 ready:'Доступен', unconfigured:'Не настроено', sampleConnected:'Демо-подключение', setup:'План подключения',
 nativeDesc:'Быстрая ручная проверка HTTPS через закреплённый core без отдельного сервиса.',
 checkerDesc:'Регулярные проверки прокси, метрики Prometheus и независимые источники наблюдения.',
 knifeDesc:'Пакетная диагностика, сравнение Xray/sing-box и явные тесты скорости.',
 nativeCaps:['HTTPS / TCP','Существующие учётные записи','Без прямого обхода'], checkerCaps:['Последний статус прокси','Именованные источники','Метрики и адаптер истории'], knifeCaps:['Пакетные результаты JSONL','Сравнение движков','Эксперименты с бюджетом'],
 integrationTitle:'План подключения адаптера', permissions:'Минимальные права', permissionsText:'Только подготовленные клиентские профили, приватные файлы задания и ограниченный набор результатов. База управления и SSH-ключи не передаются стороннему инструменту.',
 risk:'Проверки соединений и скорости расходуют трафик. Для расписания нужна отдельная учётная запись; тесты скорости и прямая диагностика выбираются отдельно.',
 operationsTitle:'У изменения есть область и проверяемый результат.', operationsIntro:'Пользователи, выдача подписок и восстановление связаны в единые сценарии.',
 journeys:['Создать пользователя → показать все включённые узлы → применить → проверить по узлам → выдать подписку','Отключить доступ → записать желаемое состояние → сверить доступные узлы → показать оставшиеся','Заменить узел → резервная копия → пробное применение → новое соединение → публикация по прежнему URL','Восстановить → проверить архив и ключ → развернуть отдельную копию → проверить доступ'],
 userChange:'Пользовательский доступ', delivery:'Выдача подписок', recovery:'Проверка восстановления',
 accessText:'Создание, отключение, сроки и скользящие квоты на сервер. Недоступные узлы видны до успешной сверки.',
 deliveryText:'Публикация, обновление, смена и отзыв URL подписки. У импортированных VPN-идентичностей отдельный жизненный цикл.',
 recoveryText:'Проверяйте материал восстановления и отдельный тестовый сервер, прежде чем полагаться на архив.',
 implementation:'Порядок реализации', steps:['1. Видимость пользователей и подробные политики','2. Общий журнал операций и проверяемые изменения доступа','3. Планировщик и адаптер Checker','4. Лаборатория Knife и матрица совместимости','5. Группы узлов, поэтапные изменения и проверки восстановления'],
 close:'Закрыть', sourceRules:'Зелёный — свежий проверенный результат. Красный — ошибка. Жёлтый — частичные или устаревшие данные. Серый — нет подтверждений.',
};
export function ProductPreview({language,mode,screen}: {language:Language;mode:string;screen:'monitoring'|'integrations'|'operations'}) {
 const t=language==='ru'?ru:en;
 const [auto,setAuto]=useState(true);
 const [cycles,setCycles]=useState(1);
 const [inspected,setInspected]=useState('');
 useEffect(()=>{if(!auto||mode!=='demo')return;const timer=setInterval(()=>setCycles(x=>x+1),5000);return ()=>clearInterval(timer);},[auto,mode]);
 const card=(name:string,description:string,kind:string)=><article className={`panel capability-card ${kind}`}><span className={`badge ${kind}`}>{t.planned}</span><h2>{name}</h2><p>{description}</p><button className="text-button" onClick={()=>setInspected(name)}>{t.setup} →</button></article>;
 return <div className="product-preview"><div className={`notice ${mode==='demo'?'preview-banner':'warn'}`}><strong>{mode==='demo'?t.demo:t.localIntro}</strong><span className="badge blue">{t.preview}</span></div>
 {screen==='monitoring'?<>
  <div className="monitor-toolbar"><div><h2>{mode==='demo'?t.health:t.localTitle}</h2><p>{t.monitorIntro}</p></div>{mode==='demo'&&<><label className="switch-label"><input type="checkbox" checked={auto} onChange={e=>setAuto(e.target.checked)}/>{t.auto}</label><button className="button" onClick={()=>setCycles(x=>x+1)}>{t.cycle}</button></>}</div>
  {mode==='demo'&&<><div className="signal-grid"><article className="signal-card ok"><span>{t.runtime}</span><strong>2 / 2</strong><small>● {t.verified}</small></article><article className="signal-card danger"><span>{t.tunnel}</span><strong>1 / 2</strong><small>● 1 {t.failed}</small></article><article className="signal-card warn"><span>{t.auth}</span><strong>1</strong><small>● {t.attention}</small></article></div><section className="panel"><div className="panel-title"><h2>{t.sampled}</h2><small>{t.cycleDone} #{cycles}</small></div><div className="table-scroll"><table><thead><tr><th>{t.node}</th><th>{t.runtime}</th><th>{t.tunnel}</th><th>{t.source}</th><th>{t.last}</th></tr></thead><tbody>{['edge-a','edge-b'].map((node,i)=><tr key={node}><td><strong>{node}</strong></td><td><span className="badge ok">✓ {t.verified}</span></td><td><span className={`badge ${i===1?'danger':'ok'}`}>{i===1?'!':'✓'} {i===1?t.failed:`HTTP 200 · ${42+i*17} ms`}</span></td><td>{i===1?t.checker:t.native}</td><td>{t.age}</td></tr>)}</tbody></table></div></section><article className="incident-card"><span className="incident-icon">!</span><div><span className="caption">{t.incidents}</span><h2>{t.incidentTitle}</h2><p>{t.incidentText}</p></div><button className="button" onClick={()=>setInspected('evidence')}>{t.inspect}</button></article></>}
  <section className="panel policy-panel"><div className="panel-title"><h2>{t.policy}</h2><span className="badge blue">{t.planned}</span></div><dl><dt>{t.cadence}</dt><dd>{t.every}</dd><dt>{t.identity}</dt><dd>{t.scoped}</dd><dt>{t.budget}</dt><dd>{t.bounded}</dd><dt>{t.scope}</dt><dd>{t.scopeValue}</dd></dl><p>{t.policyHint}</p><p>{t.daemon}</p></section><p className="help-line">{t.sourceRules}</p>
 </>:screen==='integrations'?<><div className="integration-grid">{[['Native Xray',t.nativeDesc,t.nativeCaps,'ok'],['Xray Checker',t.checkerDesc,t.checkerCaps,'blue'],['Xray Knife',t.knifeDesc,t.knifeCaps,'warn']].map(([name,description,caps,kind])=><article className={`panel integration-card ${kind}`} key={name as string}><div className="integration-mark">{name==='Native Xray'?'↗':name==='Xray Checker'?'◉':'⌁'}</div><div className="integration-card-head"><h2>{name}</h2><span className={`badge ${name==='Native Xray'?'ok':mode==='demo'?'blue':''}`}>{name==='Native Xray'?t.ready:mode==='demo'?t.sampleConnected:t.unconfigured}</span></div><p>{description}</p><ul>{(caps as readonly string[]).map(cap=><li key={cap}>✓ {cap}</li>)}</ul><button className="button wide" onClick={()=>setInspected(name as string)}>{t.setup}</button></article>)}</div><section className="panel policy-panel"><h2>{t.permissions}</h2><p>{t.permissionsText}</p><p>{t.risk}</p></section></>:<><div className="integration-grid">{card(t.userChange,t.accessText,'blue')}{card(t.delivery,t.deliveryText,'ok')}{card(t.recovery,t.recoveryText,'warn')}</div><section className="panel policy-panel"><h2>{t.operationsTitle}</h2><p>{t.operationsIntro}</p><ol className="journeys">{t.journeys.map(j=><li key={j}>{j}</li>)}</ol></section><section className="panel policy-panel"><h2>{t.implementation}</h2>{t.steps.map(step=><p key={step}>{step}</p>)}</section></>}
 {inspected&&<div className="notice inspect-panel" role="status"><div><h2>{inspected==='evidence'?t.evidenceTitle:t.integrationTitle+': '+inspected}</h2><p>{inspected==='evidence'?t.evidenceText:t.permissionsText}</p><p>{t.policyHint}</p></div><button className="button" onClick={()=>setInspected('')}>{t.close}</button></div>}
 </div>;
}
