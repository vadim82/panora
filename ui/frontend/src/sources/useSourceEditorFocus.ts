import { useLayoutEffect, useRef } from 'react';

export function canRestoreSourceFocus(origin: Element | null) {
  return [document.body, document.documentElement, origin].includes(document.activeElement);
}

export function useSourceEditorFocus(getState: () => { page: string }) {
  const focus = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (!focus.current) return;
    if (getState().page !== 'providers') {
      focus.current = null;
      return;
    }
    const target = document.getElementById(focus.current);
    if (
      !target ||
      target.closest('[hidden]') ||
      (target instanceof HTMLButtonElement && target.disabled)
    )
      return;
    target.focus();
    focus.current = null;
  });
  function restoreAfter(target: string, origin: Element | null) {
    if (getState().page === 'providers' && canRestoreSourceFocus(origin)) focus.current = target;
  }
  return { focus, restoreAfter };
}
