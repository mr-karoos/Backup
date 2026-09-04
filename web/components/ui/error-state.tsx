import { AlertTriangle, RefreshCw } from 'lucide-react';
import { Button } from './button';
import { ApiError } from '@/types/api';

export interface ErrorStateProps {
  title?: string;
  error: ApiError | Error | string | null | undefined;
  onRetry?: () => void;
  className?: string;
}

export function ErrorState({
  title = 'Something went wrong',
  error,
  onRetry,
  className,
}: ErrorStateProps) {
  let errorMessage = 'An unexpected error occurred.';
  let errorCode: string | undefined = undefined;
  let requestId: string | undefined = undefined;

  if (typeof error === 'string') {
    errorMessage = error;
  } else if (error instanceof ApiError) {
    errorMessage = error.message;
    errorCode = error.code;
    requestId = error.requestId;
  } else if (error instanceof Error) {
    errorMessage = error.message;
  }

  return (
    <div
      role="alert"
      className={`flex flex-col items-center justify-center rounded-lg border border-destructive/20 bg-destructive/5 p-8 text-center animate-in fade-in-50 ${
        className || ''
      }`}
    >
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <h3 className="mt-4 text-base font-semibold text-foreground">{title}</h3>
      {/* Explicitly rendered as plain text to prevent XSS injection */}
      <p className="mt-2 text-sm text-muted-foreground max-w-md font-mono break-words">
        {String(errorMessage)}
      </p>
      {errorCode && (
        <span className="mt-2 inline-block rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground font-mono">
          Code: {String(errorCode)}
        </span>
      )}
      {requestId && (
        <span className="mt-1 text-xs text-muted-foreground font-mono">
          Request ID: {String(requestId)}
        </span>
      )}
      {onRetry && (
        <Button onClick={onRetry} variant="outline" size="sm" className="mt-6 gap-2">
          <RefreshCw className="h-4 w-4" />
          Retry
        </Button>
      )}
    </div>
  );
}
