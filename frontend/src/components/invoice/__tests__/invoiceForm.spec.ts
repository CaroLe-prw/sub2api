import { describe, expect, it } from 'vitest'
import { parseInvoicePaste, parseInvoiceUnitPrice } from '../invoiceForm'
describe('invoice form helpers', () => {
  it('converts optional unit price exactly to cents', () => {
    expect(parseInvoiceUnitPrice('')).toBeNull()
    expect(parseInvoiceUnitPrice('3.20')).toBe(320)
    expect(parseInvoiceUnitPrice('0.01')).toBe(1)
    for (const value of ['0', '-1', '1e3', '0.001', 'abc', '90000000000001']) expect(() => parseInvoiceUnitPrice(value)).toThrow()
  })
  it('extracts pasted buyer details locally without taking an invoice amount', () => {
    const fields = parseInvoicePaste('购方名称：示例有限公司\n税号：913100000000000000\n邮箱：billing@example.com\n金额：9999')
    expect(fields).toEqual({ company_name: '示例有限公司', tax_number: '913100000000000000', delivery_email: 'billing@example.com' })
  })
})
