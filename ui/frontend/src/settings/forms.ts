import type { FormEvent } from 'react';
import type { ClientContext } from '../client';
import type { Page } from '../types';

export function revealInvalid(event: FormEvent<HTMLFormElement>) {
  const field = event.target;
  if (!(field instanceof HTMLElement)) return;
  for (
    let parent = field.parentElement;
    parent && parent !== event.currentTarget;
    parent = parent.parentElement
  ) {
    if (parent instanceof HTMLDetailsElement) parent.open = true;
  }
}

export function restoreFormFocus(
  id: string,
  client: ClientContext,
  page: Page,
  origin: Element | null,
) {
  // Let React commit enabled/hidden state before returning keyboard focus.
  setTimeout(() => {
    if (
      client.getState().page !== page ||
      ![document.body, document.documentElement, origin].includes(document.activeElement)
    )
      return;
    const target = document.getElementById(id);
    if (
      !(target instanceof HTMLElement) ||
      target.closest('[hidden]') ||
      target.matches(':disabled')
    )
      return;
    for (let parent = target.parentElement; parent; parent = parent.parentElement) {
      if (parent instanceof HTMLDetailsElement && !parent.open) return;
    }
    target.focus();
  }, 0);
}

export const fitLabel = (value: string) =>
  ({ auto: 'Automatic', contain: 'Fit whole image', crop: 'Crop to fill' })[value] || value;
export const bytes = (value: number) =>
  new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value / 1048576) + ' MiB';
