import { useEffect, useId, useState } from 'react';
import { Button, TextInput } from '../components/ui';

export type Suggestions = ((prefix: string) => Promise<string[]>) | readonly string[];

export function SuggestionInput({
  id,
  label,
  describedBy,
  addLabel,
  placeholder,
  suggest,
  selected,
  onAdd,
}: {
  id?: string;
  label: string;
  describedBy?: string;
  addLabel: string;
  placeholder?: string;
  suggest?: Suggestions;
  selected: string[];
  onAdd(value: string): boolean;
}) {
  const listID = useId();
  const [entry, setEntry] = useState('');
  const [suggesting, setSuggesting] = useState(false);
  const [active, setActive] = useState(-1);
  const [response, setResponse] = useState<{ prefix: string; values: string[] } | null>(null);
  const prefix = entry.trim();
  const remote = typeof suggest === 'function';
  const matches = remote ? (response?.prefix === prefix ? response.values : undefined) : suggest;
  const normalized = prefix.toLowerCase();
  const selectedNames = selected.map((value) => value.toLowerCase());
  const values =
    suggesting && matches
      ? matches
          .filter((value) => {
            const name = value.toLowerCase();
            return (remote || name.startsWith(normalized)) && !selectedNames.includes(name);
          })
          .slice(0, 10)
      : [];
  const expanded = Boolean(values.length);

  useEffect(() => {
    if (
      !suggesting ||
      typeof suggest !== 'function' ||
      prefix.length < 2 ||
      new TextEncoder().encode(prefix).length > 128
    )
      return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void suggest(prefix)
        .then((values) => {
          if (!cancelled) {
            const unique = new Map<string, string>();
            for (const value of values.slice(0, 10)) {
              if (value.trim() && new TextEncoder().encode(value).length <= 128) {
                unique.set(value.toLowerCase(), value);
              }
            }
            setResponse({ prefix, values: [...unique.values()] });
            setActive(-1);
          }
        })
        .catch(() => {
          if (!cancelled) setResponse({ prefix, values: [] });
        });
    }, 180);
    return () => {
      window.clearTimeout(timer);
      cancelled = true;
    };
  }, [prefix, suggest, suggesting]);

  useEffect(() => {
    if (expanded && active >= 0) {
      document.getElementById(`${listID}-${active}`)?.scrollIntoView?.({ block: 'nearest' });
    }
  }, [active, expanded, listID]);

  function add(value: string) {
    if (value && onAdd(value)) {
      setEntry('');
      setResponse(null);
      setActive(-1);
    }
  }

  return (
    <div className="search-tag-input">
      <div className="provider-suggest-input">
        <TextInput
          id={id}
          aria-label={label}
          aria-describedby={describedBy}
          autoComplete="off"
          placeholder={placeholder}
          maxLength={128}
          value={entry}
          role={suggest ? 'combobox' : undefined}
          aria-autocomplete={suggest ? 'list' : undefined}
          aria-expanded={suggest ? expanded : undefined}
          aria-controls={suggest ? listID : undefined}
          aria-activedescendant={expanded && active >= 0 ? `${listID}-${active}` : undefined}
          onFocus={() => setSuggesting(true)}
          onBlur={() => {
            setSuggesting(false);
            setActive(-1);
          }}
          onChange={(event) => {
            setEntry(event.currentTarget.value);
            if (event.currentTarget.value.trim() !== prefix) setResponse(null);
            setActive(-1);
            setSuggesting(event.currentTarget.matches(':focus'));
          }}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing) return;
            if (event.key === 'Enter') {
              event.preventDefault();
              add(values[active] || prefix);
            } else if (expanded && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
              event.preventDefault();
              const step = event.key === 'ArrowDown' ? 1 : -1;
              setActive(
                active === -1 && step === -1
                  ? values.length - 1
                  : (active + step + values.length) % values.length,
              );
            } else if (expanded && event.key === 'Escape') {
              event.preventDefault();
              event.stopPropagation();
              setSuggesting(false);
              setActive(-1);
            }
          }}
        />
        {suggest && (
          <ul
            id={listID}
            role="listbox"
            aria-label={`${label} suggestions`}
            tabIndex={-1}
            hidden={!expanded}
          >
            {values.map((value, index) => (
              <li
                id={`${listID}-${index}`}
                key={value}
                role="option"
                aria-selected={index === active}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => add(value)}
              >
                {value}
              </li>
            ))}
          </ul>
        )}
      </div>
      <Button onClick={() => add(prefix)}>{addLabel}</Button>
    </div>
  );
}
