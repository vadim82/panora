import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { Field, NumberInput, Select, TextInput } from './index';

describe('shared field accessibility', () => {
  it('keeps the short native label separate from help and validation while preserving refs and existing descriptions', async () => {
    const label = createRef<HTMLLabelElement>(),
      input = createRef<HTMLInputElement>();
    const user = userEvent.setup();
    const view = render(
      <>
        <p id="folder-context">Only this folder changes.</p>
        <Field htmlFor="folder-title" ref={label} help="Shown in Sources." error="Enter a name.">
          Folder name
          <TextInput
            id="folder-title"
            ref={input}
            defaultValue="Archive"
            aria-describedby="folder-context"
          />
        </Field>
      </>,
    );
    const field = screen.getByRole('textbox', { name: 'Folder name' });
    expect(field).toBe(input.current);
    expect(field).toHaveAccessibleDescription(
      'Only this folder changes. Shown in Sources. Enter a name.',
    );
    expect(field).toHaveAttribute('aria-invalid', 'true');
    expect(field).toHaveValue('Archive');
    await user.click(label.current!);
    expect(field).toHaveFocus();
    view.rerender(
      <Field htmlFor="folder-title" help="Shown in Sources.">
        Folder name
        <TextInput id="folder-title" defaultValue="Archive" />
      </Field>,
    );
    expect(screen.getByRole('textbox', { name: 'Folder name' })).toHaveAccessibleDescription(
      'Shown in Sources.',
    );
    expect(screen.getByRole('textbox', { name: 'Folder name' })).not.toHaveAttribute(
      'aria-invalid',
    );
  });

  it('associates native number/select controls and hides their descriptions with the field', () => {
    render(
      <>
        <Field htmlFor="width" help="Pixels, without changing other sources.">
          Minimum width
          <NumberInput id="width" defaultValue={1000} min={1} />
        </Field>
        <Field htmlFor="shape" help="Only this source." hidden>
          Shape
          <Select id="shape" defaultValue="any">
            <option value="any">Any ratio</option>
          </Select>
        </Field>
      </>,
    );
    expect(screen.getByRole('spinbutton', { name: 'Minimum width' })).toHaveAccessibleDescription(
      'Pixels, without changing other sources.',
    );
    expect(screen.queryByRole('combobox', { name: 'Shape' })).not.toBeInTheDocument();
    expect(screen.getByText('Only this source.')).not.toBeVisible();
  });
});
