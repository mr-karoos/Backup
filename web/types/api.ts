/**
 * Canonical envelope for successful API responses matching Go backend ResponseEnvelope.
 */
export interface ApiResponseEnvelope<T> {
  data: T;
  message?: string;
  request_id?: string;
}

/**
 * Detailed error object structure matching Go backend ErrorDetail.
 */
export interface ApiErrorDetail {
  code: string;
  message: string;
  details?: unknown;
}

/**
 * Canonical error envelope matching Go backend ErrorEnvelope.
 */
export interface ApiErrorEnvelope {
  error: ApiErrorDetail;
  request_id?: string;
}

/**
 * Normalized frontend representation of an API error.
 */
export class ApiError extends Error {
  public readonly code: string;
  public readonly status: number;
  public readonly details?: unknown;
  public readonly requestId?: string;

  constructor(
    status: number,
    code: string,
    message: string,
    details?: unknown,
    requestId?: string
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
    this.requestId = requestId;
  }
}
