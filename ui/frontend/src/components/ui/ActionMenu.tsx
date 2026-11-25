import type { PropsWithChildren } from 'react';

export function ActionMenu({
  children,
  id,
  label,
  className = '',
}: PropsWithChildren<{ id: string; label: string; className?: string }>) {
  return (
    <details
      className={`action-menu ${className}`}
      onBlur={(event) => {
        if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget))
          event.currentTarget.open = false;
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || !event.currentTarget.open) return;
        event.preventDefault();
        event.stopPropagation();
        event.currentTarget.open = false;
        event.currentTarget.querySelector('summary')?.focus();
      }}
    >
      <summary className="outline" id={id} aria-label={label}>
        <span aria-hidden="true">⋮</span>
      </summary>
      <div
        className="action-menu-items"
        onMouseDown={(event) => {
          // WebKit can send focus to the document when clicking a button.
          // Move it within the disclosure so blur cannot close it before click.
          const action = event.target instanceof Element && event.target.closest('button, a');
          if (event.button === 0 && action instanceof HTMLElement && !action.matches(':disabled')) {
            event.preventDefault();
            action.focus();
          }
        }}
      >
        {children}
      </div>
    </details>
  );
}
