import * as AlertDialog from '@radix-ui/react-alert-dialog';
import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { Button, FormActions } from './index';

interface ConfirmDialogProps {
  id: string;
  open: boolean;
  active: boolean;
  title: ReactNode;
  description: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  confirmAriaLabel?: string;
  confirmID?: string;
  cancelID?: string;
  returnFocusID?: string;
  disabled?: boolean;
  pending?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onConfirmClose?: () => void;
}

export function ConfirmDialog({
  id,
  open,
  active,
  title,
  description,
  confirmLabel,
  cancelLabel = 'Cancel',
  confirmAriaLabel,
  confirmID,
  cancelID,
  returnFocusID,
  disabled,
  pending,
  onCancel,
  onConfirm,
  onConfirmClose,
}: ConfirmDialogProps) {
  const origin = useRef<HTMLElement | null>(null);
  const previousOpen = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);
  const submitted = useRef(false);
  const latest = useRef({ active, onConfirmClose });
  latest.current = { active, onConfirmClose };
  const visible = open && active;
  // Remember the actual trigger before Radix moves focus into its portal.
  if (visible && !previousOpen.current) {
    origin.current =
      (returnFocusID ? document.getElementById(returnFocusID) : null) ||
      (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    submitted.current = false;
  }
  previousOpen.current = visible;
  useEffect(() => {
    if (open && !active) onCancel();
  }, [open, active, onCancel]);
  return (
    <AlertDialog.Root
      open={visible}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialog.Portal>
        {/* Radix Overlay injects scroll-lock CSS. Keep this backdrop in the
            external stylesheet so the installed app retains its strict CSP. */}
        {visible && <div className="ui-dialog-overlay" />}
        <AlertDialog.Content
          id={id}
          className="ui-confirm-dialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            cancel.current?.focus();
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (submitted.current && latest.current.active && latest.current.onConfirmClose) {
              latest.current.onConfirmClose();
              return;
            }
            const target = origin.current;
            if (
              latest.current.active &&
              target?.isConnected &&
              !target.closest('[hidden]') &&
              !(target instanceof HTMLButtonElement && target.disabled) &&
              [document.body, document.documentElement].includes(
                document.activeElement as HTMLElement,
              )
            )
              target.focus();
          }}
          onEscapeKeyDown={(event) => {
            if (pending) event.preventDefault();
          }}
          onKeyDownCapture={(event) => {
            if (event.key !== 'Tab' || event.altKey || event.ctrlKey || event.metaKey) return;
            // WebKit's default macOS tab policy can skip buttons. Keep these
            // two explicit choices reachable without changing system settings.
            const actions = Array.from(
              event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'),
            );
            if (!actions.length) return;
            event.preventDefault();
            event.stopPropagation();
            const index = actions.indexOf(document.activeElement as HTMLButtonElement);
            actions[(index + (event.shiftKey ? -1 : 1) + actions.length) % actions.length]?.focus();
          }}
        >
          <AlertDialog.Title className="ui-dialog-title">{title}</AlertDialog.Title>
          <AlertDialog.Description className="ui-dialog-description">
            {description}
          </AlertDialog.Description>
          <FormActions>
            <AlertDialog.Cancel asChild>
              <Button id={cancelID} ref={cancel} disabled={pending}>
                {cancelLabel}
              </Button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <Button
                id={confirmID}
                aria-label={confirmAriaLabel}
                variant="destructive"
                disabled={disabled}
                pending={pending}
                onClick={(event) => {
                  event.preventDefault();
                  if (submitted.current || disabled || pending || !latest.current.active) return;
                  submitted.current = true;
                  onConfirm();
                }}
              >
                {confirmLabel}
              </Button>
            </AlertDialog.Action>
          </FormActions>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
