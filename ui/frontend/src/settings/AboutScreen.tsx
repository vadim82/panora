import { useEffect, useState } from 'react';
import { bridge, usePanora } from '../client';
import type { BuildIdentity } from '../types';

type ClipboardRuntime = { ClipboardSetText(text: string): Promise<boolean> };

export function AboutScreen() {
  const { navigate } = usePanora();
  const [identity, setIdentity] = useState<BuildIdentity | null>();
  const [notice, setNotice] = useState('');
  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const value = await bridge('BuildIdentity');
        if (active) setIdentity(value);
      } catch {
        if (active) setIdentity(null);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const rows = [
    ['Version', identity?.version],
    ['Arch package', identity?.arch_package],
    ['Source revision', identity?.revision],
  ];
  async function copy() {
    const lines = ['Panora'];
    if (identity?.development) lines.push('Development build');
    for (const [label, value] of rows) {
      if (value) lines.push(`${label}: ${value}`);
    }
    const text = lines.join('\n');
    const native = (window as Window & { runtime?: ClipboardRuntime }).runtime;
    try {
      if (native?.ClipboardSetText) {
        if (!(await native.ClipboardSetText(text))) throw new Error();
      } else {
        await navigator.clipboard.writeText(text);
      }
      setNotice('Version details copied.');
    } catch {
      setNotice('Select the version details to copy them.');
    }
  }

  return (
    <>
      <div className="heading">
        <h1 id="about-title" tabIndex={-1}>
          About Panora
        </h1>
        <button type="button" className="outline" onClick={() => navigate('settings')}>
          Back to Settings
        </button>
      </div>
      <div className="settings-panel about-panel">
        {identity === undefined && <p role="status">Loading version details…</p>}
        {identity === null && <p role="status">Version details are unavailable.</p>}
        {identity && (
          <>
            {identity.development && <p className="about-development">Development build</p>}
            <dl className="about-identity">
              {rows.map(([label, value]) =>
                value ? (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ) : null,
              )}
            </dl>
            <div className="form-actions">
              <button type="button" className="outline" onClick={() => void copy()}>
                Copy version details
              </button>
              <p role="status">{notice}</p>
            </div>
          </>
        )}
      </div>
    </>
  );
}
