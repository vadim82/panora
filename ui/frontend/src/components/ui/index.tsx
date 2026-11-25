import { Children, cloneElement, isValidElement } from 'react';
import type { ComponentProps, ReactElement, ReactNode } from 'react';

const classes = (...values: (string | undefined | false)[]) => values.filter(Boolean).join(' ');

type ButtonProps = ComponentProps<'button'> & {
  variant?: 'primary' | 'secondary' | 'destructive';
  pending?: boolean;
};
export function Button({
  variant,
  pending,
  disabled,
  className,
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      type={type}
      className={classes(
        'ui-button',
        variant === 'primary' && 'primary',
        variant === 'destructive' && 'destructive',
        className,
      )}
      disabled={disabled || pending}
      aria-busy={pending || props['aria-busy'] || undefined}
    />
  );
}

type FieldProps = ComponentProps<'label'> & {
  htmlFor: string;
  help?: ReactNode;
  error?: ReactNode;
};
export function Field({ htmlFor, help, error, children, className, ...props }: FieldProps) {
  const helpID = help ? `${htmlFor}-help` : undefined;
  const errorID = error ? `${htmlFor}-error` : undefined;
  return (
    <div className="ui-field" hidden={props.hidden}>
      <label {...props} htmlFor={htmlFor} className={className}>
        {Children.map(children, (child) => {
          if (!isValidElement<ComponentProps<'input'>>(child) || child.props.id !== htmlFor)
            return child;
          return cloneElement(child as ReactElement<ComponentProps<'input'>>, {
            'aria-describedby':
              classes(child.props['aria-describedby'], helpID, errorID) || undefined,
            'aria-invalid': error ? true : child.props['aria-invalid'],
          });
        })}
      </label>
      {help && (
        <span id={helpID} className="subtext ui-field-help">
          {help}
        </span>
      )}
      {error && (
        <span id={errorID} className="ui-field-error">
          {error}
        </span>
      )}
    </div>
  );
}

export function TextInput({ className, ...props }: Omit<ComponentProps<'input'>, 'type'>) {
  return <input {...props} type="text" className={classes('ui-input', className)} />;
}
export function NumberInput({ className, ...props }: Omit<ComponentProps<'input'>, 'type'>) {
  return <input {...props} type="number" className={classes('ui-input', className)} />;
}
export function Select({ className, ...props }: ComponentProps<'select'>) {
  return <select {...props} className={classes('ui-input', className)} />;
}
export function FormActions({ className, ...props }: ComponentProps<'div'>) {
  return <div {...props} className={classes('form-actions', className)} />;
}
type NoticeProps = ComponentProps<'p'> & { tone?: 'info' | 'success' | 'error' };
export function Notice({ tone = 'info', className, role, ...props }: NoticeProps) {
  return (
    <p
      {...props}
      role={role || (tone === 'error' ? 'alert' : 'status')}
      className={classes('ui-notice', className)}
      data-tone={tone}
    />
  );
}
