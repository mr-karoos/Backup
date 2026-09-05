'use client';

import React, { createContext, useContext, useState, useCallback, useMemo } from 'react';
import { X, CheckCircle, AlertCircle, Info } from 'lucide-react';
import { cn } from '@/lib/utils';

export type ToastVariant = 'default' | 'success' | 'destructive' | 'info';

export interface ToastMessage {
  id: string;
  title: string;
  description?: string;
  variant?: ToastVariant;
}

interface ToastContextType {
  toast: (options: Omit<ToastMessage, 'id'>) => void;
  dismiss: (id: string) => void;
}

const ToastContext = createContext<ToastContextType | undefined>(undefined);

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback(
    ({ title, description, variant = 'default' }: Omit<ToastMessage, 'id'>) => {
      const id = crypto.randomUUID();
      setToasts((prev) => [...prev, { id, title, description, variant }]);

      // Auto-dismiss after 4.5 seconds
      setTimeout(() => {
        dismiss(id);
      }, 4500);
    },
    [dismiss]
  );

  const value = useMemo(() => ({ toast, dismiss }), [toast, dismiss]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <aside
        aria-label="Notifications"
        aria-live="polite"
        className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-md w-full px-4 pointer-events-none"
      >
        {toasts.map((t) => (
          <div
            key={t.id}
            role={t.variant === 'destructive' ? 'alert' : 'status'}
            className={cn(
              'pointer-events-auto flex items-start gap-3 p-4 rounded-lg border shadow-lg transition-all animate-in fade-in slide-in-from-bottom-2',
              t.variant === 'success' && 'bg-emerald-950/90 border-emerald-800 text-emerald-100',
              t.variant === 'destructive' && 'bg-destructive border-destructive text-destructive-foreground',
              t.variant === 'info' && 'bg-sky-950/90 border-sky-800 text-sky-100',
              (!t.variant || t.variant === 'default') && 'bg-card border-border text-card-foreground'
            )}
          >
            <div className="shrink-0 mt-0.5">
              {t.variant === 'success' && <CheckCircle className="h-5 w-5 text-emerald-400" />}
              {t.variant === 'destructive' && <AlertCircle className="h-5 w-5 text-white" />}
              {t.variant === 'info' && <Info className="h-5 w-5 text-sky-400" />}
              {(!t.variant || t.variant === 'default') && <Info className="h-5 w-5 text-muted-foreground" />}
            </div>
            <div className="flex-1 text-sm">
              <div className="font-semibold">{t.title}</div>
              {t.description && <div className="text-xs opacity-90 mt-0.5">{t.description}</div>}
            </div>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
              className="shrink-0 text-current opacity-70 hover:opacity-100 p-1 rounded-sm transition-opacity"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        ))}
      </aside>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
}
