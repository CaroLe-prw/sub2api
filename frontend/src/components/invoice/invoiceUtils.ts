/**
 * Shared display helpers for invoice pages (user + admin).
 */

const INVOICE_STATUS_BADGE_MAP: Record<string, string> = {
  PENDING_PAYMENT: 'badge-warning',
  PENDING_REVIEW: 'badge-warning',
  APPROVED: 'badge-info',
  SUBMITTING: 'badge-info',
  PROCESSING: 'badge-info',
  UNKNOWN: 'badge-warning',
  NEEDS_ATTENTION: 'badge-danger',
  CANCELLING: 'badge-warning',
  COMPLETED: 'badge-success',
  CANCELLED: 'badge-secondary',
  FAILED: 'badge-danger',
  REJECTED: 'badge-secondary',
  WITHDRAWN: 'badge-secondary'
}

export function invoiceStatusBadgeClass(status: string): string {
  return INVOICE_STATUS_BADGE_MAP[status] || 'badge-secondary'
}

/** Format integer cents as a yuan string, e.g. 50000 -> "500.00" */
export function formatInvoiceCent(cent: number | string | null | undefined): string {
  if (cent === null || cent === undefined || cent === '') return '-'
  const n = typeof cent === 'string' ? Number(cent) : cent
  if (!Number.isFinite(n)) return '-'
  return (n / 100).toFixed(2)
}

