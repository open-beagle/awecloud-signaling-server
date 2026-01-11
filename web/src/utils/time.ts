/**
 * 格式化相对时间
 * @param dateString ISO时间字符串
 * @returns 相对时间字符串 (1y, 1m, 1d, 1h, 1m)
 */
export function formatRelativeTime(dateString: string): string {
  if (!dateString) {
    return '-'
  }

  const date = new Date(dateString)
  
  // 检查是否是 1970 年（Unix 纪元时间，表示未初始化）
  if (date.getFullYear() === 1970) {
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
 * @param dateString ISO时间字符串
 * @returns 格式化的时间字符串
 */
export function formatFullTime(dateString: string): string {
  if (!dateString) {
    return '-'
  }

  const date = new Date(dateString)
  
  // 检查是否是 1970 年（Unix 纪元时间，表示未初始化）
  if (date.getFullYear() === 1970) {
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
