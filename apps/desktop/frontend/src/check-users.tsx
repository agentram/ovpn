import { useEffect, useRef, useState } from 'react';
import { backend } from './api';
import type { CheckUserPage } from './api';
import { catalog } from './i18n';
import type { Language } from './i18n';

type UserOptions = CheckUserPage & { serverId: string; loading: boolean; error: boolean };

export function useCheckUsers(serverId: string) {
  const [options, setOptions] = useState<UserOptions>({ serverId: '', items: [], loading: false, error: false });
  const generation = useRef(0);

  async function load(cursor = '') {
    const request = ++generation.current;
    setOptions(old => ({ serverId, items: cursor ? old.items : [], loading: !!serverId, error: false, nextCursor: cursor }));
    if (!serverId) return;
    try {
      const page = await backend.checkUsers(serverId, cursor);
      if (request !== generation.current) return;
      setOptions(old => ({ serverId, items: cursor ? [...old.items, ...page.items] : page.items,
        nextCursor: page.nextCursor, loading: false, error: false }));
    } catch {
      if (request === generation.current) setOptions(old => ({ ...old, loading: false, error: true }));
    }
  }

  useEffect(() => {
    void load();
    return () => { generation.current++; };
  }, [serverId]);

  const current = options.serverId === serverId ? options : { serverId, items: [], loading: !!serverId, error: false };
  return { ...current, load };
}

export function CheckUserSelect({ language, options, value, onChange, disabled }: {
  language: Language;
  options: ReturnType<typeof useCheckUsers>;
  value: string;
  onChange: (username: string) => void;
  disabled?: boolean;
}) {
  const t = catalog(language);
  const selected = options.items.includes(value) ? value : '';
  let placeholder = t.chooseCheckUser;
  if (!options.serverId) placeholder = t.chooseServerFirst;
  else if (options.loading && !options.items.length) placeholder = t.loadingCheckUsers;
  else if (options.error && !options.items.length) placeholder = t.checkUsersError;
  else if (!options.items.length) placeholder = t.noCheckUsers;

  return <div className="check-user-select">
    <label>{t.identity}
      <select value={selected} disabled={disabled || !options.items.length} onChange={event => onChange(event.target.value)}>
        <option value="">{placeholder}</option>
        {options.items.map(username => <option key={username} value={username}>{username}</option>)}
      </select>
    </label>
    {options.error && <p className="help-line" role="alert">{t.checkUsersError}</p>}
    {(options.nextCursor || options.error) && <button className="text-button" type="button"
      disabled={disabled || options.loading} onClick={() => void options.load(options.nextCursor)}>
      {options.error ? t.retry : t.more}
    </button>}
  </div>;
}
