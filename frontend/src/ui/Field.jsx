import { forwardRef, useId, useState } from 'react';
import { AlertCircle, Eye, EyeOff, Search, X } from 'lucide-react';
import { cn } from './cn';

const CONTROL =
  'w-full rounded-[var(--radius-control)] border bg-surface text-ink placeholder:text-ink-3/85 ' +
  'transition-[border-color,box-shadow,background-color] duration-150 ' +
  'disabled:cursor-not-allowed disabled:bg-surface-3 disabled:text-ink-3';

const CONTROL_SIZES = {
  sm: 'h-8 px-2.5 text-[12.5px]',
  md: 'h-9.5 px-3 text-[13.5px]',
  lg: 'h-12 px-3.5 text-[15px]',
  /* Sign-in only. Height and type track the measured proportions of the
     supplied design instead of a fixed step, so the card keeps its shape from
     a phone up to a 4K display. */
  auth: 'h-[clamp(44px,3.7vw,70px)] px-[clamp(0.85rem,1.05vw,1.35rem)] text-[clamp(13.5px,1vw,19px)] bg-surface-2',
};

function stateClasses(invalid) {
  return invalid
    ? 'border-critical/70 focus:border-critical'
    : 'border-line-strong hover:border-ink-3/55 focus:border-brand';
}

export function Field({ label, hint, error, required, htmlFor, children, className, labelSuffix }) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      {label && (
        <div className="flex items-baseline justify-between gap-3">
          <label
            htmlFor={htmlFor}
            className="text-[12.5px] font-medium tracking-[0.005em] text-ink-2"
          >
            {label}
            {required && (
              <span className="ml-1 text-critical" aria-hidden="true">
                *
              </span>
            )}
          </label>
          {labelSuffix}
        </div>
      )}
      {children}
      {error ? (
        <p className="flex items-start gap-1.5 text-[12px] font-medium text-critical" role="alert">
          <AlertCircle aria-hidden="true" className="mt-px size-3.5 shrink-0" />
          {error}
        </p>
      ) : (
        hint && <p className="text-[12px] text-ink-3">{hint}</p>
      )}
    </div>
  );
}

export const Input = forwardRef(function Input(
  { size = 'md', invalid, icon: Icon, className, ...rest },
  ref,
) {
  return (
    <div className="relative">
      {Icon && (
        <Icon
          aria-hidden="true"
          className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-3"
        />
      )}
      <input
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(CONTROL, CONTROL_SIZES[size], stateClasses(invalid), Icon && 'pl-9.5', className)}
        {...rest}
      />
    </div>
  );
});

/** Multi-line text, styled as the single-line input so the two sit together. */
export const Textarea = forwardRef(function Textarea({ invalid, rows = 3, className, ...rest }, ref) {
  return (
    <textarea
      ref={ref}
      rows={rows}
      aria-invalid={invalid || undefined}
      className={cn(CONTROL, 'resize-y px-3 py-2 text-[13px] leading-relaxed', stateClasses(invalid), className)}
      {...rest}
    />
  );
});

export const PasswordInput = forwardRef(function PasswordInput(
  { size = 'md', invalid, className, ...rest },
  ref,
) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="relative">
      <input
        ref={ref}
        type={visible ? 'text' : 'password'}
        aria-invalid={invalid || undefined}
        className={cn(CONTROL, CONTROL_SIZES[size], stateClasses(invalid), 'pr-11', className)}
        {...rest}
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? 'Hide password' : 'Show password'}
        className="absolute top-1/2 right-1.5 grid size-8 -translate-y-1/2 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink-2"
      >
        {visible ? (
          <EyeOff aria-hidden="true" className="size-4" />
        ) : (
          <Eye aria-hidden="true" className="size-4" />
        )}
      </button>
    </div>
  );
});

export const Select = forwardRef(function Select(
  { size = 'md', invalid, options = [], placeholder, className, ...rest },
  ref,
) {
  return (
    <div className="relative">
      <select
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(
          CONTROL,
          CONTROL_SIZES[size],
          stateClasses(invalid),
          'cursor-pointer appearance-none pr-8',
          className,
        )}
        {...rest}
      >
        {placeholder !== undefined && <option value="">{placeholder}</option>}
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        className="pointer-events-none absolute top-1/2 right-2.5 size-3.5 -translate-y-1/2 text-ink-3"
      >
        <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    </div>
  );
});

export function SearchInput({ value, onChange, placeholder = 'Search…', className, size = 'md', ...rest }) {
  const id = useId();
  return (
    <div className={cn('relative', className)}>
      <label className="sr-only" htmlFor={id}>
        {placeholder}
      </label>
      <Search
        aria-hidden="true"
        className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-3"
      />
      <input
        id={id}
        type="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className={cn(
          CONTROL,
          CONTROL_SIZES[size],
          stateClasses(false),
          'pl-9.5 pr-9 [&::-webkit-search-cancel-button]:hidden',
        )}
        {...rest}
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange('')}
          aria-label="Clear search"
          className="absolute top-1/2 right-1.5 grid size-7 -translate-y-1/2 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink-2"
        >
          <X aria-hidden="true" className="size-3.5" />
        </button>
      )}
    </div>
  );
}
