export const MANUAL_INVOICE_ITEMS = ['技术服务费', 'AI订阅服务费', '计算服务费'] as const
export const MANUAL_INVOICE_CATEGORY = '生产生活服务'

/** Convert user-entered yuan without rounding silently or changing the invoice total. */
export function parseInvoiceUnitPrice(value: string): number | null {
  const text = value.trim()
  if (!text) return null
  if (!/^\d+(\.\d{1,2})?$/.test(text)) throw new Error('invalid price')
  const [yuan, fraction = ''] = text.split('.')
  const cents = Number(yuan) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(cents) || cents <= 0 || cents > 9000000000000000) throw new Error('invalid price')
  return cents
}

/** Local, label-based assistance. Never transmit pasted company details to an AI API. */
export function parseInvoicePaste(text: string): { company_name?: string; tax_number?: string; delivery_email?: string } {
  const fields: { company_name?: string; tax_number?: string; delivery_email?: string } = {}
  for (const line of text.split(/\r?\n/).map(value => value.trim()).filter(Boolean)) {
    const buyer = /^(?:购方名称|公司名称|企业名称|单位名称|发票抬头|抬头|名称)\s*[:：]\s*(.+)$/i.exec(line)
    if (buyer) fields.company_name = buyer[1].trim()
    if (!fields.company_name && /^[^:：]{2,200}(?:公司|商行|事务所)$/.test(line)) fields.company_name = line
    const tax = /(?:纳税人识别号|税号|统一社会信用代码)\s*[:：]\s*([A-Z0-9]{15,20})\b/i.exec(line)
    if (tax) fields.tax_number = tax[1].toUpperCase()
    const email = /[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/i.exec(line)
    if (email) fields.delivery_email = email[0]
  }
  return fields
}
