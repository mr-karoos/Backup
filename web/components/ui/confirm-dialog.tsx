'use client';

import React from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from './dialog';
import { Button } from './button';
import { Loader2 } from 'lucide-react';

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: React.ReactNode;
  confirmLabel?: string;
  confirmText?: string;
  cancelLabel?: string;
  cancelText?: string;
  variant?: 'destructive' | 'default';
  destructive?: boolean;
  objectName?: string;
  isLoading?: boolean;
  onConfirm: () => void | Promise<void>;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = 'Confirm',
  confirmText,
  cancelLabel = 'Cancel',
  cancelText,
  variant = 'default',
  destructive,
  objectName,
  isLoading = false,
  onConfirm,
}: ConfirmDialogProps) {
  const cancelRef = React.useRef<HTMLButtonElement>(null);
  const isDestructive = Boolean(destructive || variant === 'destructive');
  const resolvedConfirm = confirmText || confirmLabel;
  const resolvedCancel = cancelText || cancelLabel;

  return (
    <Dialog open={open} onOpenChange={(val) => !isLoading && onOpenChange(val)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription asChild>
            <div className="text-sm text-muted-foreground mt-2">
              {objectName && (
                <div className="font-semibold text-foreground mb-1 break-words">
                  {objectName}
                </div>
              )}
              {description}
            </div>
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col-reverse sm:flex-row sm:justify-end gap-2 mt-4">
          <Button
            ref={cancelRef}
            type="button"
            variant="outline"
            disabled={isLoading}
            onClick={() => onOpenChange(false)}
          >
            {resolvedCancel}
          </Button>
          <Button
            type="button"
            variant={isDestructive ? 'destructive' : 'default'}
            disabled={isLoading}
            onClick={async () => {
              await onConfirm();
            }}
          >
            {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {resolvedConfirm}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
