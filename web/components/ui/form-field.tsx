import React from 'react';
import { Label } from './label';
import { cn } from '@/lib/utils';

export interface FormFieldProps {
  id?: string;
  htmlFor?: string;
  label: string;
  required?: boolean;
  description?: string;
  error?: string;
  children: React.ReactNode;
  className?: string;
}

export function FormField({
  id,
  htmlFor,
  label,
  required,
  description,
  error,
  children,
  className,
}: FormFieldProps) {
  const targetId = htmlFor || id;
  return (
    <div className={cn('space-y-1.5', className)}>
      <div className="flex items-center justify-between">
        <Label htmlFor={targetId} className="text-sm font-medium">
          {label}
          {required && <span className="text-destructive ml-1" aria-hidden="true">*</span>}
        </Label>
      </div>

      {description && (
        <p id={id ? `${id}-desc` : undefined} className="text-xs text-muted-foreground">
          {description}
        </p>
      )}

      {children}

      {error && (
        <p
          id={id ? `${id}-err` : undefined}
          role="alert"
          className="text-xs font-medium text-destructive animate-in fade-in-50"
        >
          {error}
        </p>
      )}
    </div>
  );
}
