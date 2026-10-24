/**
 * Invoice types
 * Mirrors backend DTOs in backend/internal/service/invoice_models.go
 */

export type InvoiceStatus =
  | 'PENDING_REVIEW'
  | 'APPROVED'
  | 'SUBMITTING'
  | 'PROCESSING'
  | 'UNKNOWN'
  | 'NEEDS_ATTENTION'
  | 'CANCELLING'
  | 'COMPLETED'
  | 'CANCELLED'
  | 'FAILED'
  | 'REJECTED'
  | 'WITHDRAWN'

export interface InvoiceConfig { enabled: boolean }

export interface InvoiceTitle {
  id: number
  company_name: string
  tax_number: string
  delivery_email: string
}

export interface InvoiceCreate {
  invoice_type?: 'NORMAL'
  invoice_category?: string
  quantity?: string
  unit_price_cent?: number | null
  company_name: string
  tax_number: string
  delivery_email: string
  order_ids: number[]
  invoice_item: string
  remark: string
  specification: string
  unit: string
  save_title: boolean
}

export interface InvoiceOrder {
  id: number
  out_trade_no: string
  pay_amount_cent: number
  amount_cent: number
  payment_fee_cent: number
  created_at: string
}

export interface InvoiceOrderPage {
  items: InvoiceOrder[]
  next_after: number
  has_more: boolean
}

export interface InvoiceRecord {
  invoice_type?: string
  invoice_category?: string
  quantity?: string
  unit_price_cent?: number | null
  manual: boolean
  requires_manual_check: boolean
  invoice_number: string
  uploaded_by: number
  completed_at: string | null
  id: string
  user_id: number
  status: InvoiceStatus
  company_name: string
  tax_number: string
  delivery_email: string
  remark: string
  invoice_item: string
  specification: string
  unit: string
  orders: InvoiceOrder[]
  amount_cent: number
  estimated_fee_cent: number
  fee_amount_cent: number
  upstream_id: string
  application_no: string
  local_status: string
  downloadable: boolean
  review_note: string
  reviewed_by: number
  reviewed_at: string | null
  last_error: string
  error_code: string
  trace_id: string
  cancel_requested: boolean
  last_synced_at: string | null
  created_at: string
  updated_at: string
}

export interface InvoiceEvent {
  actor: string
  action: string
  message: string
  created_at: string
}

export interface InvoiceDetail extends InvoiceRecord {
  events: InvoiceEvent[]
}

export interface InvoicePage {
  items: InvoiceRecord[]
  total: number
  page: number
  page_size: number
}

export interface InvoiceActionRequest {
  action: 'withdraw' | 'approve' | 'reject' | 'cancel'
  note?: string
  confirm_not_issued?: boolean
}

export interface InvoiceListParams {
  page?: number
  page_size?: number
  status?: string
  q?: string
}
