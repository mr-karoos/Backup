'use client';

import React, { useState, forwardRef } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { Input } from './input';
import { cn } from '@/lib/utils';

export interface SecretInputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
}

export const SecretInput = forwardRef<HTMLInputElement, SecretInputProps>(
  ({ className, error, ...props }, ref) => {
    const [show, setShow] = useState(false);

    return (
      <div className="relative flex items-center">
        <Input
          ref={ref}
          type={show ? 'text' : 'password'}
          autoComplete="new-password"
          spellCheck={false}
          className={cn('pr-10', error && 'border-destructive focus-visible:ring-destructive', className)}
          {...props}
        />
        <button
          type="button"
          onClick={() => setShow((prev) => !prev)}
          className="absolute right-3 p-1 text-muted-foreground hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm transition-colors"
          aria-label={show ? 'Hide secret' : 'Show secret'}
          tabIndex={0}
        >
          {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
        </button>
      </div>
    );
  }
);
SecretInput.displayName = 'SecretInput';
