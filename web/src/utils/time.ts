/**
 * 检查是否是零值时间
 * @param date Date 对象
 * @returns 是否是零值时间
 */
function isZeroTime(date: Date): boolean {
  // 检查无效日期
  if (isNaN(date.getTime())) {
    return true
  }
  // 检查 Go 零值时间（0001年）、Unix 纪元（1970年）或其他明显无效的年份
  const year = date.getFullYear()
  return year < 2000
}

/**
 * 格式化相对时间
 * @param dateString ISO时间字符串或时间戳（毫秒）
 * @returns 相对时间字符串 (1y, 1m, 1d, 1h, 1m)
 */
export function formatRelativeTime(dateString: string | number): string {
  if (!dateString) {
    return '-'
  }

  const date = new Date(dateString)
  
  // 检查是否是零值时间（Go 零值 0001 年、Unix 纪元 1970 年等）
  if (isZeroTime(date)) {
    return '-'
  }

  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSeconds = Math.floor(diffMs / 1000)

  if (diffSeconds < 60) {
    return '1m' // 小于1分钟显示1m
  }

  const diffMinutes = Math.floor(diffSeconds / 60)
  if (diffMinutes < 60) {
    return `${diffMinutes}m`
  }

  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) {
    return `${diffHours}h`
  }

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 30) {
    return `${diffDays}d`
  }

  const diffMonths = Math.floor(diffDays / 30)
  if (diffMonths < 12) {
    return `${diffMonths}mo`
  }

  const diffYears = Math.floor(diffMonths / 12)
  return `${diffYears}y`
}

/**
 * 格式化完整时间
 * @param dateString ISO时间字符串或时间戳（毫秒）
 * @returns 格式化的时间字符串
 */
export function formatFullTime(dateString: string | number): string {
  if (!dateString) {
    return '-'
  }

  const date = new Date(dateString)
  
  // 检查是否是零值时间（Go 零值 0001 年、Unix 纪元 1970 年等）
  if (isZeroTime(date)) {
    return '-'
  }

  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')
  
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

/**
 * 格式化时间（默认使用完整时间格式）
 * @param dateString ISO时间字符串
 * @returns 格式化的时间字符串
 */
export function formatTime(dateString: string): string {
  return formatFullTime(dateString)
}
