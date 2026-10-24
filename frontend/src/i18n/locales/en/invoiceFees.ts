export default {
  appliedRate: 'Applied rate: {rate}%',
  invalidPricing: 'Enter valid amounts and rates with at most two decimal places.',
  settings: 'Minimum amount and tiered invoice fees',
  minimum: 'Minimum invoice amount (CNY)', tierStart: 'Tier starts at (CNY, inclusive)', rate: 'Fee rate (%)', addTier: 'Add tier',
  rules: 'Apply the matching rate to the entire invoice amount. Starts are inclusive, next starts exclusive, and the last tier has no ceiling. Fees round up to the nearest cent. Changes do not alter existing applications.',
  minimumHint: 'Minimum invoice amount: CNY {amount}', estimated: 'Estimated invoice fee: CNY {amount}', fee: 'Invoice fee', feeStatus: 'Fee status',
  free: 'No fee', unpaid: 'Awaiting payment', paid: 'Paid', refundPending: 'Refund required', refunded: 'Refunded',
  reviewHint: 'Verify the amount and fee. Free applications go directly to review. Paid tiers enter review after payment succeeds.',
  submitPay: 'Submit and pay fee', payTitle: 'Pay invoice fee', amountDue: 'Invoice fee due',
  noCredit: 'This payment covers the invoice fee only. It does not credit your balance or increase the invoice amount.', qr: 'Payment QR code', openPayment: 'Open payment page', waiting: 'Complete payment to submit for administrator review.',
  cancelPayment: 'Cancel checkout / change payment method', noMethods: 'No CNY payment method is available. Please contact the administrator.', checkPaid: 'Check payment', backToInvoices: 'Back to invoices',
  refund: 'Refund invoice fee', refundHint: 'Return the collected fee through its original payment channel. Repeating this action while a refund is pending queries its result. Refunds must be enabled for that channel.'
}
