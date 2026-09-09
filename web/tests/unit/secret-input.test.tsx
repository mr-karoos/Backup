import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { SecretInput } from '@/components/ui/secret-input';

describe('SecretInput Component', () => {
  it('renders masked password input by default', () => {
    render(
      <SecretInput
        id="test-secret"
        value="super-secret-value"
        onChange={vi.fn()}
        placeholder="Enter secret"
      />
    );

    const input = screen.getByPlaceholderText('Enter secret');
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autoComplete', 'new-password');
    expect(input).toHaveAttribute('spellcheck', 'false');
  });

  it('toggles visibility between password and text when button clicked', () => {
    render(
      <SecretInput
        id="test-secret"
        value="my-password"
        onChange={vi.fn()}
        placeholder="Secret"
      />
    );

    const input = screen.getByPlaceholderText('Secret');
    const toggleButton = screen.getByRole('button', { name: /show secret/i });

    expect(input).toHaveAttribute('type', 'password');

    // Click show
    fireEvent.click(toggleButton);
    expect(input).toHaveAttribute('type', 'text');
    expect(screen.getByRole('button', { name: /hide secret/i })).toBeInTheDocument();

    // Click hide
    fireEvent.click(screen.getByRole('button', { name: /hide secret/i }));
    expect(input).toHaveAttribute('type', 'password');
  });

  it('calls onChange when user enters text', () => {
    const handleChange = vi.fn();
    render(
      <SecretInput
        id="test-secret"
        value=""
        onChange={handleChange}
        placeholder="Type here"
      />
    );

    const input = screen.getByPlaceholderText('Type here');
    fireEvent.change(input, { target: { value: 'new-key' } });

    expect(handleChange).toHaveBeenCalled();
  });
});
