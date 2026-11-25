import { useState } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Button } from './index';
import { ConfirmDialog } from './ConfirmDialog';

function Harness({
  active = true,
  confirm = () => {},
}: {
  active?: boolean;
  confirm?: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button id="remove-trigger" onClick={() => setOpen(true)}>
        Remove source…
      </Button>
      <Button>Other action</Button>
      <ConfirmDialog
        id="test-confirm"
        open={open}
        active={active}
        title="Remove source?"
        description="Originals and history are kept."
        confirmLabel="Remove source"
        returnFocusID="remove-trigger"
        onCancel={() => setOpen(false)}
        onConfirm={confirm}
      />
    </>
  );
}

describe('shared confirmation workflows', () => {
  it('announces the title and description, traps keyboard focus, and returns to its trigger after Escape', async () => {
    const user = userEvent.setup(),
      confirm = vi.fn();
    render(<Harness confirm={confirm} />);
    const trigger = screen.getByRole('button', { name: 'Remove source…' });
    await user.click(trigger);
    const dialog = screen.getByRole('alertdialog', { name: 'Remove source?' });
    expect(dialog).toHaveAccessibleDescription('Originals and history are kept.');
    const cancel = within(dialog).getByRole('button', { name: 'Cancel' });
    expect(cancel).toHaveFocus();
    await user.tab();
    expect(within(dialog).getByRole('button', { name: 'Remove source' })).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();
    await user.tab({ shift: true });
    expect(within(dialog).getByRole('button', { name: 'Remove source' })).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(confirm).not.toHaveBeenCalled();
    // The static backdrop does not introduce a CSP-blocked scroll-lock style tag.
    expect(document.head.querySelector('style')).toBeNull();
  });

  it('restores the explicit trigger when native activation did not focus the button', async () => {
    const user = userEvent.setup();
    render(<Harness />);
    const trigger = screen.getByRole('button', { name: 'Remove source…' });
    screen.getByRole('button', { name: 'Other action' }).focus();
    fireEvent.click(trigger);
    await user.keyboard('{Escape}');
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it('sends one explicit confirmation even before a deferred feature handler closes the dialog', async () => {
    const user = userEvent.setup(),
      confirm = vi.fn();
    render(<Harness confirm={confirm} />);
    await user.click(screen.getByRole('button', { name: 'Remove source…' }));
    const button = within(screen.getByRole('alertdialog')).getByRole('button', {
      name: 'Remove source',
    });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(confirm).toHaveBeenCalledTimes(1);
    await user.keyboard('{Escape}');
    await user.click(screen.getByRole('button', { name: 'Remove source…' }));
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Cancel' }),
    );
    expect(confirm).toHaveBeenCalledTimes(1);
  });

  it('dismisses a hidden page without mutation, stale reopening or focus restoration into that page', async () => {
    const user = userEvent.setup(),
      confirm = vi.fn();
    const view = render(<Harness confirm={confirm} />);
    await user.click(screen.getByRole('button', { name: 'Remove source…' }));
    view.rerender(<Harness active={false} confirm={confirm} />);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    const elsewhere = screen.getByRole('button', { name: 'Other action' });
    elsewhere.focus();
    view.rerender(<Harness active confirm={confirm} />);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(elsewhere).toHaveFocus();
    expect(confirm).not.toHaveBeenCalled();
  });
});
