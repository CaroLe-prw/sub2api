/**
 * User Invoice API endpoints
 */

import { apiClient } from './client'
import type {
  InvoiceCreate,
  InvoiceDetail,
  InvoiceListParams,
  InvoiceConfig,
  InvoiceOrderPage,
  InvoicePage,
  InvoiceRecord,
  InvoiceTitle
} from '@/types/invoice'

export interface InvoiceDownload {
  blob: Blob
  filename: string
}

export function parseInvoiceDownload(
  data: Blob,
  contentDisposition: string | undefined,
  fallback: string
): InvoiceDownload {
  let filename = fallback
  if (contentDisposition) {
    const match = /filename\*?=(?:UTF-8''|")?([^";]+)/i.exec(contentDisposition)
    if (match?.[1]) {
      filename = decodeURIComponent(match[1].replace(/"/g, '').trim())
    }
  }
  return { blob: data, filename }
}

export const invoiceAPI = {
  getConfig() { return apiClient.get<InvoiceConfig>('/invoices/config') },

  /** List completed top-up orders eligible for invoicing */
  getEligibleOrders(after?: number) {
    return apiClient.get<InvoiceOrderPage>('/invoices/eligible-orders', {
      params: after ? { after } : undefined
    })
  },

  /** List saved invoice titles of the current user */
  getTitles() {
    return apiClient.get<InvoiceTitle[]>('/invoices/titles')
  },

  deleteTitle(titleID: number) {
    return apiClient.delete(`/invoices/titles/${titleID}`)
  },

  /** List current user's invoice applications */
  list(params?: InvoiceListParams) {
    return apiClient.get<InvoicePage>('/invoices', { params })
  },

  /** Submit a new invoice application */
  create(data: InvoiceCreate) {
    return apiClient.post<InvoiceRecord>('/invoices', data)
  },

  detail(id: string) {
    return apiClient.get<InvoiceDetail>(`/invoices/${id}`)
  },

  /** Withdraw a pending-review application (the only user action) */
  withdraw(id: string) {
    return apiClient.post<InvoiceRecord>(`/invoices/${id}/action`, { action: 'withdraw' })
  },

  /** Download the issued invoice file */
  async download(id: string, fallbackName = 'invoice-file'): Promise<InvoiceDownload> {
    const response = await apiClient.get<Blob>(`/invoices/${id}/download`, {
      responseType: 'blob'
    })
    return parseInvoiceDownload(
      response.data,
      response.headers['content-disposition'] as string | undefined,
      fallbackName
    )
  }
}

export default invoiceAPI
