export function monitorAvailabilityTextClass(value: number | null): string {
  if (value == null) return 'text-gray-500 dark:text-gray-400'
  if (value >= 99) return 'text-emerald-600 dark:text-emerald-400'
  if (value >= 90) return 'text-amber-600 dark:text-amber-400'
  return 'text-red-500 dark:text-red-400'
}
