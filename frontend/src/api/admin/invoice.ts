/**
 * Admin Invoice API endpoints
 */

import { apiClient } from '../client'
import { parseInvoiceDownload, type InvoiceDownload } from '../invoice'
import type {
  InvoiceActionRequest,
  InvoiceConfig,
  InvoiceDetail,
  InvoiceListParams,
  InvoicePage,
  InvoiceRecord
} from '@/types/invoice'

export const adminInvoiceAPI = {
  refundFee(id: string) { return apiClient.post<InvoiceDetail>(`/admin/invoices/${id}/refund-fee`) },
  getConfig() {
    return apiClient.get<InvoiceConfig>('/admin/invoices/config')
  },

  /** Enable or disable new invoice applications. */
  updateConfig(data: Partial<InvoiceConfig>) {
    return apiClient.put<InvoiceConfig>('/admin/invoices/config', data)
  },

  upload(id: string, file: File, invoiceNumber: string, confirmExternalChecked: boolean) {
    const data = new FormData()
    data.append('file', file)
    data.append('invoice_number', invoiceNumber)
    data.append('confirm_external_checked', String(confirmExternalChecked))
    return apiClient.post<InvoiceRecord>(`/admin/invoices/${id}/file`, data, {
      headers: { 'Content-Type': undefined }, timeout: 120000
    })
  },

  list(params?: InvoiceListParams) {
    return apiClient.get<InvoicePage>('/admin/invoices', { params })
  },

  detail(id: string) {
    return apiClient.get<InvoiceDetail>(`/admin/invoices/${id}`)
  },

  action(id: string, data: InvoiceActionRequest) {
    return apiClient.post<InvoiceRecord>(`/admin/invoices/${id}/action`, data)
  },

  async download(id: string, fallbackName = 'invoice-file'): Promise<InvoiceDownload> {
    const response = await apiClient.get<Blob>(`/admin/invoices/${id}/download`, {
      responseType: 'blob'
    })
    return parseInvoiceDownload(
      response.data,
      response.headers['content-disposition'] as string | undefined,
      fallbackName
    )
  }
}

export default adminInvoiceAPI
