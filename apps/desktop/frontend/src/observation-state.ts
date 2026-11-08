import { useEffect, useRef, useState } from 'react';
import { emptyObserver, observerBackend, observationFresh, observationError } from './api';
import type { ObservationPolicy, ObservationTarget } from './api';

export function useObservation() {
  const [snapshot, setSnapshot] = useState(emptyObserver);
  const [online, setOnline] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const stamp = useRef({ mono: performance.now(), wall: Date.now() });
  const generation = useRef(0);
  const polling = useRef(false);
  const mutating = useRef(false);

  async function refresh() {
    const request = ++generation.current;
    try {
      const value = await observerBackend.status();
      if (generation.current !== request) return;
      stamp.current = { mono: performance.now(), wall: Date.now() };
      setSnapshot(value);
      setOnline(true);
    } catch {
      if (generation.current === request) setOnline(false);
    }
  }

  useEffect(() => {
    async function poll() {
      if (polling.current || mutating.current) return;
      polling.current = true;
      try { await refresh(); }
      finally { polling.current = false; }
    }
    void poll();
    const timer = setInterval(() => void poll(), 2000);
    return () => { clearInterval(timer); generation.current++; };
  }, []);

  async function perform<T>(fn: () => Promise<T>): Promise<T | undefined> {
    if (mutating.current) return;
    mutating.current = true;
    generation.current++;
    setBusy(true);
    setError('');
    try {
      const result = await fn();
      await refresh();
      return result;
    } catch (err) {
      setError(observationError(err));
    } finally {
      mutating.current = false;
      setBusy(false);
    }
  }

  const latest = snapshot.latest.map(sample => ({ ...sample, fresh: observationFresh(sample, online === true,
    stamp.current.mono, stamp.current.wall, performance.now(), Date.now()) }));
  return {
    snapshot: { ...snapshot, latest }, online, busy, error,
    configure: (policy: ObservationPolicy) => perform(() => observerBackend.configure(policy)),
    action: (action: string, id = '') => perform(() => observerBackend.action(action, id)),
    run: (policy: ObservationPolicy) => perform(() => observerBackend.run(policy.id)),
    diagnose: (engine: string, targets: ObservationTarget[]) => perform(() => observerBackend.diagnose(engine, targets)),
    install: (engine: string) => perform(() => observerBackend.install(engine)),
  };
}
export type ObservationState = ReturnType<typeof useObservation>;
